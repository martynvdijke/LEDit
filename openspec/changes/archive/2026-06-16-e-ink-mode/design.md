## Context

LEDit's web UI is built with Bootstrap 5.3, Go HTML templates, and vanilla JS. The live feed page (`index.html`) uses CSS animations (marquee scrolling, fade transitions), a 1-second clock update interval, a WebSocket connection for real-time image display, and an overlay matrix grid. The admin interface uses Bootstrap's offcanvas sidebar, colored cards, and standard form controls.

E-ink displays differ from LCD/OLED in several critical ways:
- **Slow refresh rate** (200-1000ms) — animations cause visible flickering and ghosting
- **No backlight** — relies on ambient light; high contrast (black/white) is essential
- **Capacitive/pen input** — less precise than mouse; needs larger touch targets (≥48px)
- **Partial refresh limitations** — rapid element updates degrade display quality
- **Power consumption** — screen updates are the primary power draw; minimizing updates extends battery life

The solution is a CSS + JS layer that transforms the existing UI when e-ink mode is active, without requiring a separate codebase or duplicating templates.

## Goals / Non-Goals

**Goals:**
- Provide a working e-ink mode toggle accessible from any page
- Eliminate all CSS animations and transitions when e-ink mode is active
- Replace Bootstrap's low-contrast color scheme with high-contrast black-on-white (or white-on-black)
- Disable rapid-updating UI elements (clock, marquee, matrix overlay) on the feed page
- Increase touch target sizes to ≥48px for sidebar links, buttons, and form controls
- Persist the e-ink preference across sessions (cookie-based, with optional server-side default)
- Minimize changes to existing Go backend — use CSS/JS frontend approach where possible

**Non-Goals:**
- Server-side e-ink image dithering or format conversion (the render pipeline outputs full-color PNG — that's a separate optimization)
- Dedicated e-ink hardware support or e-paper display drivers
- Rewriting templates or creating a separate mobile app
- Accessibility improvements beyond e-ink use case (separate concern)
- Changing the datasource rendering engine for e-ink output

## Decisions

### Decision 1: Cookie-based preference with server-side default
- **Choice**: Store e-ink preference in a cookie (`ledit_eink=true`), with middleware that injects `EInkMode` into template context
- **Alternatives considered**: (1) LocalStorage only — not accessible server-side for template decisions; (2) Server-side DB setting per user — overkill for single-admin; (3) URL parameter — not persistent
- **Rationale**: Cookie is readable by both Go middleware (for template context) and client JS (for dynamic adjustments). Zero database changes needed. Toggle sets/clears the cookie via a simple endpoint or JS.

### Decision 2: CSS-only e-ink theme via feature class on `<body>`
- **Choice**: When e-ink mode is active, add `.eink-mode` class to `<body>`. All e-ink overrides live in a single `eink.css` file using `.eink-mode` ancestor selector
- **Alternatives considered**: (1) Separate template set — maintenance burden; (2) Media query for `prefers-reduced-motion` + custom CSS variables — insufficient for layout/target changes; (3) JavaScript runtime manipulation — fragile
- **Rationale**: Single CSS file with `.eink-mode` prefix is clean, debuggable, and requires zero JS for visual changes. Bootstrap's utility classes are overridden with `!important` only where needed, via specific selectors.

### Decision 3: Disable, not replace, animated feed elements
- **Choice**: In e-ink mode, the feed page hides the matrix overlay, stops the clock interval, converts the marquee to static text, and removes image fade transitions. User can still manually refresh by clicking a "Refresh" button or using a configurable interval (default 30s).
- **Alternatives considered**: (1) Keep all features running but hide them — wasteful; (2) Replace with e-ink optimized canvas render — complex, minimal benefit
- **Rationale**: E-ink users want a stable, static display. Manual or slow-timer refresh matches how e-ink devices are used for monitoring. The WebSocket feed continues running in the background.

### Decision 4: Toggle button in sidebar + dedicated endpoint
- **Choice**: Add an "E-Ink Mode" toggle in the sidebar (visible on all admin pages) that calls `POST /admin/eink/toggle` and sets/clears the cookie. Feed page also gets the toggle. A settings page option sets the server-side default.
- **Alternatives considered**: (1) Settings-only toggle — requires navigation to settings; (2) Auto-detect e-ink user agent — unreliable
- **Rationale**: Visible toggle makes discovery easy. Cookie is immediate — no page load required for the server-side toggle to register since middleware reads it on every request.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| E-ink CSS overrides may conflict with future Bootstrap upgrades | Keep all overrides in single `eink.css` with `.eink-mode` scope; document upgrade procedure |
| Cookie-based preference lost on cookie clear/expiry | Set 1-year expiry; provide server-side default in Settings |
| WebSocket still sends full-frame updates to e-ink clients | Acceptable — client simply ignores updates between manual refreshes; future optimization could throttle server-side |
| Users miss animations/clock/marquee in e-ink mode | E-ink mode is opt-in via toggle; default experience unchanged |
| `.eink-mode` class not present on all pages immediately after toggle (full page load needed) | Toggle endpoint redirects to referrer with new cookie; JS can also add class immediately on toggle click for instant feedback |
