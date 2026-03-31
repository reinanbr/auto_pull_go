#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
#  auto_pull — build & process manager
#  Usage:
#    ./run.sh            → build and run in foreground
#    ./run.sh --daemon   → run in background (saves PID)
#    ./run.sh --stop     → stop the background process
#    ./run.sh --status   → check whether it is running
#    ./run.sh --build    → compile only, do not run
# ─────────────────────────────────────────────────────────────

set -euo pipefail

BINARY="./auto_pull"
CONFIG="config_auto_pull.json"
PID_FILE=".auto_pull.pid"

# ── colours ──────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()  { echo -e "${CYAN}[INFO]${RESET} $*"; }
ok()    { echo -e "${GREEN}[OK]${RESET}   $*"; }
warn()  { echo -e "${YELLOW}[WARN]${RESET} $*"; }
err()   { echo -e "${RED}[ERROR]${RESET} $*" >&2; }

# ── dependency check ─────────────────────────────────────────
check_deps() {
  local missing=()
  for dep in go git; do
    command -v "$dep" &>/dev/null || missing+=("$dep")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing dependencies: ${missing[*]}"
    err "Install with: sudo apt install ${missing[*]}   (or equivalent)"
    exit 1
  fi
}

# ── build ────────────────────────────────────────────────────
build() {
  info "Compiling auto_pull..."
  go build -o "$BINARY" main.go
  ok "Binary ready: $BINARY"
}

# ── foreground run ───────────────────────────────────────────
run_fg() {
  [[ -f "$BINARY" ]] || build
  info "Starting (Ctrl+C to stop)..."
  "$BINARY" "$CONFIG"
}

# ── daemon run ───────────────────────────────────────────────
run_daemon() {
  [[ -f "$BINARY" ]] || build

  if [[ -f "$PID_FILE" ]]; then
    pid=$(cat "$PID_FILE")
    if kill -0 "$pid" 2>/dev/null; then
      warn "auto_pull is already running (PID $pid)"
      return
    fi
  fi

  nohup "$BINARY" "$CONFIG" >> auto_pull.log 2>&1 &
  echo $! > "$PID_FILE"
  ok "auto_pull started in background (PID $(cat $PID_FILE))"
  info "Log file: auto_pull.log"
}

# ── stop ─────────────────────────────────────────────────────
stop_daemon() {
  if [[ ! -f "$PID_FILE" ]]; then
    warn "PID file not found. Process is not running (or was not started with --daemon)."
    return
  fi
  pid=$(cat "$PID_FILE")
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    rm -f "$PID_FILE"
    ok "auto_pull (PID $pid) stopped."
  else
    warn "Process PID $pid no longer exists."
    rm -f "$PID_FILE"
  fi
}

# ── status ───────────────────────────────────────────────────
status() {
  if [[ -f "$PID_FILE" ]]; then
    pid=$(cat "$PID_FILE")
    if kill -0 "$pid" 2>/dev/null; then
      ok "auto_pull is RUNNING (PID $pid)"
    else
      warn "PID file exists but process $pid is not active."
    fi
  else
    info "auto_pull is NOT running."
  fi
}

# ── entry point ──────────────────────────────────────────────
check_deps

case "${1:-}" in
  --build)   build ;;
  --daemon)  run_daemon ;;
  --stop)    stop_daemon ;;
  --status)  status ;;
  --help|-h)
    echo -e "${BOLD}Usage:${RESET} $0 [--build | --daemon | --stop | --status | --help]"
    echo "  (no flag)  compile and run in the foreground"
    ;;
  *) run_fg ;;
esac
