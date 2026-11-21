# Build log

| ID | Date | Status | Missing | Harder than expected |
|----|------|--------|---------|----------------------|
| engine | 2026-11-21 | done | GNOME/KDE backend, EVDI, audio | Ryoku's `ryoku-monitor` reloads the monitor config 1–3 s after any hotplug. That reload wiped the runtime mode rule and switched the laptop panel back on. Fixed by waiting for the layout to settle, then placing, applying and re-verifying. Hyprland 0.57's Lua config also rejects `hyprctl keyword monitor`, so the backend falls back to `hyprctl eval hl.monitor{…}` |
| S01 | 2026-11-21 | done | none | `[hidden]` lost to `display:flex` on `.stack`/`.banner`: all pages and the "lost contact" banner showed at once. Fixed globally in `ui.css` |
| S02 | 2026-11-21 | done | live preview of placement | none |
| S03 | 2026-11-21 | done | none | none |
| S04 | 2026-11-21 | done | none | none |
| V01 | 2026-11-21 | done | list of servers found on the LAN (browsers cannot send UDP) | none |
| V02 | 2026-11-21 | done | none | Creating a monitor takes about 5 s on Ryoku (settle time), so a "setting up" state was added |
| V03 | 2026-11-21 | partial | keyboard shortcuts the browser itself grabs (Ctrl+W, Ctrl+T, Super) never reach the page | Keyboard users could not reach the toolbar because every key is forwarded. Added a local Alt+Shift+O shortcut. Pen pressure and tilt added later with the tablet device |
| V04 | 2026-11-21 | done | none | none |
| pen | 2026-11-21 | done | pressure not checked by eye in a drawing app | Hyprland lists the uinput tablet as `overspan-pen`; hover maps within 1px |
| C01 | 2026-11-21 | done | QR scanning (the camera would need a decoder library) | This machine's ufw drops broadcasts, so discovery also probes saved computers directly |
| C02 | 2026-11-21 | done | none | Android's one-time "Viewing full screen" notice covers immersive apps until tapped; the UI test acknowledges it up front |
| C03 | 2026-11-21 | done | none | A sleeping (DPMS off) monitor gives no frames. The server now tells the viewer after 4 s, in both viewers |
| C04 | 2026-11-21 | done | none | Dialog buttons rendered in ALL CAPS from the platform theme; set to sentence case. The toolbar never auto-hid again after Settings closed; fixed |
| fix | 2026-11-21 | done | n/a | **Monitor leak**: stopping the server while a viewer was connected left that viewer's virtual monitor on the desktop (the session kept it for the reconnect grace period, and the server exited without waiting). Found when three test monitors were left on this desktop. Shutdown now removes monitors at once and waits for sessions; `tests/shutdown-leak.mjs` guards it |
| D01 | 2026-11-22 | done | QR scanning (no camera API used) | Child windows paint over the parent, so a dimmed list showed through the sheet. Page controls now hide while a sheet is open and the parent paints their image under the dim layer |
| D02 | 2026-11-22 | done | none | A server shutdown sent the same `closed` code as a dashboard kick, so no viewer could reconnect after a restart. Added a `shutdown` code; all three viewers retry on it |
| D03 | 2026-11-22 | partial | pen and the Windows key not run on real Windows (Wine has neither); no clipboard | The toolbar only stayed up while the mouse moved; now it also stays while the pointer rests on it |
| D04 | 2026-11-22 | done | none | none |
| W01 | n/a | not started | whole page | n/a |
| W02 | n/a | not started | whole page | n/a |

Notes
- Screens were built together in one pass (shared engine and primitives), so
  commits are grouped by layer rather than one per screen.
- The X11 and Sway backends compile and follow their tools' documented
  commands, but this machine runs Hyprland, so they have not run against a live
  session. Treat them as untested.
- Android: `tests/android-e2e.sh` drives the real app in a headless emulator
  (14 checks: add by address, validation, status, approval, streaming, a tap
  moving the desktop pointer, toolbar, settings, disconnect, saved list).
- Windows: `make windows-e2e` drives the real exe under Wine with the null
  display driver, through `windows/tools/winprobe` (26 checks: add by
  address, validation, status, approval, full-screen size, pointer mapping,
  toolbar, settings, window resize following, reconnect after a server
  restart, disconnect, PIN wrong and right, paused computer, cleanup). Wine is
  not Windows: GDI+, DPI scaling and pen input still need a run on real Windows.
- Clone screenshots in `replica/clone-screens/` show the machine they were
  taken on (hostname, addresses, desktop), so they stay local and are not in
  git. `make shots`, `tests/android-e2e.sh` and `make windows-e2e` recreate them.
