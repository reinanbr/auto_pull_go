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

## [v1.0.2] - 2026-03-31

### Added
- Multiple repositories support via `repos` array in config.
- Exponential backoff (capped) per repositório em falhas de git.
- Env token fallback: usa `AUTOPULL_TOKEN` quando `github_token` está vazio.
- Log rotation simples em ~5MB (`log_file.1`).

### Changed
- README documenta limitação de OS (Linux/macOS), multi-repo, backoff, log rotation e token por ambiente.

## [v1.0.3] - 2026-03-31

### Added
- Flag `--version` no binário.
- Tratamento de sinais (SIGINT/SIGTERM) para shutdown limpo.
- Exemplo de config no root: `config_auto_pull.example.json` e `config_auto_pull.json` ignorado no git.
- Workflow CI (GitHub Actions) com gofmt, vet, test, build.

### Changed
- README documenta suporte de OS, CI, multi-repo e config exemplo no root.

## [v1.0.4] - 2026-03-31

### Added
- run.sh: flag `--config` e suporte a caminho posicional de config.

### Changed
- run.sh: daemon não redireciona log (evita duplicação); stop espera término com timeout.
- go.mod: módulo agora é `github.com/reinanbr/auto_pull_go`.
