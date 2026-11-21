# Recon: second-screen-over-network software (reference: spacedesk.net)

Clean-room map, written from the reference product's public marketing pages
(home, download, facts) read as a user on 2026-11-21, plus general knowledge of
how this product category works. No code, assets, copy or network traffic of
the original was inspected or reused. The public site documents no settings,
ports or protocol, so everything below the "Product" section is our own design.

## Product (what the reference offers, in our words)

- A **primary** machine (Windows driver, macOS beta) gains extra displays.
- **Secondary** devices show one: Android, iOS, Fire tablets, Windows apps,
  and an HTML5 browser viewer (beta; the reference says Firefox is too slow).
- Links over Wi-Fi, LAN or USB.
- Modes: **extend** the desktop, **mirror** it (with audio), and a **video
  wall / signage** mode with rotation at any angle.
- Input flows back: touch, stylus, keyboard, mouse ("software KVM").
- Extras named: sound redirection, remote access, webcam "coming soon".
- Licensing: free for personal use, paid for business.

## Our version: Overspan

Linux-first. The user asked for a Linux "driver" that works on every
distribution, so the Linux server is the core deliverable; the viewer is the
browser (it runs on every phone, tablet, PC and TV with a modern browser).

## Screen inventory

| ID  | Screen | Purpose | Key components | States |
|-----|--------|---------|----------------|--------|
| S01 | Dashboard: Overview | Join a device, approve requests, see who is connected and the monitor layout | header with on/off switch, QR card, address list with copy, pending request card, layout map, device list | loading (skeletons), empty (no devices), pending request, live devices, paused, server unreachable banner, no network |
| S02 | Dashboard: Display | Default mode, mirror source, placement, size cap, fps, quality, cursor, keep-alive | settings rows, segmented controls, selects, sliders, switch | loaded, extend unavailable (disabled option), saving toast |
| S03 | Dashboard: Security | Name, approval, PIN, input on/off, remembered devices | text input, PIN input with validation, switches, trusted list | empty trusted list, invalid PIN, saved |
| S04 | Dashboard: System | What works in this session and how to fix the rest; activity log | check list with status badges and fix commands, log | loading, all ok, warn/fail with fix, check error |
| V01 | Viewer: Connect | Pick mode and name, connect | server card with status badge, segmented mode, name input, connect button | checking, ready, paused, PIN needed, offline, extend unavailable |
| V02 | Viewer: Status | Everything between "Connect" and the picture | spinner art, title, text, PIN form, retry/back | connecting, waiting for approval, adding a monitor, PIN, wrong PIN, declined, timed out, paused, lost |
| V03 | Viewer: Display | The remote screen with input | canvas, pull tab, toolbar (keyboard, touch mode, full screen, settings, disconnect), stats, reconnect banner | streaming, toolbar open, reconnecting, input off |
| V04 | Viewer: Picture and input | Resolution, quality, fps, touch mode, stats, rotation | sheet dialog, select, sliders, segmented, switches | open/closed |
| W01 | Website: Home | Product pitch and install | | not built |
| W02 | Website: Download | Per-architecture binaries | | not built |

## Flows

1. **Extend** (core loop): open viewer URL / scan QR → Connect → computer
   approves → virtual monitor is created beside the others → picture streams →
   touch/mouse/keyboard drive it → disconnect → monitor kept briefly, then removed.
2. **Mirror**: same, but an existing monitor is shared.
3. **Approve**: dashboard shows request → accept once / accept and remember / decline.
4. **PIN**: viewer is asked for the PIN before anything reaches the dashboard.
5. **Rotate**: device turns → monitor is resized to the new orientation.
6. **Reconnect**: link drops → viewer retries → same monitor comes back.
7. **Pause**: dashboard switch off → every viewer is told and disconnected.
8. **Diagnose**: System page / `overspan doctor` lists what works and the fix
   for what does not.

## Data model (inferred, then designed)

- **Settings**: enabled, serverName, requireApproval, pin, defaultMode,
  mirrorOutput, placement, maxLongEdge, fps, quality, showCursor, allowInput,
  keepSeconds, trusted[].
- **TrustedDevice**: deviceId, name, added, lastSeen.
- **ViewerSession** (memory only): id, deviceId, name, address, agent, mode,
  state (waiting, starting, live), output, size, since, fps, KB/s.
- **Output**: name, x, y, logical w/h, pixel w/h, scale, virtual, primary.
- Viewer-side (localStorage): deviceId, device name, prefs, recent servers.
