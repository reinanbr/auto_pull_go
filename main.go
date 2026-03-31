package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ─────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────

type Config struct {
	RepoPath             string `json:"repo_path"`
	Branch               string `json:"branch"`
	CheckIntervalSeconds int    `json:"check_interval_seconds"`
	PostPullCommand      string `json:"post_pull_command"`
	PostPullWorkdir      string `json:"post_pull_workdir"`
	LogFile              string `json:"log_file"`
	NotifyOnPull         bool   `json:"notify_on_pull"`

	// GithubToken is intentionally excluded from JSON serialization.
	// Set via AUTOPULL_TOKEN or GITHUB_TOKEN env var, or .env file in repo_path.
	GithubToken string `json:"-"`
}

// configFile is the on-disk representation — mirrors Config but omits the token.
type configFile struct {
	RepoPath             string `json:"repo_path"`
	Branch               string `json:"branch"`
	CheckIntervalSeconds int    `json:"check_interval_seconds"`
	PostPullCommand      string `json:"post_pull_command"`
	PostPullWorkdir      string `json:"post_pull_workdir"`
	LogFile              string `json:"log_file"`
	NotifyOnPull         bool   `json:"notify_on_pull"`
}

func loadDotEnvToken(baseDir string) string {
	f, err := os.Open(filepath.Join(baseDir, ".env"))
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if key == "AUTOPULL_TOKEN" || key == "GITHUB_TOKEN" {
			return val
		}
	}
	return ""
}

func tokenFromEnv() string {
	if v := os.Getenv("AUTOPULL_TOKEN"); v != "" {
		return v
	}
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		return v
	}
	return ""
}

func resolveConfigPath(p string) string {
	if p == "" {
		p = "config_auto_pull.json"
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

var version = "v1.1.3"

const gitTimeout = 15 * time.Second

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// reject legacy multi-repo configs explicitly
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if _, hasRepos := raw["repos"]; hasRepos {
		return nil, fmt.Errorf(
			"'repos' field is not supported: each repository should have its own config_auto_pull.json. " +
				"Run 'autopull init' inside each repo directory",
		)
	}
	if _, hasToken := raw["github_token"]; hasToken {
		return nil, fmt.Errorf(
			"'github_token' must not be set in config_auto_pull.json — " +
				"use AUTOPULL_TOKEN or GITHUB_TOKEN in a .env file or environment variable instead",
		)
	}

	var cf configFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg := &Config{
		RepoPath:             cf.RepoPath,
		Branch:               cf.Branch,
		CheckIntervalSeconds: cf.CheckIntervalSeconds,
		PostPullCommand:      cf.PostPullCommand,
		PostPullWorkdir:      cf.PostPullWorkdir,
		LogFile:              cf.LogFile,
		NotifyOnPull:         cf.NotifyOnPull,
	}

	// defaults
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.CheckIntervalSeconds <= 0 {
		cfg.CheckIntervalSeconds = 5
	}
	if cfg.LogFile == "" {
		cfg.LogFile = "auto_pull.log"
	}

	// token resolution: env var → .env file in repo dir → empty
	token := tokenFromEnv()
	if token == "" {
		baseDir := cfg.RepoPath
		if baseDir == "" {
			baseDir = filepath.Dir(path)
		}
		token = loadDotEnvToken(baseDir)
	}
	cfg.GithubToken = token

	return cfg, nil
}

func writeConfig(path string, cfg configFile) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func detectBranch(repoPath string) string {
	branch, err := runGit(repoPath, "", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return "main"
	}
	return branch
}

// ─────────────────────────────────────────────
// Logger
// ─────────────────────────────────────────────

type Logger struct {
	file   *os.File
	logger *log.Logger
}

func newLogger(logPath string) (*Logger, error) {
	if err := rotateIfLarge(logPath, getLogMaxBytes()); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file:   f,
		logger: log.New(f, "", 0),
	}, nil
}

func (l *Logger) log(level, msg string) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s", ts, level, msg)
	fmt.Println(line)
	l.logger.Println(line)
}

func (l *Logger) info(msg string)   { l.log("INFO ", msg) }
func (l *Logger) ok(msg string)     { l.log("OK   ", msg) }
func (l *Logger) warn(msg string)   { l.log("WARN ", msg) }
func (l *Logger) errLog(msg string) { l.log("ERROR", msg) }
func (l *Logger) close()            { l.file.Close() }

