#!/bin/sh
# Makes MyInfra.app from the macOS executables of ./build, on a Mac, and the
# MyInfra.dmg it is installed from: its window asks to drag the app into
# Applications. The Finder lays the window out, so it runs in a session with
# a screen, where the terminal may drive the Finder.
#   deploy/macos/package.sh 1.4.0 MyInfra.dmg dist/myinfra_darwin_arm64 dist/myinfra_darwin_amd64
set -eu
if [ $# -lt 3 ]; then
  echo "Usage: deploy/macos/package.sh VERSION OUT.dmg EXECUTABLE..." >&2
  exit 1
fi
version="$1"
out="$2"
shift 2
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
app="$work/MyInfra.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

# One app for Intel and Apple Silicon: each slice carries its own PostgreSQL.
lipo -create "$@" -output "$app/Contents/MacOS/myinfra"
chmod 755 "$app/Contents/MacOS/myinfra"

# macOS only takes numbers: 1.4.0-rc.1 shows as 1.4.0.
sed "s/@VERSION@/${version%%-*}/g" "$here/Info.plist" > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist" > /dev/null

iconset="$work/MyInfra.iconset"
mkdir "$iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$here/icon.png" --out "$iconset/icon_${size}x${size}.png" > /dev/null
  sips -z $((size * 2)) $((size * 2)) "$here/icon.png" --out "$iconset/icon_${size}x${size}@2x.png" > /dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/MyInfra.icns"

# Unsigned as a whole, a downloaded app is called damaged; signed without an
# Apple account, macOS asks once to allow it.
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"

# The content of the image, with its window behind hidden files.
name=MyInfra
stage="$work/stage"
mkdir -p "$stage/.background"
mv "$app" "$stage/MyInfra.app"
ln -s /Applications "$stage/Applications"
# One picture for each kind of screen, as the Finder wants it.
tiffutil -cathidpicheck "$here/dmg/background.png" "$here/dmg/background@2x.png" -out "$stage/.background/background.tiff" > /dev/null

# The Finder writes the layout of the window into the mounted image. Another
# volume of the same name would take it.
if [ -e "/Volumes/$name" ]; then
  echo "Éjectez d'abord /Volumes/$name." >&2
  exit 1
fi
rw="$work/rw.dmg"
size=$(( $(du -sm "$stage" | cut -f1) + 20 ))
hdiutil create -quiet -srcfolder "$stage" -volname "$name" -fs HFS+ -format UDRW -size "${size}m" "$rw"
dev=$(hdiutil attach -readwrite -noverify -noautoopen "$rw" | awk '/Apple_HFS/ {print $1}')
osascript "$here/dmg/layout.applescript" "$name"
for i in $(seq 1 20); do
  [ -f "/Volumes/$name/.DS_Store" ] && break
  sleep 1
done
# The icon of the volume, once the Finder is done: it drops the file.
cp "/Volumes/$name/MyInfra.app/Contents/Resources/MyInfra.icns" "/Volumes/$name/.VolumeIcon.icns"
if command -v SetFile > /dev/null; then
  SetFile -a C "/Volumes/$name"
fi
sync
hdiutil detach -quiet "$dev" || { sleep 3; hdiutil detach -force -quiet "$dev"; }

rm -f "$out"
hdiutil convert -quiet "$rw" -format UDZO -imagekey zlib-level=9 -o "$out"
echo "$out"
