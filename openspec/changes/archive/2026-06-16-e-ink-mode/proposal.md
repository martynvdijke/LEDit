## Why

E-ink note-taking tablets (Boox, reMarkable, Supernote, etc.) are increasingly used for monitoring dashboards and live feeds, but LEDit's web UI includes animations, fade transitions, low-contrast color schemes, and rapidly updating elements that cause excessive screen refreshes and poor readability on e-ink displays. Adding an e-ink mode makes LEDit fully usable on these devices — eliminating flicker, maximizing contrast, and optimizing touch targets for pen/capacitive input.

## What Changes

- **E-Ink Mode Toggle** — A persistent user preference (cookie-based + server-side setting) enabling e-ink optimization across all pages
- **E-Ink CSS Theme** — High-contrast black-on-white (or user-configurable) stylesheet that eliminates animations, transitions, and low-contrast elements
- **Reduced Motion** — Disable all CSS transitions (fade-in/out on images, marquee scrolling, clock second updates) when e-ink mode is active
- **Optimized Touch Targets** — Increase button, link, and form control sizing for imprecise e-ink touch/pen input
- **Feed Page Optimization** — Remove matrix overlay grid, clock overlay live updates, and marquee animation in e-ink mode; use static refresh instead of continuous updates
- **Sidebar & Navigation** — Convert sidebar to always-visible wider layout with larger tap targets
- **Server-Side Rendering Flag** — Ability to optionally render content frames at lower update frequency for e-ink WebSocket clients
- **PWA Manifest Update** — Add `display: "standalone"` e-ink friendly metadata and theme-color for e-ink tablets

## Capabilities

### New Capabilities
- `e-ink-theme`: High-contrast CSS theme and stylesheet optimized for e-ink displays — removes animations, transitions, low-contrast colors, and flashing elements across all admin and feed pages
- `e-ink-feed`: Optimized live feed mode for e-ink — disables matrix grid overlay, clock live-updates, marquee scroll, and fade transitions; uses static image display with manual or timed refresh
- `e-ink-toggle`: Persistent e-ink mode toggle accessible from the sidebar and stored as a user preference (cookie + optional server-side setting)

### Modified Capabilities

None — no existing specs to modify.

## Impact

- **Web UI templates** (`web/templates/`) — All HTML templates need e-ink mode awareness (conditional CSS classes)
- **Static CSS** (`web/static/`) — New `eink.css` stylesheet; possible PWA manifest update
- **Live feed page** (`web/templates/index.html`) — Significant JS changes to disable animations, clock, marquee
- **Go handlers** (`handlers/`) — New middleware/handler for e-ink cookie; feed control may need e-ink rendering rate option
- **Sidebar** (`web/templates/admin/sidebar.html`) — Add e-ink toggle button
- **No database changes** — Preference stored as cookie; optional server-side setting via existing settings infrastructure
