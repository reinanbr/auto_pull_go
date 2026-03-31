#!/usr/bin/env bash
set -euo pipefail

APP_NAME="auto_pull"
BIN_DEST="/usr/local/bin/${APP_NAME}"
CMD_DEST="/usr/local/bin/autopull"
CFG_DIR="/etc/${APP_NAME}"
LOG_DIR="/var/log/${APP_NAME}"
SYSTEMD_UNIT="/etc/systemd/system/${APP_NAME}.service"
PURGE="${1:-}"

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
      exec sudo bash "$0" "$@"
    fi
    echo "[ERROR] run as root or install sudo" >&2
    exit 1
  fi
}

main() {
  require_root "$@"

  if command -v systemctl >/dev/null 2>&1 && [[ -f "${SYSTEMD_UNIT}" ]]; then
    systemctl disable --now "${APP_NAME}.service" >/dev/null 2>&1 || true
    rm -f "${SYSTEMD_UNIT}"
    systemctl daemon-reload
    echo "[OK] removed systemd unit"
  fi

  rm -f "${BIN_DEST}"
  echo "[OK] removed binary: ${BIN_DEST}"
  rm -f "${CMD_DEST}"
  echo "[OK] removed command: ${CMD_DEST}"

  if [[ "${PURGE}" == "--purge" ]]; then
    rm -rf "${CFG_DIR}" "${LOG_DIR}"
    echo "[OK] removed config/log directories"
  else
    echo "[INFO] keeping config/log directories"
    echo "[INFO] use --purge to remove: ${CFG_DIR} ${LOG_DIR}"
  fi
}

main "$@"
