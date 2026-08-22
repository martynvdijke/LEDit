## Why

LEDit's current administration UI is visually generic, inconsistently composed, and dominated by Bootstrap defaults. It does not communicate the product's LED-matrix/nerdy identity, and the live feed leaves too much space unused while burying status and controls. A cohesive, responsive redesign is needed now so the UI feels intentional and current without forcing a risky rewrite of the Go server-rendered application.

## What Changes

- Replace the Bootstrap-CDN presentation layer with a project-owned, compiled frontend asset pipeline while retaining Go templates and existing server routes.
- Establish a cohesive “CRT Control Room” visual system: dark control-room surfaces, phosphor green/amber accents, pixel-aware typography, monospace telemetry, clear status indicators, and restrained matrix/CRT texture.
- Redesign the shared application shell, navigation, responsive mobile navigation, page headers, cards, forms, tables, alerts, buttons, badges, and empty/loading/error states.
- Redesign the live feed as a purposeful control-room display with a prominent media stage, connection/source/next telemetry, grouped playback controls, fullscreen behavior, and E-Ink affordances.
- Apply the shared system consistently across dashboard, settings, schedules, devices, datasource forms, logs, notifications, analytics, theme, and account pages without changing their domain behavior.
- Add a coherent LEDit brand mark, favicon/app icons, PWA icons, and reusable icon treatment for navigation and actions.
- Preserve accessibility, keyboard operation, reduced-motion behavior, E-Ink mode, WebSocket feed behavior, and existing URLs/forms.

## Capabilities

### New Capabilities

- `control-room-visual-system`: Defines the reusable visual tokens, typography, components, responsive rules, accessibility states, and branding assets for the administration UI.
- `live-feed-control-room`: Defines the redesigned live-feed layout and interaction states while preserving current feed controls and connection behavior.

### Modified Capabilities

<!-- No existing OpenSpec capability specifications are present; this change introduces the UI contracts above. -->

## Impact

- `web/templates/base.html`, `web/templates/index.html`, and all admin templates will consume shared compiled assets instead of relying on Bootstrap defaults and page-local styling.
- New frontend source/build files, generated static CSS/JS, icon artwork, font loading, and PWA metadata will be added under the existing web/static structure.
- Go template data, handlers, WebSocket messages, routes, persistence, and datasource behavior remain compatible; changes are presentation-focused.
- Package/task/Docker/build configuration will gain a reproducible frontend asset build step and CI validation.
- Existing Playwright coverage will need selector-stable updates and new visual/behavioral assertions for responsive navigation, live-feed controls, branding, and key form states.
