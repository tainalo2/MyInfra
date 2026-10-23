#!/bin/sh
# Renders background.html into the two pictures of the window of MyInfra.dmg,
# with Google Chrome and the Geist font of web/node_modules.
#   deploy/macos/dmg/render.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
for scale in 1 2; do
  out="$here/background.png"
  [ "$scale" = 1 ] || out="$here/background@${scale}x.png"
  "$chrome" --headless --disable-gpu --hide-scrollbars --window-size=660,400 \
    --force-device-scale-factor="$scale" --screenshot="$out" "file://$here/background.html" 2>/dev/null
  echo "$out"
done
