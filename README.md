# Overspan

Use a phone, tablet or second laptop as an extra monitor for your Linux
desktop, over Wi-Fi. The other device needs only a web browser.

- **Extra monitor**: a real new output appears in your layout, beside your others.
- **Same picture**: share a monitor you already have.
- Touch, mouse and keyboard on the device control the computer.
- One static binary per CPU architecture: no libraries, any distribution.

## Install

```sh
make all                       # builds dist/overspan-linux-{amd64,arm64,386,arm,riscv64}
./packaging/install.sh         # installs to ~/.local, sets up input permission (asks for sudo once)
overspan serve --open          # starts the server and opens the dashboard
```

On the other device, scan the QR code on the dashboard (or open the address
it shows), tap **Connect**, then accept the device on the computer.

`overspan doctor` tells you what works in your session and how to fix what
does not.

### AppImage

Prefer one file and no install? Download `Overspan-<version>-<arch>.AppImage`
(x86_64, aarch64, i686 or armhf), make it executable and double-click it: it
starts Overspan and opens the dashboard. From a terminal it takes the same
commands as `overspan`:

```sh
chmod +x Overspan-0.1.0-x86_64.AppImage
./Overspan-0.1.0-x86_64.AppImage install-udev   # once: lets the device's touch, mouse and keyboard work (asks for sudo)
./Overspan-0.1.0-x86_64.AppImage                # serve and open the dashboard
```

Without FUSE, add `--appimage-extract-and-run`. `make appimage` builds them.

## Android app

The other device can also use the native Android app (Android 8 or newer). It
finds Overspan computers on the network by itself, remembers them, runs truly
full screen, keeps the screen awake and sends stylus pressure and tilt.

```sh
cd android && ./build.sh       # needs a JDK and the Android SDK (platform 35, build-tools 35.0.0)
adb install build/overspan-viewer.apk
```

The build uses the SDK tools directly (aapt2, javac, d8, apksigner): no Gradle
and no third-party libraries. The APK is about 85 KB and debug-signed. Set
`OVERSPAN_KEYSTORE` and `OVERSPAN_KS_PASS` to sign with your own key.

## Windows app

A Windows PC can be the extra screen too: `overspan-viewer.exe` (64-bit
Windows 10 or 11; Windows on Arm runs it through x64 emulation). It finds
computers on the network, remembers them, opens the picture full screen or in
a window (the extra monitor follows the window's size), and sends mouse,
keyboard, wheel and pen input with pressure and tilt. The Windows key goes to
the computer while the picture has focus. Ctrl+Alt+Del and Win+L stay with
Windows. Move the pointer to the top edge, or press Alt+Shift+O, for the
toolbar.

```sh
make windows                   # cross-compiles dist/overspan-viewer.exe from any OS; needs Go only
overspan-viewer.exe 192.168.1.20   # optional: open the connect sheet for an address
```

It is a single static exe with no installer and no runtime. It is written in
Go against the Win32 API through the standard library: no cgo and no
third-party modules. Settings live in `%APPDATA%\Overspan\viewer.json`. The
exe is not code-signed, so SmartScreen warns on first run.

## What works where

| Desktop | Extra monitor | Same picture | Input |
|---------|---------------|--------------|-------|
| Hyprland | yes (tested) | yes (tested) | yes (tested) |
| Sway | yes (not run live yet) | yes | yes |
| river, labwc, Wayfire, niri | no | yes | yes |
| Any X11 desktop | yes, via xrandr (not run live yet) | yes, needs ffmpeg | yes |
| GNOME / KDE on Wayland | not yet | not yet | yes |

Input goes through the kernel's `/dev/uinput`, so it works everywhere. The
installer adds a `uaccess` udev rule; without udev, add yourself to the
`input` group.

## Ports

- TCP 7940: viewer page and stream (`--port` changes it)
- UDP 7941: discovery replies for native viewers

Open TCP 7940 (and UDP 7941 for discovery by the Android and Windows apps) in your firewall if devices cannot connect or find the computer.

## Security

The dashboard only opens on the computer itself (`http://localhost:7940/admin`).
Devices on the network can only reach the viewer. New devices must be
accepted, and you can add a PIN. Traffic is not encrypted, so use it on
networks you trust.

## Development

```sh
make test     # unit tests (QR codes are decoded with zbarimg when it is installed)
make e2e      # full session against the running Hyprland/Sway (creates and removes a monitor)
make shots    # screenshots of every screen through headless Chromium
tests/android-e2e.sh   # the Android app in a headless emulator, driven through its UI
make windows-e2e       # the Windows app under Wine (null display driver: nothing shows), driven through its UI
```

Design notes, the protocol and the feature matrix are in `replica/`.
