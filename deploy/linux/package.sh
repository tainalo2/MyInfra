#!/bin/sh
# Makes the Linux packages of MyInfra from an executable of ./build: the
# AppImage, the .deb and the .rpm. The applications menu then opens MyInfra
# in its own window. Needs Go, curl and ImageMagick.
#   deploy/linux/package.sh 1.4.0 amd64 dist/myinfra_linux_amd64 dist
set -eu
if [ $# -lt 3 ]; then
  echo "Usage: deploy/linux/package.sh VERSION ARCH EXECUTABLE [OUTDIR]" >&2
  exit 1
fi
version="$1"
arch="$2"
exe=$(cd "$(dirname "$3")" && pwd)/$(basename "$3")
out="${4:-dist}"
here=$(cd "$(dirname "$0")" && pwd)
cd "$here/../.."
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

case "$arch" in
  amd64) machine=x86_64 ;;
  arm64) machine=aarch64 ;;
  *) echo "Architecture inconnue : $arch" >&2; exit 1 ;;
esac

magick=magick
command -v magick > /dev/null || magick=convert
icons="$work/hicolor"
for size in 16 24 32 48 64 128 256 512; do
  mkdir -p "$icons/${size}x${size}/apps"
  "$magick" deploy/macos/icon.png -resize "${size}x${size}" "$icons/${size}x${size}/apps/myinfra.png"
done

# .deb and .rpm: MyInfra in /usr/bin, updated by the system.
stage="$work/stage"
mkdir "$stage"
cp "$exe" "$stage/myinfra"
cp deploy/linux/myinfra.desktop deploy/linux/nfpm.yaml "$stage/"
cp -R "$icons" "$stage/hicolor"
for packager in deb rpm; do
  (cd "$stage" && VERSION="$version" ARCH="$arch" \
    go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.48.0 package \
    --config nfpm.yaml --packager "$packager" \
    --target "$out/myinfra_${version}_linux_${arch}.$packager")
done

# The AppImage: one file, on every distribution, with the WebKitGTK of the
# system. It updates itself, as the executable does.
appdir="$work/MyInfra.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/applications" "$appdir/usr/share/icons"
cp "$exe" "$appdir/usr/bin/myinfra"
chmod 755 "$appdir/usr/bin/myinfra"
cp deploy/linux/myinfra.desktop "$appdir/myinfra.desktop"
cp deploy/linux/myinfra.desktop "$appdir/usr/share/applications/myinfra.desktop"
cp -R "$icons" "$appdir/usr/share/icons/hicolor"
cp "$icons/256x256/apps/myinfra.png" "$appdir/myinfra.png"
cat > "$appdir/AppRun" <<'APPRUN'
#!/bin/sh
# --app last: an update checks the new AppImage with --version first.
exec "$APPDIR/usr/bin/myinfra" "$@" --app
APPRUN
chmod 755 "$appdir/AppRun"

tool="$work/appimagetool"
curl -fsSL -o "$tool" "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-$(uname -m).AppImage"
curl -fsSL -o "$work/runtime" "https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-$machine"
chmod 755 "$tool"
ARCH="$machine" "$tool" --appimage-extract-and-run --no-appstream --runtime-file "$work/runtime" \
  "$appdir" "$out/myinfra_${version}_linux_${arch}.AppImage"

ls -lh "$out"/myinfra_"${version}"_linux_"${arch}".*
