#!/usr/bin/env bash
# Builds GB-Link Companion (run from gblink-companion-native, inside WSL Ubuntu or Linux).
#   ./build.sh            Windows .exe and Linux AppImage
#   ./build.sh windows    only the Windows .exe
#   ./build.sh linux      only the Linux AppImage
# build.bat runs this for you from Windows.
set -euo pipefail

TARGET="${1:-all}"
NAME="GBLinkCompanion"
ICON="GBLinkCompanion.ico"
DIST="$PWD/dist"
CACHE="$HOME/.cache/gblink-companion-build"

export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
if ! command -v go >/dev/null; then
  echo "Go isn't installed in WSL. Install Go 1.24 or newer from https://go.dev/dl/ (linux-amd64)."
  exit 1
fi
export PATH="$PATH:$(go env GOPATH)/bin"
mkdir -p "$DIST" "$CACHE"

# goversioninfo's resource.syso is Windows-only, but Go links every .syso it finds:
# keep it under a name Go only uses for Windows builds.
fix_syso() {
  if [ -f resource.syso ]; then mv -f resource.syso resource_windows_amd64.syso; fi
}

build_windows() {
  echo "=== Building Windows ==="
  if ! command -v goversioninfo >/dev/null; then
    echo "Installing goversioninfo..."
    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
  fi
  go generate
  fix_syso
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -H windowsgui" -o "$DIST/GB-Link-Companion.exe" .
  echo "Built dist/$NAME.exe"
}

build_linux() {
  echo "=== Building Linux AppImage ==="
  fix_syso

  # Gio needs cgo and these libraries; ImageMagick turns the .ico into the AppImage's icon
  local pkgs="build-essential pkg-config libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev
              libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev imagemagick file wget"
  local missing=""
  for p in $pkgs; do dpkg -s "$p" >/dev/null 2>&1 || missing="$missing $p"; done
  if [ -n "$missing" ]; then
    echo "Installing:$missing (asks for your WSL password)"
    sudo apt-get update
    sudo apt-get install -y $missing
  fi

  # build and stage on the Linux side (faster than /mnt/c, and file permissions work)
  local work
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' RETURN
  local app="$work/AppDir"
  mkdir -p "$app/usr/bin"

  CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o "$app/usr/bin/$NAME" .

  cat > "$app/AppRun" <<'EOF'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/bin/GBLinkCompanion" "$@"
EOF
  chmod +x "$app/AppRun"

  cat > "$app/gblink-companion.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=GB-Link Companion
Comment=Play in mGBA with the save on your real cartridge
Exec=GBLinkCompanion
Icon=gblink-companion
Categories=Game;Emulator;
Terminal=false
StartupWMClass=GBLinkCompanion
EOF

  if [ ! -f "$ICON" ]; then echo "$ICON not found in $(pwd)"; exit 1; fi
  # the biggest picture in the .ico, at 256x256
  local idx
  idx="$(identify -format '%p %w\n' "$ICON" | sort -k2 -n | tail -1 | cut -d' ' -f1)"
  convert "$ICON[$idx]" -background none -resize 256x256 "$app/gblink-companion.png"
  cp "$app/gblink-companion.png" "$app/.DirIcon"

  local tool="$CACHE/appimagetool"
  if [ ! -x "$tool" ]; then
    echo "Downloading appimagetool..."
    wget -q -O "$tool" https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage
    chmod +x "$tool"
  fi
  # WSL has no FUSE: run appimagetool without mounting it
  ARCH=x86_64 "$tool" --appimage-extract-and-run "$app" "$work/GB-Link-Companion-x86_64.AppImage"
  cp "$work/GB-Link-Companion-x86_64.AppImage" "$DIST/"
  echo "Built dist/GB-Link-Companion-x86_64.AppImage"
}

case "$TARGET" in
  all)     build_windows; build_linux ;;
  windows) build_windows ;;
  linux)   build_linux ;;
  *)       echo "Unknown target '$TARGET' (use windows, linux or nothing for both)"; exit 1 ;;
esac