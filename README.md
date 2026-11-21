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

Open TCP 7940 (and UDP 7941 for the Android app's discovery) in your firewall if devices cannot connect or find the computer.

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
```

Design notes, the protocol and the feature matrix are in `replica/`.
