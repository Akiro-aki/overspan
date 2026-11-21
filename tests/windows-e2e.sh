#!/bin/sh
# Builds the Windows viewer and runs tests/windows_e2e.py under Wine with the
# null display driver: no window appears on this desktop, and the Wine prefix
# is separate from any you use (default ~/.cache/overspan/wineprefix).
# Needs wine, Go, and a live Hyprland or Sway session.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
export WINEPREFIX=${OVERSPAN_WINEPREFIX:-${XDG_CACHE_HOME:-$HOME/.cache}/overspan/wineprefix}
export WINEDEBUG=-all WINEDLLOVERRIDES="mscoree,mshtml="
# Wine runs without a display; the server keeps WAYLAND_DISPLAY (it captures)
headless="env -u DISPLAY -u WAYLAND_DISPLAY"

make -C "$here" -s build windows
if [ ! -f "$WINEPREFIX/system.reg" ]; then
  echo "creating Wine prefix at $WINEPREFIX"
  mkdir -p "$WINEPREFIX"
  $headless wineboot -i >/dev/null 2>&1
fi
# headless: the null graphics driver (windows exist, nothing is shown)
$headless wine reg add 'HKCU\Software\Wine\Drivers' /v Graphics /d null /f >/dev/null 2>&1
exec python3 -I "$here/tests/windows_e2e.py" "$here/replica/clone-screens"
