package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ─────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────

type Config struct {
	RepoPath             string       `json:"repo_path"`
	Branch               string       `json:"branch"`
	CheckIntervalSeconds int          `json:"check_interval_seconds"`
	GithubToken          string       `json:"github_token"`
	PostPullCommand      string       `json:"post_pull_command"`
	PostPullWorkdir      string       `json:"post_pull_workdir"`
	LogFile              string       `json:"log_file"`
	NotifyOnPull         bool         `json:"notify_on_pull"`
	Repos                []RepoConfig `json:"repos"`
}

type RepoConfig struct {
	RepoPath        string `json:"repo_path"`
	Branch          string `json:"branch"`
	PostPullCommand string `json:"post_pull_command"`
	PostPullWorkdir string `json:"post_pull_workdir"`
	NotifyOnPull    bool   `json:"notify_on_pull"`
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
		val := strings.TrimSpace(parts[1])
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

var version = "v1.0.8"
var multiRepoWarned bool

const gitTimeout = 15 * time.Second

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
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

	// token resolution: prefer env (.env), then JSON (deprecated)
	token := tokenFromEnv()
	if token == "" {
		baseDir := cfg.RepoPath
		if baseDir == "" {
			baseDir = filepath.Dir(path)
		}
		token = loadDotEnvToken(baseDir)
	}
	if token == "" {
		token = cfg.GithubToken // legacy fallback
	}
	cfg.GithubToken = token
	return &cfg, nil
}

func writeConfig(path string, cfg Config) error {
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
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	return nil
}

func getLogMaxBytes() int64 {
	const defaultSize = int64(5 * 1024 * 1024)
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
		return strings.TrimSpace(string(out)), fmt.Errorf("git command timed out: %w", err)
	}

	return strings.TrimSpace(string(out)), err
}

