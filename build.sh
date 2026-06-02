#!/bin/bash
# ============================================================
#  FMG cross-platform build + package script
#  Usage: ./build.sh [--release]
# ============================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="v1.0.0"
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GoVersion=${GO_VERSION:-$(go version | awk '{print $3}')}"

OUTPUT_DIR="${SCRIPT_DIR}/dist"
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

echo "============================================="
echo "  Free Model Gateway - Build & Package"
echo "  Version: ${VERSION} (${BUILD_TIME})"
echo "============================================="
echo ""

PLATFORMS=(
    "linux/amd64/fmg-linux-x64"
    "linux/arm64/fmg-linux-arm64"
    "darwin/amd64/fmg-macos-intel"
    "darwin/arm64/fmg-macos-apple-silicon"
    "windows/amd64/fmg-windows-x64.exe"
)

build_one() {
    local os=$1 arch=$2 output=$3
    local target="${OUTPUT_DIR}/${os}-${arch}"
    echo ">>> Building: ${os}/${arch} -> ${output}"
    mkdir -p "$target"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build $LDFLAGS -o "${target}/${output}" ./cmd/fmg/
    cp config.example.yaml "${target}/config.yaml" 2>/dev/null || true
    cp start.sh "${target}/start.sh" 2>/dev/null && chmod +x "${target}/start.sh" 2>/dev/null || true
    cp .env.example "${target}/.env.example" 2>/dev/null || true
    if [ "$os" = "windows" ]; then
        cat > "${target}/启动网关.bat" << 'BAT'
@echo off
chcp 65001 >nul
cd /d "%~dp0"
if exist ".env" for /f "usebackq tokens=1,2 delims==" %%a in (".env") do set "%%a=%%b"
if not exist "logs" mkdir logs
echo Starting Free Model Gateway on http://localhost:10086
fmg-windows-x64.exe -c config.yaml -l info
pause
BAT
    fi
    echo "  -> ${target}/${output} ($(du -h "${target}/${output}" | cut -f1))"
}

for platform in "${PLATFORMS[@]}"; do
    IFS='/' read -r os arch output <<< "$platform"
    build_one "$os" "$arch" "$output"
done

echo ""
echo "============================================="
echo "  Build complete. Output: ${OUTPUT_DIR}/"
echo "============================================="
ls -lh "${OUTPUT_DIR}"/*/fmg* 2>/dev/null || true

if [[ "${1:-}" == "--release" ]]; then
    echo ""
    echo ">>> Packaging release archives..."
    if ! command -v zip >/dev/null 2>&1; then
        echo "zip not found; skipping archive step"
        exit 0
    fi
    for dir in "${OUTPUT_DIR}"/*/; do
        [ -d "$dir" ] || continue
        local dirname
        dirname=$(basename "$dir")
        local zip_name="${OUTPUT_DIR}/fmg-${dirname}.zip"
        (cd "$OUTPUT_DIR" && zip -r -q "$zip_name" "$dirname"/)
        echo "  ${zip_name} ($(du -h "$zip_name" | cut -f1))"
    done
fi
