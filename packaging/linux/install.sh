#!/bin/sh
# autopull — bundle installer
# Installs the autopull binary from an extracted release bundle.
#
# Usage:
#   sudo ./install.sh              # install
#   sudo ./install.sh --dry-run   # preview what would be done
#   sudo ./install.sh --help      # show this help
#   sudo ./install.sh --purge     # remove all installed files and dirs

set -e

# ─── paths ─────────────────────────────────────────────────

APP_NAME="auto_pull"
CMD_NAME="autopull"
BIN_DEST="/usr/local/bin/${APP_NAME}"
CMD_DEST="/usr/local/bin/${CMD_NAME}"
CFG_DIR="/etc/${APP_NAME}"
CFG_FILE="${CFG_DIR}/config_auto_pull.json"
LOG_DIR="/var/log/${APP_NAME}"
LOG_FILE="${LOG_DIR}/auto_pull.log"
SYSTEMD_UNIT="/etc/systemd/system/${APP_NAME}.service"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="${SCRIPT_DIR}/${APP_NAME}"
CFG_EXAMPLE="${SCRIPT_DIR}/config_auto_pull.example.json"
UNIT_SRC="${SCRIPT_DIR}/${APP_NAME}.service"

# ─── flags ─────────────────────────────────────────────────

DRY_RUN=0
PURGE=0

for arg in "$@"; do
    case "$arg" in
        --dry-run)  DRY_RUN=1 ;;
        --purge)    PURGE=1 ;;
        --help|-h)
            cat <<EOF
autopull bundle installer

Usage:
  sudo ./install.sh              install autopull system-wide
  sudo ./install.sh --dry-run   preview actions without making changes
  sudo ./install.sh --purge     remove all installed files
  sudo ./install.sh --help      show this help

Installs to:
  ${BIN_DEST}       binary
  ${CMD_DEST}       global command (symlink to binary)
  ${CFG_FILE}       default config (created from example if missing)
  ${LOG_FILE}       log file
  ${SYSTEMD_UNIT}   systemd unit (when systemd is available)
EOF
            exit 0
            ;;
        *)
            printf 'Unknown option: %s\n' "$arg" >&2
            printf "Run './install.sh --help' for usage.\n" >&2
            exit 1
            ;;
    esac
done

# ─── helpers ───────────────────────────────────────────────

info() { printf '  \033[1;34m::\033[0m %s\n' "$*"; }
ok()   { printf '  \033[1;32m+\033[0m  %s\n' "$*"; }
warn() { printf '  \033[1;33m!\033[0m  %s\n' "$*"; }
die()  { printf '  \033[1;31mx\033[0m  %s\n' "$*" >&2; exit 1; }
skip() { printf '  \033[2m-  %s (dry-run)\033[0m\n' "$*"; }

run() {
    if [ "$DRY_RUN" -eq 1 ]; then
        skip "$*"
    else
        "$@"
    fi
}

# ─── root check ────────────────────────────────────────────

if [ "$(id -u)" -ne 0 ] && [ "$DRY_RUN" -eq 0 ]; then
    die "Must be run as root. Try: sudo ./install.sh"
fi

# ─── purge ─────────────────────────────────────────────────

if [ "$PURGE" -eq 1 ]; then
    printf '\n\033[1mRemoving autopull...\033[0m\n\n'

    if command -v systemctl >/dev/null 2>&1; then
        systemctl is-active --quiet "${APP_NAME}.service" 2>/dev/null \
            && run systemctl stop "${APP_NAME}.service" || true
        systemctl is-enabled --quiet "${APP_NAME}.service" 2>/dev/null \
            && run systemctl disable "${APP_NAME}.service" || true
        run systemctl daemon-reload
    fi

    for path in "$BIN_DEST" "$CMD_DEST" "$SYSTEMD_UNIT"; do
        [ -e "$path" ] && run rm -f "$path" && ok "Removed $path" || true
    done

    for dir in "$CFG_DIR" "$LOG_DIR"; do
        [ -d "$dir" ] && run rm -rf "$dir" && ok "Removed $dir" || true
    done

    printf '\n'
    ok "autopull removed."
    exit 0
