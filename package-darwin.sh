#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

APP_NAME="Free Model Gateway"
BUNDLE_ID="com.freemodelgateway.fmg"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION=$(go version | awk '{print $3}')

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GoVersion=${GO_VERSION}"

BIN_DIR="${SCRIPT_DIR}/bin"
mkdir -p "$BIN_DIR"

DIST_DIR="${SCRIPT_DIR}/dist"
PKG_DIR="${DIST_DIR}/pkg-tmp"
DMG_DIR="${DIST_DIR}/dmg-tmp"
rm -rf "$PKG_DIR" "$DMG_DIR" "$DIST_DIR"
mkdir -p "$PKG_DIR" "$DMG_DIR" "$DIST_DIR"

echo "============================================="
echo "  FMG macOS Package Builder"
echo "  Version: ${VERSION}"
echo "============================================="
echo ""

echo ">>> Building fmg binary to bin/..."

ARM64_BIN="${BIN_DIR}/fmg-darwin-arm64"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o "$ARM64_BIN" ./cmd/fmg/

AMD64_BIN="${BIN_DIR}/fmg-darwin-amd64"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$AMD64_BIN" ./cmd/fmg/

UNIVERSAL_BIN="${BIN_DIR}/fmg"
lipo -create "$ARM64_BIN" "$AMD64_BIN" -output "$UNIVERSAL_BIN"

echo "  -> bin/fmg ($(du -h "$UNIVERSAL_BIN" | cut -f1))"

echo ""
echo ">>> Building tray app (universal binary) to bin/..."

TRAY_ARM64="${BIN_DIR}/fmg-tray-arm64"
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
    CGO_CFLAGS="-mmacosx-version-min=10.13" CGO_LDFLAGS="-mmacosx-version-min=10.13" \
    go build -ldflags "$LDFLAGS" -o "$TRAY_ARM64" ./cmd/tray/ 2>/dev/null || {
        echo "  -> Warning: arm64 tray build failed (may need macOS SDK)"
    }

TRAY_AMD64="${BIN_DIR}/fmg-tray-amd64"
if command -v arch >/dev/null 2>&1 && arch -x86_64 uname -m >/dev/null 2>&1; then
    arch -x86_64 env CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
        CGO_CFLAGS="-mmacosx-version-min=10.13" CGO_LDFLAGS="-mmacosx-version-min=10.13" \
        go build -ldflags "$LDFLAGS" -o "$TRAY_AMD64" ./cmd/tray/ 2>/dev/null || {
            echo "  -> Warning: amd64 tray build failed (Rosetta may not be installed)"
        }
else
    CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
        CGO_CFLAGS="-mmacosx-version-min=10.13" CGO_LDFLAGS="-mmacosx-version-min=10.13" \
        go build -ldflags "$LDFLAGS" -o "$TRAY_AMD64" ./cmd/tray/ 2>/dev/null || {
            echo "  -> Warning: amd64 tray build failed"
        }
fi

TRAY_BIN="${BIN_DIR}/fmg-tray"
if [ -f "$TRAY_ARM64" ] && [ -f "$TRAY_AMD64" ]; then
    lipo -create "$TRAY_ARM64" "$TRAY_AMD64" -output "$TRAY_BIN"
    echo "  -> bin/fmg-tray (universal, $(du -h "$TRAY_BIN" | cut -f1))"
elif [ -f "$TRAY_ARM64" ]; then
    cp "$TRAY_ARM64" "$TRAY_BIN"
    echo "  -> bin/fmg-tray (arm64 only, $(du -h "$TRAY_BIN" | cut -f1))"
elif [ -f "$TRAY_AMD64" ]; then
    cp "$TRAY_AMD64" "$TRAY_BIN"
    echo "  -> bin/fmg-tray (amd64 only, $(du -h "$TRAY_BIN" | cut -f1))"
else
    echo "  -> Error: tray app build failed completely"
    exit 1
fi
echo ""

echo ">>> Creating .app bundle..."

APP_BUNDLE="${DMG_DIR}/${APP_NAME}.app"
CONTENTS="${APP_BUNDLE}/Contents"
MACOS="${CONTENTS}/MacOS"
RESOURCES="${CONTENTS}/Resources"

