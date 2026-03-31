# auto_pull

A daemon that monitors a GitHub repository and runs `git pull` + a custom command whenever a new commit is pushed.

Linux-first and distro-agnostic: you can distribute it as a portable `tar.gz` containing the binary + installer.

After installation, the global command is `autopull`.

OS support today: Linux and macOS (notifications). Windows is not supported in this build.

CI: GitHub Actions runs gofmt, vet, test, and build on pushes/PRs.

---

## Files

```
auto_pull/
├── main.go                  ← main source code (Go)
├── go.mod                   ← Go module file
├── run.sh                   ← build / process manager script
├── config_auto_pull.example.json
└── (local) config_auto_pull.json is ignored — keep your real config out of git
```
---

## Requirements

- [Go 1.21+](https://go.dev/dl/)
- `git` installed and available on PATH

```bash
# Ubuntu / Debian
sudo apt install golang-go git

# macOS
brew install go git
```

---

## Configuration (`config_auto_pull.json`)

Single repo (legacy, still supported):

```json
{
  "repo_path": "/path/to/your/repository",
  "branch": "main",
  "check_interval_seconds": 5,
  "github_token": "",
  "post_pull_command": "./deploy.sh",
  "post_pull_workdir": "",
  "log_file": "auto_pull.log",
  "notify_on_pull": true
}
```

Multiple repos (new):

```json
{
  "check_interval_seconds": 5,
  "log_file": "auto_pull.log",
  "repos": [
    {
      "repo_path": "/path/one",
      "branch": "main",
      "post_pull_command": "",
      "notify_on_pull": true
    },
    {
      "repo_path": "/path/two",
      "branch": "develop",
      "post_pull_command": "make deploy",
      "post_pull_workdir": "/path/two",
      "notify_on_pull": false
    }
  ]
}
```

Token from environment: if `github_token` is empty, it is read from `AUTOPULL_TOKEN`.

| Field | Description | Default |
|---|---|---|
| `repo_path` | Absolute path to the local repository | **required** |
| `branch` | Branch to monitor | `main` |
| `check_interval_seconds` | Polling interval in seconds | `5` |
| `github_token` | GitHub OAuth token (for private repos) | empty |
| `post_pull_command` | Command to run after each pull | empty |
| `post_pull_workdir` | Working directory for the post-pull command | `repo_path` |
| `log_file` | Path to the log file | `auto_pull.log` |
| `notify_on_pull` | Send a desktop notification on pull | `true` |

> **Tip**: The config file is re-read on every tick — you can edit it without restarting the daemon.

---

## Usage

### Local development

```bash
chmod +x run.sh

# Run in the foreground (live logs)
./run.sh [--config path/to/config.json]

# Run as a background daemon
./run.sh --daemon [--config path/to/config.json]

# Stop the daemon
./run.sh --stop

# Check status
./run.sh --status

# Compile only
./run.sh --build
```

Or directly with Go:

```bash
go build -o auto_pull main.go
./auto_pull                               # uses local config_auto_pull.json
./auto_pull /other/path/config.json
./auto_pull --version
```

### Global command (installed)

```bash
autopull
autopull /path/to/config_auto_pull.json
autopull --version
```

---

## Distro-agnostic Linux distribution

This repository now includes a portable Linux packaging structure:

```
packaging/linux/
├── auto_pull.service
├── config_auto_pull.example.json
├── install.sh
└── uninstall.sh

scripts/
└── release-linux.sh
```

### Build portable release (`tar.gz`)

```bash
./scripts/release-linux.sh v1.0.5
```

Output example:

```bash
dist/auto_pull_linux_amd64_v1.0.5.tar.gz
```

### Install on any Linux distro

```bash
tar -xzf dist/auto_pull_linux_amd64_v1.0.0.tar.gz -C /tmp
cd /tmp/auto_pull_linux_amd64_v1.0.5
sudo ./install.sh
```

What installer does:

- installs binary in `/usr/local/bin/auto_pull`
- creates global command `/usr/local/bin/autopull`
- creates config in `/etc/auto_pull/config_auto_pull.json` (if missing)
- creates log file in `/var/log/auto_pull/auto_pull.log`
- installs and enables systemd unit when systemd is available

### Global command from any folder

After install, use:

```bash
autopull
```

Resolution order when you run `autopull` without arguments:

- `./config_auto_pull.json` (current folder)
- `/etc/auto_pull/config_auto_pull.json` (global fallback)

Important:

- `autopull` in terminal uses current folder first.
- the `systemd` service always uses `/etc/auto_pull/config_auto_pull.json`.

You can also pass an explicit config path:

```bash
autopull /path/to/config_auto_pull.json
```

### Uninstall

```bash
sudo ./uninstall.sh
```

Remove config/log folders too:

```bash
sudo ./uninstall.sh --purge
```

---

## How it works

```
every N seconds:
  ├─ git fetch origin <branch>      (does not touch working tree)
  ├─ compare local HEAD vs origin/<branch>
  ├─ if different → git pull origin <branch>
  └─ run post_pull_command via sh -c
```

- Uses `git fetch` + hash comparison — no GitHub API polling required
- Zero external dependencies — pure Go stdlib
- Dual logging: file + stdout simultaneously
- Optional desktop notifications (Linux: `notify-send`, macOS: `osascript`)
- Config is reloaded on every tick — no restart needed for changes
- Backoff: git failures use exponential backoff (cap 5m) per repo.
- Log rotation: built-in rotation around 5MB (`log_file` → `log_file.1`).

- Git credentials: token is injected via a temporary `GIT_ASKPASS` script and `GIT_TOKEN` env var (no `GIT_PASSWORD` in env; prompt disabled with `GIT_TERMINAL_PROMPT=0`). If `github_token` estiver vazio, é lido de `AUTOPULL_TOKEN`.
- Timeouts: all git commands run with a 15s timeout; failures are logged.
- Concurrency: only one pull cycle runs at a time; overlapping ticks are skipped with a warning.
- Backoff: git failures use exponential backoff (cap 5m) per repo.
- post_pull_command: executed via `sh -c` — treat the config as trusted input. If untrusted, avoid enabling `post_pull_command` or wrap with your own validation.
- Log rotation: built-in rotation around 5MB (`log_file` → `log_file.1`). For production, you can also wire logrotate.
- Signals: SIGINT/SIGTERM are trapped for graceful shutdown (logger is closed).

---

## Quick start (Linux)

```bash
./scripts/release-linux.sh v1.0.1
tar -xzf dist/auto_pull_linux_amd64_v1.0.1.tar.gz -C /tmp
cd /tmp/auto_pull_linux_amd64_v1.0.1
sudo ./install.sh
```

Then edit `/etc/auto_pull/config_auto_pull.json` or run from your project folder with local config:

```bash
cd /path/to/your/project
autopull
```

---

## Private repositories

Generate a token at **GitHub → Settings → Developer settings → Personal access tokens** with the `repo` scope, then set it in `github_token`.

---

## Post-pull command examples

```json
"post_pull_command": "npm install --silent && pm2 restart app"
```

```json
"post_pull_command": "docker compose up -d --build"
```

```json
"post_pull_command": "systemctl restart my-service"
```

```json
"post_pull_command": "go build -o bin/app . && ./bin/app"
```
