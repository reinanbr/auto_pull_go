# Changelog

All notable changes to this project will be documented in this file.

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