mkdir -p "$MACOS" "$RESOURCES"

cp "$UNIVERSAL_BIN" "${MACOS}/fmg"
chmod +x "${MACOS}/fmg"
cp "$TRAY_BIN" "${MACOS}/fmg-tray"
chmod +x "${MACOS}/fmg-tray"

cat > "${CONTENTS}/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleExecutable</key>
    <string>fmg-tray</string>
    <key>CFBundleIdentifier</key>
    <string>com.freemodelgateway.fmg</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>Free Model Gateway</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>__VERSION__</string>
    <key>CFBundleVersion</key>
    <string>__VERSION__</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

sed -i '' "s/__VERSION__/${VERSION}/g" "${CONTENTS}/Info.plist"


if [ -f "${SCRIPT_DIR}/assets/AppIcon.icns" ]; then
    cp "${SCRIPT_DIR}/assets/AppIcon.icns" "${RESOURCES}/AppIcon.icns"
fi

if [ -d "${SCRIPT_DIR}/web-app" ]; then
    cp -R "${SCRIPT_DIR}/web-app" "${RESOURCES}/web-app"
    echo "  -> Copied web-app to Resources/"
fi

echo "  -> Created: ${APP_BUNDLE}"
echo ""

echo ">>> Creating .pkg installer..."

PKG_ROOT="${PKG_DIR}/pkg-root"
mkdir -p "${PKG_ROOT}/usr/local/bin"
mkdir -p "${PKG_ROOT}/usr/local/share/fmg"
mkdir -p "${PKG_ROOT}/usr/local/share/doc/fmg"

cp "$UNIVERSAL_BIN" "${PKG_ROOT}/usr/local/bin/fmg"
chmod 755 "${PKG_ROOT}/usr/local/bin/fmg"

if [ -d "${SCRIPT_DIR}/web-app" ]; then
    cp -R "${SCRIPT_DIR}/web-app" "${PKG_ROOT}/usr/local/share/fmg/web-app"
    echo "  -> Copied web-app to /usr/local/share/fmg/"
fi


cat > "${PKG_ROOT}/usr/local/share/doc/fmg/README.txt" << 'README'
Free Model Gateway (FMG)
========================

Installation complete!

Quick Start:
1. Start gateway:        fmg
2. Open Dashboard:       http://localhost:10086
3. Configure providers:  Use the Dashboard Settings page

Or use the app bundle:   Open "Free Model Gateway.app" in Applications

Dashboard:               http://localhost:10086 (after starting)
README

COMPONENT_PKG="${DIST_DIR}/fmg-component.pkg"
pkgbuild \
    --root "$PKG_ROOT" \
    --identifier "$BUNDLE_ID" \
    --version "$VERSION" \
    --install-location / \
    "$COMPONENT_PKG"

cat > "${PKG_DIR}/distribution.xml" << EOF
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="1">
    <title>Free Model Gateway ${VERSION}</title>
    <organization>com.freemodelgateway</organization>
    <domains enable_localSystem="true"/>
    <options customize="never" require-scripts="false" rootVolumeOnly="true" />
    <welcome file="welcome.rtf" mime-type="text/rtf"/>
    <license file="license.rtf" mime-type="text/rtf"/>
    <conclusion file="conclusion.rtf" mime-type="text/rtf"/>
    <pkg-ref id="${BUNDLE_ID}" version="${VERSION}" onConclusion="none">fmg-component.pkg</pkg-ref>
    <choices-outline>
        <line choice="default">
            <line choice="${BUNDLE_ID}"/>
        </line>
    </choices-outline>
    <choice id="default" title="Free Model Gateway" enabled="false" selected="true" description="-"/>
    <choice id="${BUNDLE_ID}" visible="false" selected="true" title="Free Model Gateway" description="Main installation">
        <pkg-ref id="${BUNDLE_ID}"/>
    </choice>
</installer-gui-script>
EOF

