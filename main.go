package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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

var version = "v1.0.5"

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

	if cfg.GithubToken == "" {
		if envTok := os.Getenv("AUTOPULL_TOKEN"); envTok != "" {
			cfg.GithubToken = envTok
		}
	}
	return &cfg, nil
}

// ─────────────────────────────────────────────
// Logger
// ─────────────────────────────────────────────

type Logger struct {
	file   *os.File
	logger *log.Logger
}

func newLogger(logPath string) (*Logger, error) {
	if err := rotateIfLarge(logPath, 5*1024*1024); err != nil {
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
	l.info("          auto_pull started")
	l.info(fmt.Sprintf("  repo    : %s", cfg.RepoPath))
	l.info(fmt.Sprintf("  branch  : %s", cfg.Branch))
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

			repos := buildRepos(cfg)
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

				processRepo(repo, cfg.GithubToken, l, st)
			}
		}()
	}
}

type repoState struct {
	consecutiveErrors int
	backoffUntil      time.Time
}

type RepoWork struct {
	RepoPath        string
	Branch          string
	PostPullCommand string
	PostPullWorkdir string
	NotifyOnPull    bool
}

func buildRepos(cfg *Config) []RepoWork {
	if len(cfg.Repos) == 0 {
		return []RepoWork{singleRepoFromLegacy(cfg)}
	}

	repos := make([]RepoWork, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		branch := r.Branch
		if branch == "" {
			branch = "main"
		}
		repos = append(repos, RepoWork{
			RepoPath:        r.RepoPath,
			Branch:          branch,
			PostPullCommand: r.PostPullCommand,
			PostPullWorkdir: r.PostPullWorkdir,
			NotifyOnPull:    r.NotifyOnPull,
		})
	}
	return repos
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

func processRepo(repo RepoWork, token string, l *Logger, st *repoState) {
	local, err := localCommit(repo.RepoPath)
	if err != nil {
		st.consecutiveErrors++
		st.backoffUntil = time.Now().Add(backoffDuration(st.consecutiveErrors))
		l.errLog(fmt.Sprintf("%s: git rev-parse (local) failed (%dx): %v", repo.RepoPath, st.consecutiveErrors, err))
		return
	}

	remote, err := remoteCommit(repo.RepoPath, repo.Branch, token)
	if err != nil {
		st.consecutiveErrors++
		st.backoffUntil = time.Now().Add(backoffDuration(st.consecutiveErrors))
		l.errLog(fmt.Sprintf("%s: git fetch failed (%dx): %v", repo.RepoPath, st.consecutiveErrors, err))
		return
	}
	st.consecutiveErrors = 0
	st.backoffUntil = time.Time{}

	if local == remote {
		return
	}

	l.ok(fmt.Sprintf("%s: new commit detected: %s → %s", repo.RepoPath, local[:7], remote[:7]))

	out, err := pull(repo.RepoPath, repo.Branch, token)
	if err != nil {
		l.errLog(fmt.Sprintf("%s: git pull failed: %v\n%s", repo.RepoPath, err, out))
		return
	}
	l.ok(fmt.Sprintf("%s: git pull completed", repo.RepoPath))
	if out != "" {
		l.info("  " + strings.ReplaceAll(out, "\n", "\n  "))
	}

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

func main() {
	cfgPath := "config_auto_pull.json"
	args := os.Args[1:]
	for _, a := range args {
		if a == "--version" || a == "-v" {
			fmt.Println("auto_pull", version)
			return
		}
	}
	if len(args) > 0 {
		cfgPath = args[len(args)-1]
	}

	abs, err := filepath.Abs(cfgPath)
	if err == nil {
		cfgPath = abs
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Config file not found: %s\n", cfgPath)
		fmt.Fprintln(os.Stderr, "Usage: auto_pull [--version] [path/to/config_auto_pull.json]")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watch(ctx, cfgPath)
}
