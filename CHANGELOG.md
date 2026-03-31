# Changelog

All notable changes to this project will be documented in this file.

## [v1.1.3] - 2026-04-01

### Added
- CI now triggers on tags (`v*`) and publishes release binaries automatically; manual dispatch supported.

### Changed
- Version bumped to v1.1.3.

## [v1.1.2] - 2026-04-01

### Added
- Installer now falls back to `go install github.com/reinanbr/auto_pull_go@<version>` when a release binary is missing, avoiding 404s.

### Changed
- Version bumped to v1.1.2.

## [v1.1.1] - 2026-04-01

### Added
- Single-command installer via curl (`install.sh`) that auto-detects Linux/macOS and downloads the latest release binary to `/usr/local/bin/autopull`.
- Uninstall flag in the installer: `sh -s -- uninstall` removes the installed binary.

### Changed
- README documents the curl installer/uninstaller alongside manual and source install flows.

## [v1.1.0] - 2026-04-01

### Added
- `autopull dry-run` validates config and connectivity without pulling.

### Changed
- Config validation forbids `github_token` in JSON; tokens must come from environment variables or `.env`. Docs and examples updated accordingly.
- CLI docs now match runtime state (status reports pid, pulls, errors/backoff, log path).

## [v1.0.8] - 2026-03-31

### Added
- New CLI subcommands: `autopull init` (generate config in current git repo), `status` (pid + pulls + backoff + errors), `stop` (terminate daemon via pid), `logs` (tail recent log lines).
- Runtime state persisted to `.auto_pull.state.json` (pull count, bytes transferred, last pull, consecutive errors, backoff).

### Changed
- The watcher writes its PID to `.auto_pull.pid` (alongside the config) for status/stop commands.

## [v1.0.7] - 2026-03-31

### Added
- Token can also be supplied via `GITHUB_TOKEN` (env or .env), alongside `AUTOPULL_TOKEN`.

### Changed
- Docs updated to reflect env-first token flow and `.env` usage; JSON token remains legacy.

## [v1.0.6] - 2026-03-31

### Added
- Token loading from `.env` (AUTOPULL_TOKEN) in the repo directory; env wins over JSON.
- Log rotation size configurable via env `AUTOPULL_LOG_MAX_BYTES` (default ~5MB).
- Repo validation at startup to fail fast if the path is not a git repo.

### Changed
- Dirty working tree now skips pull with a clear warning (no accidental stash/pop).
- Safer hash rendering avoids panics on short hashes.
- Backoff hardens after repeated failures (caps to 5 minutes after 5 errors).
- Multi-repo configs are deprecated; only the first entry is processed when present.

## [v1.0.5] - 2026-03-31

### Added
- Release builds now embed the version string via `-X main.version`.
- Default binary version set to `v1.0.5` for tagged builds.

## [v1.0.0] - 2026-03-31

### Added
- MIT license file.
- Base `.gitignore` for Go and local development artifacts.
- Distro-agnostic Linux packaging structure under `packaging/linux/`.
- Portable release builder script at `scripts/release-linux.sh`.
- Example Linux systemd unit: `packaging/linux/auto_pull.service`.
- Linux install script: `packaging/linux/install.sh`.
- Linux uninstall script: `packaging/linux/uninstall.sh`.
- Example config for Linux install flow: `packaging/linux/config_auto_pull.example.json`.
- Global command wrapper `autopull` (installed at `/usr/local/bin/autopull`) to run from any folder.

### Changed
- Updated `README.md` with Linux distro-agnostic distribution and installation flow.
- Added global command usage docs (`autopull`) and config resolution behavior.
- Marked `run.sh` as executable.

### Notes
- `autopull` resolves config in this order when run without arguments:
  1. `./config_auto_pull.json` (current directory)
  2. `/etc/auto_pull/config_auto_pull.json` (fallback)
- systemd service uses `/etc/auto_pull/config_auto_pull.json`.

## [v1.0.2] - 2026-03-31

### Added
- Multiple repositories support via `repos` array in config.
- Exponential backoff (capped) per repository on git failures.
- Env token fallback: uses `AUTOPULL_TOKEN` when `github_token` is empty.
- Simple log rotation around ~5MB (`log_file.1`).

### Changed
- README documents OS limitation (Linux/macOS), multi-repo, backoff, log rotation, and env token.

## [v1.0.3] - 2026-03-31

### Added
- `--version` flag in the binary.
- Signal handling (SIGINT/SIGTERM) for graceful shutdown.
- Config example in root: `config_auto_pull.example.json`; `config_auto_pull.json` ignored by git.
- CI workflow (GitHub Actions) running gofmt, vet, test, build.

### Changed
- README documents OS support, CI, multi-repo, and root config example.

## [v1.0.4] - 2026-03-31

### Added
- run.sh: `--config` flag and positional config path support.

### Changed
- run.sh: daemon no longer redirects logs (avoids duplication); `--stop` waits for termination with timeout.
- go.mod: module is now `github.com/reinanbr/auto_pull_go`.
