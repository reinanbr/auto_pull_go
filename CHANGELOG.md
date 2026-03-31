# Changelog

All notable changes to this project will be documented in this file.

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