func createAskPassScript() (string, func(), error) {
	f, err := os.CreateTemp("", "autopull-askpass-*")
	if err != nil {
		return "", func() {}, err
	}

	script := "#!/usr/bin/env bash\nprintf \"%s\" \"$GIT_TOKEN\"\n"
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

// localCommit returns the current HEAD hash
func localCommit(dir string) (string, error) {
	return runGit(dir, "", "rev-parse", "HEAD")
}

// remoteCommit fetches and returns the remote HEAD hash (without merging)
func remoteCommit(dir, branch, token string) (string, error) {
	// fetch silently
	if _, err := runGit(dir, token, "fetch", "origin", branch); err != nil {
		return "", fmt.Errorf("git fetch failed: %w", err)
	}
	return runGit(dir, token, "rev-parse", fmt.Sprintf("origin/%s", branch))
}

// pull executes git pull
func pull(dir, branch, token string) (string, error) {
	return runGit(dir, token, "pull", "origin", branch)
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
// Notification (desktop — opcional)
// ─────────────────────────────────────────────

func notify(title, body string) {
	// tenta notify-send (Linux) ou osascript (macOS)
	if err := exec.Command("notify-send", title, body).Run(); err != nil {
		_ = exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "%s"`, body, title)).Run()
	}
}

// ─────────────────────────────────────────────
// Main loop
// ─────────────────────────────────────────────

func watch(ctx context.Context, cfgPath string) {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	primaryRepo := cfg.RepoPath
	if primaryRepo == "" && len(cfg.Repos) > 0 {
		primaryRepo = cfg.Repos[0].RepoPath
	}
	if err := ensureGitRepo(primaryRepo); err != nil {
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

	displayRepo := primaryRepo
	displayBranch := cfg.Branch
	if len(cfg.Repos) > 0 && cfg.Repos[0].Branch != "" {
		displayBranch = cfg.Repos[0].Branch
	}

	l.info("═══════════════════════════════════════════")
	l.info("          auto_pull started")
	l.info(fmt.Sprintf("  repo    : %s", displayRepo))
	l.info(fmt.Sprintf("  branch  : %s", displayBranch))
	l.info(fmt.Sprintf("  interval: %ds", cfg.CheckIntervalSeconds))
	l.info(fmt.Sprintf("  log     : %s", logPath))
	if cfg.PostPullCommand != "" {
		l.info(fmt.Sprintf("  post-pull: %s", cfg.PostPullCommand))
	}
	l.info("═══════════════════════════════════════════")

	interval := time.Duration(cfg.CheckIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	running := false
	state := map[string]*repoState{}

	for {
		select {
		case <-ctx.Done():
			l.info("shutting down (signal)")
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

			// re-read config on every tick so user can change it without restart
			newCfg, err := loadConfig(cfgPath)
			if err != nil {
				l.warn(fmt.Sprintf("Invalid config, keeping previous: %v", err))
			} else {
				cfg = newCfg
			}

			repos := buildRepos(cfg, l)
			now := time.Now()

			for _, repo := range repos {
				st := state[repo.RepoPath]
				if st == nil {
					st = &repoState{}
					state[repo.RepoPath] = st
				}

				if now.Before(st.backoffUntil) {
					l.warn(fmt.Sprintf("backing off %s until %s", repo.RepoPath, st.backoffUntil.Format(time.RFC3339)))
					continue
				}

				processRepo(repo, cfg.GithubToken, l, st, runtimeState)
				runtimeState.ConsecutiveErrors = st.consecutiveErrors
				runtimeState.BackoffUntil = st.backoffUntil
			}

			if err := saveRuntimeState(statePath, runtimeState); err != nil {
				l.warn(fmt.Sprintf("could not persist state: %v", err))
			}
		}()
	}
}

type repoState struct {
	consecutiveErrors int
	backoffUntil      time.Time
}

type RuntimeState struct {
	Pulls             int       `json:"pulls"`
	BytesTransferred  int64     `json:"bytes_transferred"`
	LastPull          time.Time `json:"last_pull"`
	ConsecutiveErrors int       `json:"consecutive_errors"`
	BackoffUntil      time.Time `json:"backoff_until"`
	LastError         string    `json:"last_error"`
}

func pidFilePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), ".auto_pull.pid")
}

func stateFilePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), ".auto_pull.state.json")
}

func shortHash(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

func ensureGitRepo(path string) error {
	if path == "" {
		return fmt.Errorf("repo_path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("repo_path not accessible: %w", err)
	}
	if _, err := runGit(path, "", "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("not a git repo: %w", err)
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

type RepoWork struct {
	RepoPath        string
	Branch          string
	PostPullCommand string
	PostPullWorkdir string
	NotifyOnPull    bool
}

func buildRepos(cfg *Config, l *Logger) []RepoWork {
	if len(cfg.Repos) == 0 {
		return []RepoWork{singleRepoFromLegacy(cfg)}
	}

	if l != nil && !multiRepoWarned {
		l.warn("multi-repo config is deprecated; processing only the first entry")
		multiRepoWarned = true
	}

	r := cfg.Repos[0]
	branch := r.Branch
	if branch == "" {
		branch = "main"
	}
	return []RepoWork{{
		RepoPath:        r.RepoPath,
		Branch:          branch,
		PostPullCommand: r.PostPullCommand,
		PostPullWorkdir: r.PostPullWorkdir,
		NotifyOnPull:    r.NotifyOnPull,
	}}
}

func singleRepoFromLegacy(cfg *Config) RepoWork {
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}
	return RepoWork{
		RepoPath:        cfg.RepoPath,
		Branch:          branch,
		PostPullCommand: cfg.PostPullCommand,
		PostPullWorkdir: cfg.PostPullWorkdir,
		NotifyOnPull:    cfg.NotifyOnPull,
	}
}

func processRepo(repo RepoWork, token string, l *Logger, st *repoState, rs *RuntimeState) {
	local, err := localCommit(repo.RepoPath)
	if err != nil {
		st.consecutiveErrors++
		delay := backoffDuration(st.consecutiveErrors)
		if st.consecutiveErrors >= 5 {
			delay = 5 * time.Minute
		}
		st.backoffUntil = time.Now().Add(delay)
		rs.ConsecutiveErrors = st.consecutiveErrors
		rs.BackoffUntil = st.backoffUntil
		rs.LastError = err.Error()
		l.errLog(fmt.Sprintf("%s: git rev-parse (local) failed (%dx): %v", repo.RepoPath, st.consecutiveErrors, err))
		return
	}

	if isRepoDirty(repo.RepoPath) {
		l.warn(fmt.Sprintf("%s: working tree has uncommitted changes; skipping pull", repo.RepoPath))
		return
	}

	remote, err := remoteCommit(repo.RepoPath, repo.Branch, token)
	if err != nil {
		st.consecutiveErrors++
		delay := backoffDuration(st.consecutiveErrors)
		if st.consecutiveErrors >= 5 {
			delay = 5 * time.Minute
		}
		st.backoffUntil = time.Now().Add(delay)
		rs.ConsecutiveErrors = st.consecutiveErrors
		rs.BackoffUntil = st.backoffUntil
		rs.LastError = err.Error()
		l.errLog(fmt.Sprintf("%s: git fetch failed (%dx): %v", repo.RepoPath, st.consecutiveErrors, err))
		return
	}
	st.consecutiveErrors = 0
	st.backoffUntil = time.Time{}
	rs.ConsecutiveErrors = 0
	rs.BackoffUntil = time.Time{}
	rs.LastError = ""

	if local == remote {
		rs.ConsecutiveErrors = 0
		rs.BackoffUntil = time.Time{}
		rs.LastError = ""
		return
	}

	l.ok(fmt.Sprintf("%s: new commit detected: %s → %s", repo.RepoPath, shortHash(local), shortHash(remote)))

	out, err := pull(repo.RepoPath, repo.Branch, token)
	if err != nil {
		l.errLog(fmt.Sprintf("%s: git pull failed: %v\n%s", repo.RepoPath, err, out))
		return
	}
	l.ok(fmt.Sprintf("%s: git pull completed", repo.RepoPath))
	if out != "" {
		l.info("  " + strings.ReplaceAll(out, "\n", "\n  "))
	}
	if bytes := parseBytesTransferred(out); bytes > 0 {
		rs.BytesTransferred += bytes
	}
	rs.Pulls++
	rs.LastPull = time.Now()

	if repo.NotifyOnPull {
		notify("auto_pull", fmt.Sprintf("Pull done: %s@%s", filepath.Base(repo.RepoPath), repo.Branch))
	}

	repoCfg := Config{
		RepoPath:        repo.RepoPath,
		PostPullCommand: repo.PostPullCommand,
		PostPullWorkdir: repo.PostPullWorkdir,
	}
	if err := runPostCommand(&repoCfg, l); err != nil {
		l.errLog(fmt.Sprintf("%s: post-pull command failed: %v", repo.RepoPath, err))
	} else if repo.PostPullCommand != "" {
		l.ok(fmt.Sprintf("%s: post-pull command completed successfully", repo.RepoPath))
	}
}

func backoffDuration(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	// exponential backoff with cap at 5 minutes
	base := time.Second
	d := base << (failures - 1)
	max := 5 * time.Minute
	if d > max {
		return max
	}
	return d
}

func parseBytesTransferred(out string) int64 {
	re := regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(kib|kb|mib|mb|gib|gb)`) // best-effort
	m := re.FindStringSubmatch(out)
	if len(m) < 3 {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "kb":
		return int64(val * 1_000)
	case "kib":
		return int64(val * 1024)
	case "mb":
		return int64(val * 1_000_000)
	case "mib":
		return int64(val * 1024 * 1024)
	case "gb":
		return int64(val * 1_000_000_000)
	case "gib":
		return int64(val * 1024 * 1024 * 1024)
	}
	return 0
}

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
	cfg := Config{
		RepoPath:             repoRoot,
		Branch:               branch,
		CheckIntervalSeconds: 5,
		GithubToken:          "",
		PostPullCommand:      "",
		PostPullWorkdir:      "",
		LogFile:              "auto_pull.log",
		NotifyOnPull:         true,
	}
	if err := writeConfig(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Created %s for repo %s (branch %s)\n", cfgPath, repoRoot, branch)
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

	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Printf("PID file: %s\n", pidPath)
	if pidErr != nil {
		fmt.Printf("Status: no pid file (%v)\n", pidErr)
	} else {
		status := "stopped"
		if alive {
			status = "running"
		}
		fmt.Printf("Status: %s (pid %d)\n", status, pid)
	}
	fmt.Printf("Pulls: %d\n", state.Pulls)
	fmt.Printf("Bytes transferred: %d\n", state.BytesTransferred)
	if !state.LastPull.IsZero() {
		fmt.Printf("Last pull: %s\n", state.LastPull.Format(time.RFC3339))
	}
	if state.ConsecutiveErrors > 0 {
		fmt.Printf("Consecutive errors: %d\n", state.ConsecutiveErrors)
	}
	if !state.BackoffUntil.IsZero() {
		fmt.Printf("Backoff until: %s\n", state.BackoffUntil.Format(time.RFC3339))
	}
	if state.LastError != "" {
		fmt.Printf("Last error: %s\n", state.LastError)
	}
	fmt.Printf("Log: %s\n", logPath)
	fmt.Printf("State: %s\n", stateFilePath(cfgPath))
	return nil
}

func cmdStop(cfgPath string) error {
	cfgPath = resolveConfigPath(cfgPath)
	pidPath := pidFilePath(cfgPath)
	msg, err := stopProcess(pidPath)
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
	case "init":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdInit(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "init error: %v\n", err)
			os.Exit(1)
		}
		return
	case "status":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdStatus(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "status error: %v\n", err)
			os.Exit(1)
		}
		return
	case "stop":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdStop(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "stop error: %v\n", err)
			os.Exit(1)
		}
		return
	case "logs":
		cfg := ""
		if len(args) > 1 {
			cfg = args[1]
		}
		if err := cmdLogs(cfg, 100); err != nil {
			fmt.Fprintf(os.Stderr, "logs error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Backward compatible: treat last argument as config path
	cfgPath := args[len(args)-1]
	runWatcher(cfgPath)
}

func runWatcher(cfgPath string) {
	cfgPath = resolveConfigPath(cfgPath)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Config file not found: %s\n", cfgPath)
		fmt.Fprintln(os.Stderr, "Usage: auto_pull [--version] [init|status|stop|logs] [path/to/config_auto_pull.json]")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watch(ctx, cfgPath)
}
