#!/bin/sh
# Makes MyInfra.app from the macOS executables of ./build, on a Mac: a double
# click starts MyInfra in the background and opens the browser.
#   deploy/macos/package.sh 1.4.0 MyInfra.zip dist/myinfra_darwin_arm64 dist/myinfra_darwin_amd64
set -eu
if [ $# -lt 3 ]; then
  echo "Usage: deploy/macos/package.sh VERSION OUT.zip EXECUTABLE..." >&2
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

# ditto keeps the rights and the signature, which a plain zip may lose.
rm -f "$out"
ditto -c -k --keepParent "$app" "$out"
echo "$out"