cat > "${PKG_DIR}/welcome.rtf" << 'WELCOME'
{\rtf1\ansi
{\b Free Model Gateway}\
\par Version __VERSION__\
\par \
\par This installer will install Free Model Gateway to your Mac.\
\par \
\par Requirements:\
\par - macOS 11.0 or later\
\par - Apple Silicon or Intel Mac\
\par \
\par The gateway will be installed to /usr/local/bin/fmg\
\par}
WELCOME
sed -i '' "s/__VERSION__/${VERSION}/g" "${PKG_DIR}/welcome.rtf"

cat > "${PKG_DIR}/license.rtf" << 'LICENSE'
{\rtf1\ansi
{\b MIT License}\
\par \
\par Copyright (c) 2024 Free Model Gateway\
\par \
\par Permission is hereby granted...\
\par}
LICENSE

cat > "${PKG_DIR}/conclusion.rtf" << 'CONCLUSION'
{\rtf1\ansi
{\b Installation Complete!}\
\par \
\par Free Model Gateway has been installed to:\
\par - Binary: /usr/local/bin/fmg\
\par - Templates: /usr/local/share/fmg/\
\par \
\par Next steps:\
\par 1. Start gateway: fmg\
\par 2. Open Dashboard: http://localhost:10086\
\par 3. Configure providers via Dashboard Settings\
\par \
\par Or open the Free Model Gateway app from Applications.\
\par \
\par Dashboard: http://localhost:10086\
\par}
CONCLUSION

PKG_FILE="${DIST_DIR}/fmg-${VERSION}-macos.pkg"
productbuild \
    --distribution "${PKG_DIR}/distribution.xml" \
    --resources "${PKG_DIR}" \
    --package-path "${DIST_DIR}" \
    "$PKG_FILE"

rm "$COMPONENT_PKG"

echo "  -> Created: ${PKG_FILE}"
echo ""

echo ">>> Applying ad-hoc signature..."
codesign --force --deep --sign - "${APP_BUNDLE}" 2>/dev/null || \
    echo "  -> Warning: ad-hoc codesign failed"

echo ">>> Creating .dmg disk image..."

DMG_LAYOUT="${DIST_DIR}/dmg-layout"
mkdir -p "${DMG_LAYOUT}"

cp -R "${APP_BUNDLE}" "${DMG_LAYOUT}/"
ln -s /Applications "${DMG_LAYOUT}/Applications"

cat > "${DMG_LAYOUT}/README.txt" << 'README'
Free Model Gateway (FMG)
========================

1. Drag "Free Model Gateway.app" to Applications
2. Open from Applications
3. On first launch, it creates ~/.fmg/ directory
4. Click the tray icon to open Dashboard, start/stop service
5. Open http://localhost:10086 in your browser

Dashboard: http://localhost:10086
API:      http://localhost:10086/v1/chat/completions
README

DMG_FILE="${DIST_DIR}/fmg-${VERSION}-macos.dmg"
hdiutil create \
    -srcfolder "${DMG_LAYOUT}" \
    -volname "Free Model Gateway ${VERSION}" \
    -fs HFS+ \
    -format UDZO \
    "$DMG_FILE" 2>/dev/null || \
hdiutil create \
    -srcfolder "${DMG_LAYOUT}" \
    -volname "Free Model Gateway ${VERSION}" \
    -fs APFS \
    -format ULMO \
    "$DMG_FILE"

echo "  -> Created: ${DMG_FILE}"
echo ""

echo "============================================="
echo "  macOS Packaging Complete!"
echo "============================================="
echo ""
echo "Binaries:"
echo "  bin/fmg          ($(du -h "$UNIVERSAL_BIN" | cut -f1))"
echo "  bin/fmg-tray     ($(du -h "$TRAY_BIN" | cut -f1))"
echo ""
echo "Artifacts:"
echo "  .pkg: ${PKG_FILE} ($(du -h "$PKG_FILE" | cut -f1))"
echo "  .dmg: ${DMG_FILE} ($(du -h "$DMG_FILE" | cut -f1))"
echo ""
echo "Distribution:"
echo "  - .pkg: Double-click to install CLI to /usr/local/bin"
echo "  - .dmg: Drag app to Applications (includes tray menu)"
echo ""

rm -rf "$PKG_DIR" "$DMG_DIR"

echo "Done."
