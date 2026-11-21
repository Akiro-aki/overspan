#!/bin/sh
# Overspan installer. POSIX sh, no distribution-specific package manager:
# works the same on Arch, Debian/Ubuntu, Fedora, openSUSE, Void, Alpine, NixOS
# (user profile), Gentoo...
#
#   ./install.sh                 install for this user (~/.local)
#   ./install.sh --system        install to /usr/local (needs sudo)
#   ./install.sh --autostart     also start Overspan with the desktop session
#   ./install.sh --no-udev       skip the input permission rule
#   ./install.sh --uninstall     remove what this script installed
set -eu

prefix="$HOME/.local"
system=0 autostart=0 udev=1 uninstall=0
for a in "$@"; do
  case "$a" in
    --system) system=1; prefix=/usr/local ;;
    --autostart) autostart=1 ;;
    --no-udev) udev=0 ;;
    --uninstall) uninstall=1 ;;
    -h|--help) sed -n '2,13p' "$0"; exit 0 ;;
    *) echo "unknown option: $a" >&2; exit 2 ;;
  esac
done

here=$(cd "$(dirname "$0")" && pwd)
sudo_cmd=""
[ "$(id -u)" -eq 0 ] || sudo_cmd="sudo"
as_root() { if [ -n "$sudo_cmd" ]; then $sudo_cmd "$@"; else "$@"; fi; }
put() { # put <mode> <dest> (content on stdin)
  if [ "$system" -eq 1 ]; then as_root install -D -m "$1" /dev/stdin "$2"; else mkdir -p "$(dirname "$2")"; cat > "$2"; chmod "$1" "$2"; fi
}

bin="$prefix/bin/overspan"
apps="$prefix/share/applications/overspan.desktop"
icon="$prefix/share/icons/hicolor/scalable/apps/overspan.svg"
unit="$HOME/.config/systemd/user/overspan.service"
xdg_auto="$HOME/.config/autostart/overspan.desktop"
rule=/etc/udev/rules.d/70-overspan-uinput.rules
modload=/etc/modules-load.d/overspan.conf

if [ "$uninstall" -eq 1 ]; then
  systemctl --user disable --now overspan.service 2>/dev/null || true
  for f in "$bin" "$apps" "$icon"; do
    [ -e "$f" ] || continue
    if [ "$system" -eq 1 ]; then as_root rm -f "$f"; else rm -f "$f"; fi
  done
  rm -f "$unit" "$xdg_auto"
  if [ -e "$rule" ] || [ -e "$modload" ]; then
    echo "Removing the input rule (sudo)"; as_root rm -f "$rule" "$modload"
  fi
  echo "Overspan removed. Settings are kept in ~/.config/overspan (delete it by hand if you like)."
  exit 0
fi

# pick the binary for this CPU
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7l|armv7*|armhf) arch=arm ;;
  i386|i486|i586|i686) arch=386 ;;
  riscv64) arch=riscv64 ;;
  *) echo "No Overspan build for $(uname -m). Build from source: make build" >&2; exit 1 ;;
esac
src=""
for c in "$here/overspan-linux-$arch" "$here/../dist/overspan-linux-$arch" "$here/overspan" "$here/../dist/overspan"; do
  [ -x "$c" ] && { src="$c"; break; }
done
[ -n "$src" ] || { echo "Cannot find an overspan binary next to this script (expected overspan-linux-$arch)." >&2; exit 1; }

echo "Installing $src -> $bin"
put 0755 "$bin" < "$src"

icon_svg='<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#2f55e4"/><rect x="10" y="17" width="28" height="20" rx="3" fill="none" stroke="#fff" stroke-width="4"/><rect x="30" y="27" width="24" height="20" rx="3" fill="#2f55e4" stroke="#fff" stroke-width="4"/><path d="M18 46h12" stroke="#fff" stroke-width="4" stroke-linecap="round"/></svg>'
printf '%s\n' "$icon_svg" | put 0644 "$icon"

put 0644 "$apps" <<EOF
[Desktop Entry]
Type=Application
Name=Overspan
GenericName=Extra monitors over Wi-Fi
Comment=Use phones, tablets and laptops as extra screens for this computer
Exec=$bin serve --open
Icon=overspan
Terminal=false
Categories=Utility;System;
Keywords=monitor;display;screen;tablet;second screen;extend;mirror;
EOF

if [ "$udev" -eq 1 ]; then
  if [ -w /dev/uinput ]; then
    echo "Input already works (/dev/uinput is writable)."
  else
    echo "Allowing touch, mouse and keyboard from viewers (writes $rule, needs sudo)"
    printf '%s\n' '# Overspan: let the logged-in user create virtual input devices' \
      'KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"' \
      | as_root tee "$rule" >/dev/null
    printf 'uinput\n' | as_root tee "$modload" >/dev/null
    as_root modprobe uinput 2>/dev/null || true
    if command -v udevadm >/dev/null 2>&1; then
      as_root udevadm control --reload || true
      as_root udevadm trigger --name-match=uinput || true
    else
      echo "  (no udevadm: on mdev/eudev-less systems add yourself to the input group instead)"
    fi
  fi
fi

if [ "$autostart" -eq 1 ]; then
  if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    mkdir -p "$(dirname "$unit")"
    cat > "$unit" <<EOF
[Unit]
Description=Overspan display server
PartOf=graphical-session.target
After=graphical-session.target

[Service]
ExecStart=$bin serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=graphical-session.target
EOF
    systemctl --user daemon-reload
    systemctl --user enable overspan.service
    echo "Autostart: systemd user service (starts with your next graphical session)."
    echo "  Your compositor must export its environment to systemd (uwsm, GNOME, KDE and most"
    echo "  session managers do; otherwise add: systemctl --user import-environment WAYLAND_DISPLAY"
    echo "  HYPRLAND_INSTANCE_SIGNATURE SWAYSOCK DISPLAY to your compositor's startup)."
  else
    mkdir -p "$(dirname "$xdg_auto")"
    sed 's/ --open//' "$apps" > "$xdg_auto" 2>/dev/null || cp "$apps" "$xdg_auto"
    echo "Autostart: XDG autostart entry ($xdg_auto). Non-XDG compositors: run 'overspan serve' at startup."
  fi
fi

case ":$PATH:" in *":$prefix/bin:"*) ;; *) echo "Note: $prefix/bin is not on your PATH." ;; esac
echo
"$bin" doctor || true
echo
echo "Done. Start it with:  overspan serve --open"
