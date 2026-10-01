#!/bin/sh
# Renders each mock to ../<name>.png at 2x scale with headless Chrome.
cd "$(dirname "$0")" || exit 1
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
shot() { # name size [hash] [out]
  "$chrome" --headless --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
    --window-size="$2" --screenshot="../${4:-$1}.png" "file://$PWD/$1.html$3" >/dev/null 2>&1
}
shot before 1600,900
shot a-wide 1600,900
shot a-wide 1600,900 '#light' a-wide-light
shot b-medium 1100,560
shot c-statusline 1100,470
shot d-whichkey 1100,560
shot e-list-row 1000,470
shot f-empty-loading 1300,560
shot h-image-viewer 1100,560