fi

# ─── pre-flight ────────────────────────────────────────────

printf '\n\033[1mInstalling autopull...\033[0m\n\n'

[ -f "${BIN_SRC}" ] || die "Binary not found: ${BIN_SRC}
         Build it first:  go build -o packaging/linux/auto_pull main.go
         Or download a release bundle from:
         https://github.com/reinanbr/auto_pull_go/releases"

[ -f "${CFG_EXAMPLE}" ] || die "Config example not found: ${CFG_EXAMPLE}"

# verify sha256 checksum if a checksum file is present in the bundle
CHECKSUM_FILE="${SCRIPT_DIR}/sha256sums.txt"
if [ -f "$CHECKSUM_FILE" ]; then
    info "Verifying checksum..."
    if command -v sha256sum >/dev/null 2>&1; then
        if (cd "$SCRIPT_DIR" && sha256sum --check --ignore-missing "$CHECKSUM_FILE" >/dev/null 2>&1); then
            ok "Checksum OK"
        else
            die "Checksum mismatch — binary may be corrupted. Re-download the release bundle."
        fi
    else
        warn "sha256sum not found — skipping checksum verification"
    fi
fi

# ─── binary ────────────────────────────────────────────────

info "Installing binary       ${BIN_DEST}"
run install -Dm755 "${BIN_SRC}" "${BIN_DEST}"
ok "Binary installed"

# ─── symlink autopull → auto_pull ──────────────────────────

info "Linking command         ${CMD_DEST}"
run ln -sf "${BIN_DEST}" "${CMD_DEST}"
ok "autopull → auto_pull"

# ─── config ────────────────────────────────────────────────

info "Config dir              ${CFG_DIR}"
run install -d "${CFG_DIR}"

if [ ! -f "${CFG_FILE}" ]; then
    run install -m644 "${CFG_EXAMPLE}" "${CFG_FILE}"
    ok "Default config created: ${CFG_FILE}"
else
    ok "Existing config kept:   ${CFG_FILE}"
fi

# ─── log ───────────────────────────────────────────────────

info "Log dir                 ${LOG_DIR}"
run install -d "${LOG_DIR}"
if [ ! -f "${LOG_FILE}" ]; then
    run touch "${LOG_FILE}"
    run chmod 0644 "${LOG_FILE}"
fi
ok "Log file ready:         ${LOG_FILE}"

# ─── systemd ───────────────────────────────────────────────

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    if [ -f "${UNIT_SRC}" ]; then
        info "Installing systemd unit ${SYSTEMD_UNIT}"
        run install -Dm644 "${UNIT_SRC}" "${SYSTEMD_UNIT}"
        run systemctl daemon-reload
        run systemctl enable --now "${APP_NAME}.service"
        ok "Service enabled:        ${APP_NAME}.service"
    else
        warn "Unit file not found (${UNIT_SRC}) — skipping systemd setup"
    fi
else
    warn "systemd not detected — start manually:"
    warn "  ${BIN_DEST} ${CFG_FILE}"
fi

# ─── done ──────────────────────────────────────────────────

printf '\n'
ok "Installation complete"
printf '\n'
printf '  binary  : %s\n' "${BIN_DEST}"
printf '  command : %s\n' "${CMD_DEST}"
printf '  config  : %s\n' "${CFG_FILE}"
printf '  log     : %s\n' "${LOG_FILE}"
printf '\n'
printf '  Edit the config, then:\n'
printf '    autopull dry-run     # verify connectivity\n'
printf '    autopull             # start watching\n'
printf '\n'
printf '  Or via systemd:\n'
printf '    sudo systemctl status %s\n' "${APP_NAME}"
printf '\n'