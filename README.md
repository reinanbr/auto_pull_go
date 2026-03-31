# auto_pull

A daemon that monitors a GitHub repository and runs `git pull` + a custom command whenever a new commit is pushed.

---

## Files

```
auto_pull/
├── main.go                  ← main source code (Go)
├── go.mod                   ← Go module file
├── run.sh                   ← build / process manager script
└── config_auto_pull.json    ← your configuration
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

```bash
chmod +x run.sh

# Run in the foreground (live logs)
./run.sh

# Run as a background daemon
./run.sh --daemon

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
