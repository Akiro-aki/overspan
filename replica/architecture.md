# Architecture: Overspan

## Stack (boring on purpose)

- **Server**: one Go program, standard library only, built with `CGO_ENABLED=0`.
  The result is a static ELF with no runtime dependencies: it runs on glibc
  and musl distributions alike. Built for amd64, arm64, 386, armv7, riscv64.
- **Viewer and dashboard**: plain HTML, CSS and JavaScript embedded in the
  binary (`go:embed`). No build step, no framework, no CDN.
- **Android viewer**: Java against the platform SDK, built with the raw SDK
  tools (no Gradle, no libraries).
- **Windows viewer**: Go against Win32 through `syscall` (no cgo, no
  modules), cross-compiled to one static x64 exe. `windows/internal/client`
  holds the protocol, frame decoding, discovery, settings and the connection
  state machine with no Windows code, so it is unit-tested on Linux.
  `windows/cmd/overspan-viewer` is the UI: owner-drawn BUTTONs, EDITs and
  trackbars as real child windows, so Tab, Enter, Esc and screen readers
  behave as in any dialog. Painting is GDI+ for shapes and GDI for ClearType
  text, from the same tokens as the web and Android. `windows/tools/winprobe`
  drives it for tests.
- **No database**: settings in `$XDG_CONFIG_HOME/overspan/config.json`;
  sessions live in memory.

## "Works on every distro": where the differences really are

Distribution differences (package manager, libc, init system) are removed by
the static binary, the POSIX installer and the kernel interfaces it uses.
What still differs is the **desktop**, so that is where the backends are:

| Concern | Mechanism | Coverage |
|---------|-----------|----------|
| Input injection | `/dev/uinput` (kernel), absolute pointer + keyboard | every session: X11, all Wayland compositors, console |
| Input permission | udev `uaccess` rule (or `input` group) | systemd-udev, eudev; mdev falls back to the group |
| Screen capture (Wayland) | own pure-Go Wayland client speaking `wlr-screencopy` over the socket, shm buffers via SCM_RIGHTS | Hyprland, Sway, river, labwc, Wayfire, niri, any wlroots-style compositor |
| Screen capture (X11) | `ffmpeg -f x11grab` raw frames | any X server (ffmpeg is in every distro) |
| Virtual monitor: Hyprland | `hyprctl output create headless` + Lua `hl.monitor` (0.5x) or `keyword monitor` (older) | tested live on 0.57.2 |
| Virtual monitor: Sway | `swaymsg create_output` / `output … unplug` | written, not run live |
| Virtual monitor: X11 | `VIRTUAL*` output + CVT-RB modeline computed in Go, else `--fb` + `--setmonitor` | written, not run live |
| Virtual monitor: other wlroots | none exposed by the compositor | mirror only |
| GNOME / KDE Wayland | needs the screencast portal (`RecordVirtual` / virtual source) over D-Bus + PipeWire | **not built** |
| Kernel fallback | EVDI virtual DRM connectors | **not built** |

## Packages (`server/internal`)

- `wl`: Wayland wire protocol, registry, `wl_output` names, `wl_shm`,
  `zwlr_screencopy_manager_v1` with double-buffered shm and copy_with_damage
  (idle screens cost nothing).
- `capture`: one interface over screencopy and x11grab.
- `encode`: 64-row strips × 64-px tiles; only changed tiles, JPEG-encoded in
  parallel across cores; one binary message per frame.
- `input`: uinput devices (raw ioctls, no cgo), evdev keymap from
  `KeyboardEvent.code`, text-to-keys for soft keyboards, stuck-key release.
- `vdisplay`: backends, layout, placement, bounds, modeline maths.
- `ws`: RFC 6455 server (masking, fragmentation, ping/pong, size limits).
- `qr`: QR encoder (byte mode, level M, versions 1–10) → SVG.
- `server`: HTTP routes, dashboard API + SSE, sessions, approval, discovery.

## Wire protocol (our own)

WebSocket `/ws`. First message from the viewer (text):

```json
{"t":"hello","device":"<random id>","name":"…","mode":"extend|mirror",
 "width":1600,"height":2560,"scale":2,"pin":"","quality":70,"fps":30}
```

Server → viewer text: `wait`, `setup`, `ready {display:{name,w,h}, input}`,
`size {w,h}`, `input {on}`, `error {code,text}` (codes: pin, pin-wrong,
denied, timeout, disabled, display, capture, closed, shutdown, protocol).
`shutdown` means Overspan itself is stopping. Viewers that were streaming
treat it as a drop and keep reconnecting. `closed` (paused, or sent away from
the dashboard) is final.

Server → viewer binary frame (little-endian):
`'F' | flags u8 (bit0 = full) | seq u32 | width u16 | height u16 | count u16 |
count × (x u16 | y u16 | w u16 | h u16 | len u32 | JPEG)`

Viewer → server: `ack {sq}` (flow control: at most two frames in flight),
`p {e: move|down|up, x, y (0..1), b}`, `rel {dx, dy}`, `btn {b, d}`,
`w {dx, dy}` (1/120 detent units), `k {c: KeyboardEvent.code, d}`,
`txt {s}`, `pen {e: hover|down|move|up|leave, x, y, p, tx, ty, b, r}`,
`cfg {q, f}`, `resize {w, h}`, `refresh`, `bye`.

UDP `port+1`: `OVERSPAN?` → JSON card, used by the Android and Windows viewers.

## Security model

- Viewer page and WebSocket are open to the LAN; nothing else is.
- Dashboard and API answer only to loopback peers **and** a localhost Host
  header (stops DNS rebinding) **and** a per-run key embedded only in the
  page served to localhost (stops CSRF).
- New devices need approval (default) and/or a PIN; remembered devices are
  revocable. Input can be switched off globally.
- Limits: 1 MiB client messages, 60-char names, 256-char text bursts,
  10 s hello deadline, 2 min approval timeout.
- Not done: TLS. Traffic is plain HTTP/WS on the LAN.

## Build order (as executed)

1. Wayland capture spike → JPEG strips (proves the hard part).
2. uinput, keymap. 3. Hyprland backend (+ Sway, X11). 4. WebSocket, QR.
5. Server: sessions, approval, dashboard API. 6. Tokens and primitives.
7. Viewer V01–V04. 8. Dashboard S01–S04. 9. e2e + screenshots. 10. Packaging.
11. Android viewer C01–C04. 12. Windows viewer D01–D04.
