#!/usr/bin/env bash
set -euo pipefail

APP_NAME="auto_pull"
VERSION="${1:-dev}"
ARCH="${ARCH:-amd64}"
OS="linux"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${ROOT_DIR}/dist/${APP_NAME}_${OS}_${ARCH}_${VERSION}"

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

pushd "${ROOT_DIR}" >/dev/null
CGO_ENABLED=0 GOOS="${OS}" GOARCH="${ARCH}" go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o "${OUT_DIR}/${APP_NAME}" main.go
popd >/dev/null

cp "${ROOT_DIR}/packaging/linux/install.sh" "${OUT_DIR}/install.sh"
cp "${ROOT_DIR}/packaging/linux/uninstall.sh" "${OUT_DIR}/uninstall.sh"
cp "${ROOT_DIR}/packaging/linux/${APP_NAME}.service" "${OUT_DIR}/${APP_NAME}.service"
cp "${ROOT_DIR}/packaging/linux/config_auto_pull.example.json" "${OUT_DIR}/config_auto_pull.example.json"

chmod +x "${OUT_DIR}/${APP_NAME}" "${OUT_DIR}/install.sh" "${OUT_DIR}/uninstall.sh"

tar -C "${ROOT_DIR}/dist" -czf "${ROOT_DIR}/dist/${APP_NAME}_${OS}_${ARCH}_${VERSION}.tar.gz" "$(basename "${OUT_DIR}")"

echo "[OK] release generated: dist/${APP_NAME}_${OS}_${ARCH}_${VERSION}.tar.gz"
