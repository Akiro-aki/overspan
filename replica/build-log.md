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
| V03 | 2026-11-21 | partial | pen pressure and tilt; keyboard shortcuts the browser itself grabs (Ctrl+W, Ctrl+T, Super) never reach the page | Keyboard users could not reach the toolbar because every key is forwarded. Added a local Alt+Shift+O shortcut |
| V04 | 2026-11-21 | done | none | none |
| pen | 2026-11-21 | done | pressure not checked by eye in a drawing app | Hyprland lists the uinput tablet as `overspan-pen`; hover maps within 1px |
| C01 | 2026-11-21 | done | QR scanning (the camera would need a decoder library) | This machine's ufw drops broadcasts, so discovery also probes saved computers directly |
| C02 | 2026-11-21 | done | none | none |
| C03 | 2026-11-21 | done | none | A sleeping (DPMS off) monitor gives no frames. The server now tells the viewer after 4 s, in both viewers |
| C04 | 2026-11-21 | done | none | none |
| W01 | n/a | not started | whole page | n/a |
| W02 | n/a | not started | whole page | n/a |

Notes
- Screens were built together in one pass (shared engine and primitives), so
  commits are grouped by layer rather than one per screen.
- The X11 and Sway backends compile and follow their tools' documented
  commands, but this machine runs Hyprland, so they have not run against a live
  session. Treat them as untested.
- Clone screenshots in `replica/clone-screens/` capture the real desktop
  (V03 shows the lock screen of this machine). Re-shoot before publishing them.
