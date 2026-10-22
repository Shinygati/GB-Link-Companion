#!/bin/sh
# Builds GBLinkCompanion-<version>-x86_64.AppImage.
#
# The program is built against glibc 2.28 with zig as the C compiler, so it runs on any desktop
# Linux from about 2018 on. It uses the system's X11/Wayland, EGL and xkbcommon (on every desktop;
# AppImages don't bundle them). Needs: Go, zig (pip install ziglang), the X11/Wayland/xkbcommon/EGL
# development headers (Debian/Ubuntu: libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libxcursor-dev
# libxfixes-dev libwayland-dev libegl-dev libvulkan-dev), and appimagetool + its type2 runtime from
# github.com/AppImage (set APPIMAGETOOL and RUNTIME, or put them in ./aitool/).
set -e
cd "$(dirname "$0")/../.."
VERSION=$(sed -n 's/.*appVersion = "\(.*\)".*/\1/p' version.go)
WORK=$(mktemp -d)
# zig needs the toolkit headers without the system's own C library headers next to them
mkdir -p "$WORK/inc"
for h in X11 xkbcommon EGL KHR GLES2 GLES3 xcb vulkan vk_video; do cp -r "/usr/include/$h" "$WORK/inc/" 2>/dev/null || true; done
cp /usr/include/wayland-*.h "$WORK/inc/"
cat > "$WORK/cc" <<'CC'
#!/bin/sh
exec python3 -m ziglang cc -target x86_64-linux-gnu.2.28 "$@"
CC
chmod +x "$WORK/cc"
CGO_ENABLED=1 CC="$WORK/cc" CGO_CFLAGS="-O2 -I$WORK/inc" \
	CGO_LDFLAGS="-L/usr/lib/x86_64-linux-gnu -Wl,--allow-shlib-undefined" \
	go build -trimpath -ldflags "-s -w" -o "$WORK/GBLinkCompanion" .

APPDIR="$WORK/AppDir"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"
cp "$WORK/GBLinkCompanion" "$APPDIR/usr/bin/"
cp packaging/linux/AppRun "$APPDIR/AppRun"; chmod +x "$APPDIR/AppRun"
cp packaging/linux/gblinkcompanion.desktop "$APPDIR/"
cp packaging/linux/gblinkcompanion.desktop "$APPDIR/usr/share/applications/"
cp winres/icon256.png "$APPDIR/gblinkcompanion.png"
cp winres/icon256.png "$APPDIR/usr/share/icons/hicolor/256x256/apps/gblinkcompanion.png"
TOOL=${APPIMAGETOOL:-./aitool/appimagetool}
RUNTIME=${RUNTIME:-./aitool/runtime-x86_64}
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$TOOL" --runtime-file "$RUNTIME" "$APPDIR" "GBLinkCompanion-$VERSION-x86_64.AppImage"
rm -rf "$WORK"
