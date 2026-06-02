#!/bin/bash
# ============================================================
#  FMG macOS .app bundle generator
#  Usage: ./make-app.sh
#  Output: builds FreeModelGateway.app in current directory
# ============================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="FreeModelGateway.app"
APP_PATH="${SCRIPT_DIR}/${APP_NAME}"

echo "Building ${APP_NAME}..."

rm -rf "$APP_PATH"
mkdir -p "${APP_PATH}/Contents/MacOS"
mkdir -p "${APP_PATH}/Contents/Resources"

cat > "${APP_PATH}/Contents/MacOS/FreeModelGateway" << 'EOF'
#!/bin/bash
cd "$(dirname "$0")/../Resources"
exec ./start.sh
EOF
chmod +x "${APP_PATH}/Contents/MacOS/FreeModelGateway"

cp "${SCRIPT_DIR}/config.yaml" "${APP_PATH}/Contents/Resources/" 2>/dev/null || cp "${SCRIPT_DIR}/config.example.yaml" "${APP_PATH}/Contents/Resources/config.yaml"
cp "${SCRIPT_DIR}/bin/fmg" "${APP_PATH}/Contents/Resources/" 2>/dev/null || true
cp "${SCRIPT_DIR}/start.sh" "${APP_PATH}/Contents/Resources/" 2>/dev/null || true
cp "${SCRIPT_DIR}/.env" "${APP_PATH}/Contents/Resources/" 2>/dev/null || true
chmod +x "${APP_PATH}/Contents/Resources/start.sh" 2>/dev/null || true

cat > "${APP_PATH}/Contents/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>FreeModelGateway</string>
    <key>CFBundleIdentifier</key>
    <string>ai.fmg.gateway</string>
    <key>CFBundleName</key>
    <string>Free Model Gateway</string>
    <key>CFBundleDisplayName</key>
    <string>Free Model Gateway</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleVersion</key>
    <string>1.0.0</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0.0</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSUIElement</key>
    <false/>
</dict>
</plist>
PLIST

if [ -f "${SCRIPT_DIR}/icon.png" ] && command -v sips >/dev/null 2>&1 && command -v iconutil >/dev/null 2>&1; then
    mkdir -p "${APP_PATH}/Contents/Resources/icon.iconset"
    for spec in "16:16" "32:32" "64:64" "128:128" "256:256" "512:512" "1024:1024"; do
        size=${spec%:*}
        sips -z "$size" "$size" "${SCRIPT_DIR}/icon.png" --out "${APP_PATH}/Contents/Resources/icon.iconset/icon_${size}x${size}.png" >/dev/null 2>&1 || true
    done
    iconutil -c icns "${APP_PATH}/Contents/Resources/icon.iconset" -o "${APP_PATH}/Contents/Resources/icon.icns" 2>/dev/null || true
    rm -rf "${APP_PATH}/Contents/Resources/icon.iconset"
fi

echo ""
echo "${APP_NAME} built at: ${APP_PATH}"
echo "Double-click to launch, or drag to /Applications."
