#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GoVersion=${GO_VERSION:-$(go version | awk '{print $3}')}"

BIN_DIR="${SCRIPT_DIR}/bin"
DIST_DIR="${SCRIPT_DIR}/dist"
rm -rf "$DIST_DIR"
mkdir -p "$BIN_DIR" "$DIST_DIR"

echo "============================================="
echo "  Free Model Gateway - Build & Package"
echo "  Version: ${VERSION} (${BUILD_TIME})"
echo "============================================="
echo ""

PLATFORMS=(
    "linux/amd64/fmg-linux-amd64"
    "linux/arm64/fmg-linux-arm64"
    "darwin/amd64/fmg-darwin-amd64"
    "darwin/arm64/fmg-darwin-arm64"
    "windows/amd64/fmg-windows-amd64.exe"
)

build_one() {
    local os=$1 arch=$2 output=$3
    echo ">>> Building: ${os}/${arch} -> bin/${output}"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build $LDFLAGS -o "${BIN_DIR}/${output}" ./cmd/fmg/
    echo "  -> ${BIN_DIR}/${output} ($(du -h "${BIN_DIR}/${output}" | cut -f1))"
}

for platform in "${PLATFORMS[@]}"; do
    IFS='/' read -r os arch output <<< "$platform"
    build_one "$os" "$arch" "$output"
done

echo ""
echo "============================================="
echo "  Build complete."
echo "============================================="
echo ""
echo "Binaries in bin/:"
ls -lh "$BIN_DIR"/fmg* 2>/dev/null || true
echo ""

if [[ "${1:-}" == "--release" ]]; then
    echo ">>> Packaging release archives..."
    if ! command -v zip >/dev/null 2>&1; then
        echo "zip not found; skipping archive step"
        exit 0
    fi
    for file in "$BIN_DIR"/fmg-*; do
        [ -f "$file" ] || continue
        name=$(basename "$file")
        target_dir="${DIST_DIR}/${name}"
        mkdir -p "$target_dir"
        cp "$file" "$target_dir/"
        cp config.example.yaml "$target_dir/" 2>/dev/null || true
        cp start.sh "$target_dir/" 2>/dev/null && chmod +x "$target_dir/start.sh" 2>/dev/null || true
        cp .env.example "$target_dir/" 2>/dev/null || true
        if [[ "$name" == *windows* ]]; then
            cat > "${target_dir}/start.bat" << 'BAT'
@echo off
chcp 65001 >nul
cd /d "%~dp0"
if exist ".env" for /f "usebackq tokens=1,2 delims==" %%a in (".env") do set "%%a=%%b"
if not exist "logs" mkdir logs
echo Starting Free Model Gateway on http://localhost:10086
fmg-windows-amd64.exe -c config.yaml -l info
pause
BAT
        fi
        zip_name="${DIST_DIR}/${name}.zip"
        (cd "$DIST_DIR" && zip -r -q "$zip_name" "$name"/)
        echo "  ${zip_name} ($(du -h "$zip_name" | cut -f1))"
    done
fi
