## 1. Server-Side: E-Ink Mode Toggle Infrastructure

- [x] 1.1 Add `eink_mode` boolean field to Settings model/DB (optional, for server-side default)
- [x] 1.2 Create e-ink middleware that reads `ledit_eink` cookie and injects `EInkMode` into template context
- [x] 1.3 Add `POST /admin/eink/toggle` endpoint that sets/clears `ledit_eink` cookie with 1-year expiry
- [x] 1.4 Add e-ink default toggle to admin Settings page (form field + save handler)
- [x] 1.5 Update `handlers/server.go` route setup to register new e-ink routes

## 2. CSS: E-Ink Theme Stylesheet

- [x] 2.1 Create `web/static/eink.css` with all rules scoped under `.eink-mode` selector
- [x] 2.2 Implement high-contrast color scheme overrides (body bg white, text black, bordered cards)
- [x] 2.3 Disable all CSS animations, transitions, keyframe animations, and hover effects
- [x] 2.4 Increase touch targets: sidebar links, buttons, form controls to minimum 48px
- [x] 2.5 Simplify visual noise: hr elements, table borders, dropdown menus, sidebar separators
- [x] 2.6 Add fullscreen feed pure black background override

## 3. Templates: E-Ink Mode Awareness

- [x] 3.1 Update `base.html`: conditionally add `.eink-mode` class to `<body>` and include `eink.css`
- [x] 3.2 Update `index.html` (live feed): add `.eink-mode` body class and `eink.css` include
- [x] 3.3 Update `sidebar.html`: add "E-Ink Mode" toggle button with current state (On/Off)
- [x] 3.4 Add e-ink toggle button to feed page controls area

## 4. Feed Page: E-Ink JavaScript Optimization

- [x] 4.1 Disable matrix overlay canvas rendering in e-ink mode
- [x] 4.2 Clear clock overlay update interval in e-ink mode; show static timestamp
- [x] 4.3 Disable marquee scrolling animation; display marquee text as static
- [x] 4.4 Remove image fade-in/out transition logic on WebSocket message
- [x] 4.5 Add "Refresh Display" button that sends WebSocket `next` action
- [x] 4.6 Implement configurable auto-refresh interval (default 30s, override via cookie)
- [x] 4.7 Simplify WebSocket status display (plain text, no color classes)

## 5. Client-Side: Instant Toggle Feedback

- [x] 5.1 Add inline JS to toggle button that immediately adds/removes `.eink-mode` class on `<body>`
- [x] 5.2 Ensure e-ink.css is loaded on all pages (it's inactive until `.eink-mode` is present)

## 6. Verification

- [x] 6.1 Test e-ink toggle on admin pages — confirm cookie is set/cleared and page reloads
- [x] 6.2 Test e-ink toggle on live feed page — confirm animations/clock/marquee disabled
- [x] 6.3 Test touch target sizing on mobile viewport widths
- [x] 6.4 Test server-side default setting — confirm cookie-less users get correct mode
- [x] 6.5 Run `task pre-push` (gofmt, tests, build)