func rotateIfLarge(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < maxBytes {
		return nil
	}
	backup := path + ".1"
	_ = os.Remove(backup)
	return os.Rename(path, backup)
}

func getLogMaxBytes() int64 {
	const defaultSize = int64(5 * 1024 * 1024) // 5 MB
	raw := os.Getenv("AUTOPULL_LOG_MAX_BYTES")
	if raw == "" {
		return defaultSize
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return defaultSize
	}
	return v
}

// ─────────────────────────────────────────────
// Git helpers
// ─────────────────────────────────────────────

func runGit(dir string, token string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	env := os.Environ()
	var cleanup func()

	if token != "" {
		askpassPath, cleanupFn, err := createAskPassScript()
		if err != nil {
			return "", err
		}
		cleanup = cleanupFn
		env = append(env,
			"GIT_ASKPASS="+askpassPath,
			"GIT_TERMINAL_PROMPT=0",
			"GIT_USERNAME=oauth2",
			"GIT_TOKEN="+token,
		)
	}

	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if cleanup != nil {
		cleanup()
	}

	if ctx.Err() == context.DeadlineExceeded {
		return strings.TrimSpace(string(out)), fmt.Errorf("git command timed out after %s", gitTimeout)
	}

	return strings.TrimSpace(string(out)), err
}

// createAskPassScript creates a minimal POSIX sh script (no bash dependency)
// that prints the token when git asks for a password.
func createAskPassScript() (string, func(), error) {
	f, err := os.CreateTemp("", "autopull-askpass-*")
	if err != nil {
		return "", func() {}, err
	}

	// Use /bin/sh instead of bash — works on Alpine, Debian, macOS, etc.
	script := "#!/bin/sh\nprintf '%s' \"$GIT_TOKEN\"\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	if err := f.Chmod(0700); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", func() {}, err
	}

	cleanup := func() { _ = os.Remove(f.Name()) }
	return f.Name(), cleanup, nil
}

func localCommit(dir string) (string, error) {
	return runGit(dir, "", "rev-parse", "HEAD")
}

func remoteCommit(dir, branch, token string) (string, error) {
	if _, err := runGit(dir, token, "fetch", "origin", branch); err != nil {
		return "", fmt.Errorf("git fetch failed: %w", err)
	}
	return runGit(dir, token, "rev-parse", fmt.Sprintf("origin/%s", branch))
}

func pull(dir, branch, token string) (string, error) {
	return runGit(dir, token, "pull", "origin", branch)
}

