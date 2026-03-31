# autopull

Watches a git repository and runs `git pull` whenever a new commit lands on the tracked branch. Optionally runs a command after each pull.

Pure Go · zero dependencies · Linux & macOS · systemd-ready

---

## Install

Quick installer (auto-detects Linux/macOS, amd64/arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/reinanbr/auto_pull_go/main/install.sh | sh
```

- Downloads the latest release binary and installs to `/usr/local/bin/autopull`.
- Requires `curl` and `git` on `PATH`.

Uninstall:

```bash
curl -fsSL https://raw.githubusercontent.com/reinanbr/auto_pull_go/main/install.sh | sh -s -- uninstall
```

Manual install (from local build artifact):

```bash
./scripts/release-linux.sh v1.1.4
tar -xzf dist/auto_pull_linux_amd64_v1.1.4.tar.gz -C /tmp
cd /tmp/auto_pull_linux_amd64_v1.1.4
sudo ./install.sh
```

Build from source (Go 1.21+):

```bash
go build -o autopull .
```

Then move `autopull` somewhere on `PATH`, e.g. `/usr/local/bin/`.

---

## Quick start

```bash
cd /path/to/your/repo
autopull init        # generates config_auto_pull.json
autopull dry-run     # verify connectivity before running
autopull             # start watching
```

---

## Configuration

`autopull init` creates `config_auto_pull.json` in the current repo.  
Edit it as needed — it is reloaded on every tick, no restart required.

```json
{
  "repo_path": "/srv/myapp",
  "branch": "main",
  "check_interval_seconds": 10,
  "post_pull_command": "systemctl restart myapp",
  "post_pull_workdir": "",
  "log_file": "auto_pull.log",
  "notify_on_pull": false
}
```

| Field | Default | Description |
|---|---|---|
| `repo_path` | — | Absolute path to the local repository *(required)* |
| `branch` | `main` | Branch to track |
| `check_interval_seconds` | `5` | Polling interval |
| `post_pull_command` | — | Shell command to run after each pull |
| `post_pull_workdir` | `repo_path` | Working directory for the post-pull command |
| `log_file` | `auto_pull.log` | Log file path (absolute or relative to config) |
| `notify_on_pull` | `true` | Desktop notification on pull (Linux: `notify-send`, macOS: `osascript`) |

**`github_token` is not a valid field.** Tokens belong in the environment.

---

## Authentication

For private repositories, provide a token via environment variable or `.env` file:

```bash
# environment variable (preferred)
export AUTOPULL_TOKEN=ghp_xxxxxxxxxxxx

# or: .env file in repo_path (never commit this)
echo 'AUTOPULL_TOKEN=ghp_xxxxxxxxxxxx' >> /srv/myapp/.env
echo '.env' >> /srv/myapp/.gitignore
```

Resolution order: `AUTOPULL_TOKEN` → `GITHUB_TOKEN` → `.env` in `repo_path`.  
Tokens set in `config_auto_pull.json` are rejected at startup.

---

## Usage

```
autopull [command] [config]
```

| Command | Description |
|---|---|
| *(none)* | Start the watcher (default config: `./config_auto_pull.json`) |
| `init` | Scaffold `config_auto_pull.json` for the current git repo |
| `status` | Show daemon state: pid, pulls, errors, backoff, last pull |
| `stop` | Send SIGTERM to the running daemon |
| `logs [N]` | Print last N lines of the log (default: 50) |
| `dry-run` | Validate config and test remote connectivity without pulling |
| `--version` | Print version |
| `--help` | Print this reference |

Config path can be passed as the last argument to any command:

```bash
autopull status /etc/auto_pull/config_auto_pull.json
autopull logs 100 /etc/auto_pull/config_auto_pull.json
```

---

## How it works

```
every N seconds
  ├── git fetch origin <branch>
  ├── compare local HEAD with origin/<branch>
  ├── dirty check — skip pull if working tree has uncommitted changes
  ├── if hashes differ → git pull origin <branch>
  └── run post_pull_command via sh -c
```

- **No GitHub API** — uses native `git fetch` + hash comparison  
- **15s timeout** on every git command; failures are logged and backed off  
- **Exponential backoff** on consecutive failures, capped at 5 minutes  
- **Overlapping ticks are skipped** — only one cycle runs at a time  
- **Log rotation** at ~5 MB (`auto_pull.log` → `auto_pull.log.1`); override with `AUTOPULL_LOG_MAX_BYTES`  
- **Token injection** via temporary `GIT_ASKPASS` script; `GIT_TERMINAL_PROMPT=0` prevents interactive prompts  
- **Graceful shutdown** on `SIGINT`/`SIGTERM` — logger is flushed and closed  

---

## Running as a systemd service

After `sudo ./install.sh`, a systemd unit is registered automatically.

```bash
sudo systemctl status autopull
sudo journalctl -u autopull -f
```

The service reads `/etc/auto_pull/config_auto_pull.json`.  
Place your token in `/etc/auto_pull/.env` or set `Environment=AUTOPULL_TOKEN=...` in the unit override:

```bash
sudo systemctl edit autopull
```

```ini
[Service]
Environment=AUTOPULL_TOKEN=ghp_xxxxxxxxxxxx
```

---

## Linux packaging

```bash
# build portable tar.gz
./scripts/release-linux.sh v1.1.4

# install
tar -xzf dist/auto_pull_linux_amd64_v1.1.4.tar.gz -C /tmp
cd /tmp/auto_pull_linux_amd64_v1.1.4
sudo ./install.sh

# uninstall
sudo ./uninstall.sh

# uninstall + remove config and logs
sudo ./uninstall.sh --purge
```

`install.sh` places the binary at `/usr/local/bin/autopull`, writes a default config to `/etc/auto_pull/`, and registers the systemd unit if systemd is available.

---

## Post-pull command examples

```json
"post_pull_command": "systemctl restart myapp"
```
```json
"post_pull_command": "docker compose up -d --build"
```
```json
"post_pull_command": "npm ci --silent && pm2 reload ecosystem.config.js"
```
```json
"post_pull_command": "go build -o bin/app . && ./bin/app"
```

`post_pull_command` is executed via `sh -c` in `post_pull_workdir`. Treat the config as trusted input.

---

## Files created at runtime

| File | Description |
|---|---|
| `.auto_pull.pid` | PID of the running daemon (next to config) |
| `.auto_pull.state.json` | Pull count, last pull time, error state, backoff |
| `auto_pull.log` | Daemon log (path set by `log_file`) |
| `auto_pull.log.1` | Previous log, kept after rotation |

---

## License

MIT