#!/usr/bin/env bash
set -euo pipefail

APP_NAME="auto_pull"
BIN_DEST="/usr/local/bin/${APP_NAME}"
CMD_NAME="autopull"
CMD_DEST="/usr/local/bin/${CMD_NAME}"
CFG_DIR="/etc/${APP_NAME}"
CFG_FILE="${CFG_DIR}/config_auto_pull.json"
LOG_DIR="/var/log/${APP_NAME}"
LOG_FILE="${LOG_DIR}/auto_pull.log"
SYSTEMD_UNIT="/etc/systemd/system/${APP_NAME}.service"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="${SCRIPT_DIR}/${APP_NAME}"
CFG_EXAMPLE="${SCRIPT_DIR}/config_auto_pull.example.json"
UNIT_SRC="${SCRIPT_DIR}/${APP_NAME}.service"

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

  if [[ ! -f "${BIN_SRC}" ]]; then
    echo "[ERROR] missing binary: ${BIN_SRC}" >&2
    echo "Build first (go build -o packaging/linux/auto_pull main.go) or use release bundle." >&2
    exit 1
  fi

  if [[ ! -f "${CFG_EXAMPLE}" ]]; then
    echo "[ERROR] missing config example: ${CFG_EXAMPLE}" >&2
    exit 1
  fi

  install -Dm755 "${BIN_SRC}" "${BIN_DEST}"
  cat > "${CMD_DEST}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

BIN="/usr/local/bin/auto_pull"
LOCAL_CFG="$(pwd)/config_auto_pull.json"
GLOBAL_CFG="/etc/auto_pull/config_auto_pull.json"

if [[ "$#" -gt 0 ]]; then
  exec "${BIN}" "$@"
fi

if [[ -f "${LOCAL_CFG}" ]]; then
  exec "${BIN}" "${LOCAL_CFG}"
fi

if [[ -f "${GLOBAL_CFG}" ]]; then
  exec "${BIN}" "${GLOBAL_CFG}"
fi

echo "[ERROR] config_auto_pull.json not found in current directory or /etc/auto_pull" >&2
echo "Usage: autopull [path/to/config_auto_pull.json]" >&2
exit 1
EOF
  chmod 0755 "${CMD_DEST}"
  install -d "${CFG_DIR}" "${LOG_DIR}"

  if [[ ! -f "${CFG_FILE}" ]]; then
    install -Dm644 "${CFG_EXAMPLE}" "${CFG_FILE}"
    echo "[INFO] created default config: ${CFG_FILE}"
  else
    echo "[INFO] keeping existing config: ${CFG_FILE}"
  fi

  touch "${LOG_FILE}"
  chmod 0644 "${LOG_FILE}"

  if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
    if [[ -f "${UNIT_SRC}" ]]; then
      install -Dm644 "${UNIT_SRC}" "${SYSTEMD_UNIT}"
      systemctl daemon-reload
      systemctl enable --now "${APP_NAME}.service"
      echo "[OK] systemd service enabled: ${APP_NAME}.service"
    else
      echo "[WARN] ${UNIT_SRC} not found; skipping systemd unit install"
    fi
  else
    echo "[WARN] systemd not detected; run manually: ${BIN_DEST} ${CFG_FILE}"
  fi

  echo "[OK] installation complete"
  echo "[INFO] binary: ${BIN_DEST}"
  echo "[INFO] command: ${CMD_DEST}"
  echo "[INFO] config: ${CFG_FILE}"
  echo "[INFO] log: ${LOG_FILE}"
}

main "$@"