func shortHash(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

func ensureGitRepo(path string) error {
	if path == "" {
		return fmt.Errorf("repo_path is required in config")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("repo_path not accessible: %w", err)
	}
	if _, err := runGit(path, "", "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("repo_path is not a git repository: %s", path)
	}
	return nil
}

func isRepoDirty(path string) bool {
	out, err := runGit(path, "", "status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// ─────────────────────────────────────────────
// Post-pull command
// ─────────────────────────────────────────────

func runPostCommand(cfg *Config, l *Logger) error {
	if cfg.PostPullCommand == "" {
		return nil
	}

	workdir := cfg.PostPullWorkdir
	if workdir == "" {
		workdir = cfg.RepoPath
	}

	l.info(fmt.Sprintf("Running post-pull command: %s", cfg.PostPullCommand))

	cmd := exec.Command("sh", "-c", cfg.PostPullCommand)
	cmd.Dir = workdir
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if output != "" {
		for _, line := range strings.Split(output, "\n") {
			l.info("  > " + line)
		}
	}
	return err
}

// ─────────────────────────────────────────────
// Notification (desktop — optional)
// ─────────────────────────────────────────────

func notify(title, body string) {
	if err := exec.Command("notify-send", title, body).Run(); err != nil {
		_ = exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "%s"`, body, title)).Run()
	}
}

// ─────────────────────────────────────────────
// Runtime state
// ─────────────────────────────────────────────

type RuntimeState struct {
	Pulls             int       `json:"pulls"`
	LastPull          time.Time `json:"last_pull"`
	ConsecutiveErrors int       `json:"consecutive_errors"`
	BackoffUntil      time.Time `json:"backoff_until"`
	LastError         string    `json:"last_error"`
}

type repoState struct {
	consecutiveErrors int
	backoffUntil      time.Time
}

func pidFilePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), ".auto_pull.pid")
}

func stateFilePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), ".auto_pull.state.json")
}

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func stopProcess(pidPath string) (string, error) {
	pid, err := readPID(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("pid file not found: %s", pidPath), nil
		}
		return "", err
	}
	if !processAlive(pid) {
		_ = os.Remove(pidPath)
		return fmt.Sprintf("Process %d not running; cleaned pid file", pid), nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	return fmt.Sprintf("Sent SIGTERM to %d", pid), nil
}

func loadRuntimeState(path string) *RuntimeState {
	data, err := os.ReadFile(path)
	if err != nil {
		return &RuntimeState{}
	}
	var rs RuntimeState
	if err := json.Unmarshal(data, &rs); err != nil {
		return &RuntimeState{}
	}
	return &rs
}

func saveRuntimeState(path string, rs *RuntimeState) error {
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func tailFile(path string, lines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		buf = append(buf, scanner.Text())
		if len(buf) > lines {
			buf = buf[1:]
		}
	}
	return strings.Join(buf, "\n"), nil
}

// ─────────────────────────────────────────────
// Backoff
// ─────────────────────────────────────────────

func backoffDuration(failures int) time.Duration {
	if failures < 1 {
		return 0
	}

	max := 5 * time.Minute
	d := time.Second

	for i := 1; i < failures; i++ {
		if d >= max {
			return max
		}
		// guard against overflow on large failure counts
		if d > (time.Duration(math.MaxInt64) / 2) {
			return max
		}
		d *= 2
	}

	if d > max {
		return max
	}
	return d
}

// ─────────────────────────────────────────────
// Core watch loop
// ─────────────────────────────────────────────

func watch(ctx context.Context, cfgPath string) {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	if err := ensureGitRepo(cfg.RepoPath); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	pidPath := pidFilePath(cfgPath)
	if err := writePID(pidPath, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: could not write pid file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(pidPath)

	statePath := stateFilePath(cfgPath)
	runtimeState := loadRuntimeState(statePath)

	logPath := cfg.LogFile
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(filepath.Dir(cfgPath), logPath)
	}

	l, err := newLogger(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: could not open log file: %v\n", err)
		os.Exit(1)
	}
	defer l.close()

	l.info("═══════════════════════════════════════════")
	l.info("         auto_pull started")
	l.info(fmt.Sprintf("  repo    : %s", cfg.RepoPath))
	l.info(fmt.Sprintf("  branch  : %s", cfg.Branch))
	l.info(fmt.Sprintf("  interval: %ds", cfg.CheckIntervalSeconds))
	l.info(fmt.Sprintf("  log     : %s", logPath))
	if cfg.GithubToken != "" {
		l.info("  token   : (set)")
	}
	if cfg.PostPullCommand != "" {
		l.info(fmt.Sprintf("  post-pull: %s", cfg.PostPullCommand))
	}
	l.info("═══════════════════════════════════════════")

	interval := time.Duration(cfg.CheckIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	st := &repoState{}
	running := false

	for {
		select {
		case <-ctx.Done():
			l.info("shutting down (signal received)")
			return
		case <-ticker.C:
		}

		if running {
			l.warn("previous cycle still running; skipping tick")
			continue
		}
		running = true

		func() {
			defer func() { running = false }()

			newCfg, err := loadConfig(cfgPath)
			if err != nil {
				l.warn(fmt.Sprintf("invalid config, keeping previous: %v", err))
			} else {
				cfg = newCfg
			}

			now := time.Now()
			if now.Before(st.backoffUntil) {
				l.warn(fmt.Sprintf("in backoff until %s", st.backoffUntil.Format(time.RFC3339)))
				return
			}

			processRepo(cfg, st, runtimeState, l)

			if err := saveRuntimeState(statePath, runtimeState); err != nil {
				l.warn(fmt.Sprintf("could not persist state: %v", err))
			}
		}()
	}
}

func processRepo(cfg *Config, st *repoState, rs *RuntimeState, l *Logger) {
	local, err := localCommit(cfg.RepoPath)
	if err != nil {
		st.consecutiveErrors++
		st.backoffUntil = time.Now().Add(backoffDuration(st.consecutiveErrors))
		rs.ConsecutiveErrors = st.consecutiveErrors
		rs.BackoffUntil = st.backoffUntil
		rs.LastError = err.Error()
		l.errLog(fmt.Sprintf("git rev-parse (local) failed (%dx): %v", st.consecutiveErrors, err))
		return
	}

	if isRepoDirty(cfg.RepoPath) {
		l.warn("working tree has uncommitted changes — skipping pull to avoid conflicts")
		return
	}

	remote, err := remoteCommit(cfg.RepoPath, cfg.Branch, cfg.GithubToken)
	if err != nil {
		st.consecutiveErrors++
		st.backoffUntil = time.Now().Add(backoffDuration(st.consecutiveErrors))
		rs.ConsecutiveErrors = st.consecutiveErrors
		rs.BackoffUntil = st.backoffUntil
		rs.LastError = err.Error()
		l.errLog(fmt.Sprintf("git fetch failed (%dx): %v", st.consecutiveErrors, err))
		return
	}

	// success — reset backoff
	st.consecutiveErrors = 0
	st.backoffUntil = time.Time{}
	rs.ConsecutiveErrors = 0
	rs.BackoffUntil = time.Time{}
	rs.LastError = ""

	if local == remote {
		return // nothing new
	}

	l.ok(fmt.Sprintf("new commit detected: %s → %s", shortHash(local), shortHash(remote)))

	out, err := pull(cfg.RepoPath, cfg.Branch, cfg.GithubToken)
	if err != nil {
		l.errLog(fmt.Sprintf("git pull failed: %v\n%s", err, out))
		return
	}
	l.ok("git pull completed")
	if out != "" {
		l.info("  " + strings.ReplaceAll(out, "\n", "\n  "))
	}

	rs.Pulls++
	rs.LastPull = time.Now()

	if cfg.NotifyOnPull {
		notify("auto_pull", fmt.Sprintf("Pull done: %s@%s", filepath.Base(cfg.RepoPath), cfg.Branch))
	}

	if err := runPostCommand(cfg, l); err != nil {
		l.errLog(fmt.Sprintf("post-pull command failed: %v", err))
	} else if cfg.PostPullCommand != "" {
		l.ok("post-pull command completed successfully")
	}
}

// ─────────────────────────────────────────────
// CLI commands
// ─────────────────────────────────────────────

func cmdInit(cfgPath string) error {
	cfgPath = resolveConfigPath(cfgPath)
	repoRoot, err := runGit(".", "", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("init requires a git repository: %w", err)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("config already exists: %s", cfgPath)
	}
	branch := detectBranch(repoRoot)
	cf := configFile{
		RepoPath:             repoRoot,
		Branch:               branch,
		CheckIntervalSeconds: 5,
		PostPullCommand:      "",
		PostPullWorkdir:      "",
		LogFile:              "auto_pull.log",
		NotifyOnPull:         true,
	}
	if err := writeConfig(cfgPath, cf); err != nil {
		return err
	}
	fmt.Printf("Created %s\n", cfgPath)
	fmt.Printf("  repo   : %s\n", repoRoot)
	fmt.Printf("  branch : %s\n", branch)
	fmt.Println()
	fmt.Println("For private repos, set your token in .env:")
	fmt.Println("  echo 'AUTOPULL_TOKEN=ghp_xxxx' >> .env")
	fmt.Println("  echo '.env' >> .gitignore")
	return nil
}

func cmdStatus(cfgPath string) error {
	cfgPath = resolveConfigPath(cfgPath)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	logPath := cfg.LogFile
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(filepath.Dir(cfgPath), logPath)
	}
	pidPath := pidFilePath(cfgPath)
	pid, pidErr := readPID(pidPath)
	alive := pidErr == nil && processAlive(pid)
	state := loadRuntimeState(stateFilePath(cfgPath))

	fmt.Printf("Config  : %s\n", cfgPath)
	if pidErr != nil {
		fmt.Printf("Status  : stopped (no pid file)\n")
	} else {
		status := "stopped"
		if alive {
			status = "running"
		}
		fmt.Printf("Status  : %s (pid %d)\n", status, pid)
	}
	fmt.Printf("Pulls   : %d\n", state.Pulls)
	if !state.LastPull.IsZero() {
		fmt.Printf("Last pull: %s\n", state.LastPull.Format(time.RFC3339))
	}
	if state.ConsecutiveErrors > 0 {
		fmt.Printf("Errors  : %d consecutive\n", state.ConsecutiveErrors)
	}
	if !state.BackoffUntil.IsZero() && time.Now().Before(state.BackoffUntil) {
		fmt.Printf("Backoff : until %s\n", state.BackoffUntil.Format(time.RFC3339))
	}
	if state.LastError != "" {
		fmt.Printf("Last err: %s\n", state.LastError)
	}
	fmt.Printf("Log     : %s\n", logPath)
	return nil
}

func cmdStop(cfgPath string) error {
	cfgPath = resolveConfigPath(cfgPath)
	msg, err := stopProcess(pidFilePath(cfgPath))
	if err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}

func cmdLogs(cfgPath string, lines int) error {
	cfgPath = resolveConfigPath(cfgPath)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	logPath := cfg.LogFile
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(filepath.Dir(cfgPath), logPath)
	}
	out, err := tailFile(logPath, lines)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func cmdDryRun(cfgPath string) error {
	cfgPath = resolveConfigPath(cfgPath)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	fmt.Println("=== dry-run: checking configuration ===")
	fmt.Printf("  config  : %s\n", cfgPath)
	fmt.Printf("  repo    : %s\n", cfg.RepoPath)
	fmt.Printf("  branch  : %s\n", cfg.Branch)
	fmt.Printf("  interval: %ds\n", cfg.CheckIntervalSeconds)
	if cfg.GithubToken != "" {
		fmt.Printf("  token   : (set)\n")
	} else {
		fmt.Printf("  token   : (not set — public repo or SSH assumed)\n")
	}

	fmt.Print("  git repo: ")
	if err := ensureGitRepo(cfg.RepoPath); err != nil {
		fmt.Printf("FAIL — %v\n", err)
		return err
	}
	fmt.Println("OK")

	fmt.Print("  fetch   : ")
	if _, err := runGit(cfg.RepoPath, cfg.GithubToken, "fetch", "origin", cfg.Branch); err != nil {
		fmt.Printf("FAIL — %v\n", err)
		return err
	}
	fmt.Println("OK")

	fmt.Print("  remote  : ")
	remote, err := runGit(cfg.RepoPath, cfg.GithubToken, "rev-parse", fmt.Sprintf("origin/%s", cfg.Branch))
	if err != nil {
		fmt.Printf("FAIL — %v\n", err)
		return err
	}
	fmt.Printf("%s\n", shortHash(remote))

	fmt.Println("=== dry-run passed — ready to run autopull ===")
	return nil
}

// ─────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────

const usage = `Usage: autopull [command] [config_path]

Commands:
  (none)           start watching (default config: ./config_auto_pull.json)
  init             create config_auto_pull.json for the current git repo
  status           show daemon status and stats
  stop             send SIGTERM to the running daemon
  logs [N]         print last N lines of the log (default: 50)
  dry-run          validate config and connectivity without pulling
  --version, -v    print version

Token for private repos:
  Set AUTOPULL_TOKEN or GITHUB_TOKEN in environment or in .env inside repo_path.
  Never put the token in config_auto_pull.json.
`

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		runWatcher("config_auto_pull.json")
		return
	}

	switch args[0] {
	case "--version", "-v":
		fmt.Println("auto_pull", version)
		return

	case "--help", "-h":
		fmt.Print(usage)
		return

	case "init":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdInit(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "init: %v\n", err)
			os.Exit(1)
		}
		return

	case "status":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdStatus(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "status: %v\n", err)
			os.Exit(1)
		}
		return

	case "stop":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdStop(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "stop: %v\n", err)
			os.Exit(1)
		}
		return

	case "logs":
		cfg := ""
		lines := 50
		for _, a := range args[1:] {
			if n, err := strconv.Atoi(a); err == nil {
				lines = n
			} else {
				cfg = a
			}
		}
		if err := cmdLogs(cfg, lines); err != nil {
			fmt.Fprintf(os.Stderr, "logs: %v\n", err)
			os.Exit(1)
		}
		return

	case "dry-run":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdDryRun(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "dry-run: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// backward compatible: treat argument as config path
	runWatcher(args[len(args)-1])
}

func runWatcher(cfgPath string) {
	cfgPath = resolveConfigPath(cfgPath)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Config file not found: %s\n", cfgPath)
		fmt.Fprintln(os.Stderr, "Run 'autopull init' to create one, or see 'autopull --help'")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watch(ctx, cfgPath)
}
