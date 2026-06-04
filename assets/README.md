# FMG Icon Resources

## Directory Structure

assets/
  README.md
  AppIcon.icns        - macOS App icon (Dock, Finder)

cmd/tray/assets/
  tray-running.png    - Tray icon (service running)
  tray-stopped.png    - Tray icon (service stopped)

## Usage

### App Icon

Place AppIcon.icns in assets/, it will be copied to app bundle automatically during packaging.

### Tray Icons

Place tray-running.png and tray-stopped.png in cmd/tray/assets/, they are embedded into the tray binary via go:embed.

After replacing icons, rebuild:
    make build-tray
