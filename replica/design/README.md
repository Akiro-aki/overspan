# Overspan design system

Original palette and components; nothing taken from the reference product.

- **Tokens**: `server/internal/web/static/assets/tokens.css` (the single source;
  the binary embeds it). Colour roles for light and dark, type scale, 4 px
  spacing grid, radii, shadows, layout sizes, motion.
- **Primitives**: `server/internal/web/static/assets/ui.css`.
- **Logo**: `server/internal/web/static/assets/icon.svg`: two offset monitors on a
  cobalt tile.

## Palette

| Role | Light | Dark |
|------|-------|------|
| accent | `#2f55e4` cobalt | `#6d8bff` |
| background / surface | `#f4f5f8` / `#ffffff` | `#0f1218` / `#171b23` |
| text / muted | `#161a23` / `#596174` | `#e9ecf3` / `#a3abbd` |
| ok / warn / danger | `#187a4a` / `#9a5b00` / `#c22b3a` | `#4cc98a` / `#f0b04a` / `#ff6b78` |

Checked against WCAG AA (4.5:1) for the pairs the UI uses: body text 17.4:1
(light) / 14.6:1 (dark); muted 5.3–6.8:1; faint 5.2:1 / 5.6:1 (darkened after
the first check failed at 3.2:1); badges on their soft backgrounds 4.6–7.3:1;
the tightest pair is light-mode status text on its tint at 4.59:1.

## Components and states

| Component | Variants | States |
|-----------|----------|--------|
| `.btn` | primary, default, ghost, danger, icon, lg, block | hover, focus-visible, disabled |
| `.switch` | n/a | off, on, focus |
| `.segmented` | inline, block | checked, focus, disabled |
| `.input` / `.select` | text, mono, PIN | focus, invalid (`aria-invalid`) |
| `.card` | default, request (accent) | n/a |
| `.badge` | ok, warn, fail, accent, plain | n/a |
| `.setting` row | label + hint + control | wraps to stacked under 760 px |
| `.empty` | n/a | n/a |
| `.skeleton` | text, block, QR, layout | animated; static with reduced motion |
| `.banner` | warn, danger, info | n/a |
| `.toast` | n/a | enters, auto-dismisses |
| `dialog.sheet` | n/a | modal, Esc closes |
| viewer toolbar (`.v-tool`) | default, pressed, danger | labels hide under 480 px |
