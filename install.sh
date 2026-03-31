#!/bin/sh
# autopull installer
# Usage:
#   Install:   curl -fsSL https://raw.githubusercontent.com/reinanbr/auto_pull_go/main/install.sh | sh
#   Uninstall: curl -fsSL https://raw.githubusercontent.com/reinanbr/auto_pull_go/main/install.sh | sh -s -- uninstall

set -e

REPO="reinanbr/auto_pull_go"
BINARY="autopull"
INSTALL_DIR="/usr/local/bin"

# ─── helpers ───────────────────────────────────────────────

info()  { printf '\033[1;34m::\033[0m %s\n' "$*"; }
ok()    { printf '\033[1;32m[OK]\033[0m %s\n' "$*"; }
warn()  { printf '\033[1;33m[WARN]\033[0m %s\n' "$*"; }
die()   { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not found on PATH"
}

ACTION="install"
if [ "$#" -gt 0 ]; then
    case "$1" in
        uninstall|--uninstall|remove)
            ACTION="uninstall"
            ;;
        --help|-h)
            cat <<EOF
autopull installer

Install:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sh

Uninstall:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sh -s -- uninstall
EOF
            exit 0
            ;;
    esac
fi

# ─── checks ────────────────────────────────────────────────

if [ "$ACTION" = "install" ]; then
    need curl
    need git
elif [ "$ACTION" != "uninstall" ]; then
    die "Unknown action: $1 (use 'uninstall' or no arg)"
fi

# ─── uninstall path ─────────────────────────────────────────

if [ "$ACTION" = "uninstall" ]; then
    TARGET="${INSTALL_DIR}/${BINARY}"
    info "Removing ${TARGET}..."
    if [ -e "$TARGET" ]; then
        if [ -w "$INSTALL_DIR" ]; then
            rm -f "$TARGET"
        else
            sudo rm -f "$TARGET"
        fi
        ok "Removed ${TARGET}"
    else
        warn "Binary not found at ${TARGET}"
    fi

    printf '\n'
    info "Uninstall complete"
    exit 0
fi

# ─── detect OS / arch ──────────────────────────────────────

if [ "$ACTION" = "install" ]; then
    OS="$(uname -s)"
    ARCH="$(uname -m)"

    case "$OS" in
        Linux)  os="linux" ;;
        Darwin) os="darwin" ;;
        *)      die "Unsupported OS: $OS" ;;
    esac

    case "$ARCH" in
        x86_64)          arch="amd64" ;;
        aarch64|arm64)   arch="arm64" ;;
        *)               die "Unsupported architecture: $ARCH" ;;
    esac
fi

# ─── resolve latest version ────────────────────────────────

if [ "$ACTION" = "install" ]; then
    info "Fetching latest release..."

    VERSION="$(
        curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | sed 's/.*"tag_name": *"\(.*\)".*/\1/'
    )"

    # fallback: use latest tag if no GitHub release exists yet
    if [ -z "$VERSION" ]; then
        VERSION="$(
            git ls-remote --tags --sort=-v:refname \
                "https://github.com/${REPO}.git" \
            | grep -oE 'refs/tags/v[0-9]+\.[0-9]+\.[0-9]+' \
            | head -1 \
            | sed 's|refs/tags/||'
        )"
    fi

    [ -z "$VERSION" ] && die "Could not determine latest version"

    ok "Latest version: $VERSION"
fi

# ─── download ──────────────────────────────────────────────

if [ "$ACTION" = "install" ]; then
    FILENAME="${BINARY}_${os}_${arch}"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

    TMP="$(mktemp)"
    trap 'rm -f "$TMP"' EXIT

    info "Downloading ${FILENAME} (${VERSION})..."
    curl -fsSL --progress-bar "$URL" -o "$TMP" \
        || die "Download failed — check that release ${VERSION} has a binary for ${os}/${arch}:\n  ${URL}"

    chmod +x "$TMP"

    # ─── install ───────────────────────────────────────────────

    info "Installing to ${INSTALL_DIR}/${BINARY}..."

    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMP" "${INSTALL_DIR}/${BINARY}"
    else
        sudo mv "$TMP" "${INSTALL_DIR}/${BINARY}"
    fi

    # ─── verify ────────────────────────────────────────────────

    if ! command -v "$BINARY" >/dev/null 2>&1; then
        die "Install succeeded but '${BINARY}' is not on PATH. Add ${INSTALL_DIR} to your PATH."
    fi

    ok "Installed: $(command -v "$BINARY")"
    ok "Version  : $("$BINARY" --version)"

    # ─── next steps ────────────────────────────────────────────

    printf '\n'
    info "Next steps:"
    printf '    cd /path/to/your/repo\n'
    printf '    autopull init        # create config_auto_pull.json\n'
    printf '    autopull dry-run     # test connectivity\n'
    printf '    autopull             # start watching\n'
    printf '\n'
    printf '  For private repos:\n'
    printf "    echo 'AUTOPULL_TOKEN=ghp_xxx' >> .env\n"
    printf "    echo '.env' >> .gitignore\n"
    printf '\n'
fi
