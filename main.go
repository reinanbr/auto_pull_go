package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────

type Config struct {
	RepoPath             string `json:"repo_path"`
	Branch               string `json:"branch"`
	CheckIntervalSeconds int    `json:"check_interval_seconds"`
	GithubToken          string `json:"github_token"`
	PostPullCommand      string `json:"post_pull_command"`
	PostPullWorkdir      string `json:"post_pull_workdir"`
	LogFile              string `json:"log_file"`
	NotifyOnPull         bool   `json:"notify_on_pull"`
}

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

func watch(cfgPath string) {
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

	consecutiveErrors := 0

	for range ticker.C {
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

			local, err := localCommit(cfg.RepoPath)
			if err != nil {
				consecutiveErrors++
				l.errLog(fmt.Sprintf("git rev-parse (local) failed (%dx): %v", consecutiveErrors, err))
				return
			}

			remote, err := remoteCommit(cfg.RepoPath, cfg.Branch, cfg.GithubToken)
			if err != nil {
				consecutiveErrors++
				l.errLog(fmt.Sprintf("git fetch failed (%dx): %v", consecutiveErrors, err))
				return
			}
			consecutiveErrors = 0

			if local == remote {
				return // nada novo
			}

			l.ok(fmt.Sprintf("New commit detected: %s → %s", local[:7], remote[:7]))

			out, err := pull(cfg.RepoPath, cfg.Branch, cfg.GithubToken)
			if err != nil {
				l.errLog(fmt.Sprintf("git pull failed: %v\n%s", err, out))
				return
			}
			l.ok("git pull completed")
			if out != "" {
				l.info("  " + strings.ReplaceAll(out, "\n", "\n  "))
			}

			if cfg.NotifyOnPull {
				notify("auto_pull", fmt.Sprintf("Pull done: %s@%s", filepath.Base(cfg.RepoPath), cfg.Branch))
			}

			if err := runPostCommand(cfg, l); err != nil {
				l.errLog(fmt.Sprintf("post-pull command failed: %v", err))
			} else if cfg.PostPullCommand != "" {
				l.ok("post-pull command completed successfully")
			}
		}()
	}
}

func main() {
	cfgPath := "config_auto_pull.json"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	abs, err := filepath.Abs(cfgPath)
	if err == nil {
		cfgPath = abs
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Config file not found: %s\n", cfgPath)
		fmt.Fprintln(os.Stderr, "Usage: auto_pull [path/to/config_auto_pull.json]")
		os.Exit(1)
	}

	watch(cfgPath)
}
