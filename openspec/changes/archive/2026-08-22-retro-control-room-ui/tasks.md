## 1. Frontend pipeline and branding

- [x] 1.1 Add a lockfile-backed Vite/TypeScript/CSS build entrypoint and scripts for development, production assets, and icon generation.
- [x] 1.2 Add the LEDit SVG mark, deterministic favicon/PWA raster generation, and wire generated assets into manifest and document metadata.
- [x] 1.3 Add static asset serving/build integration to Taskfile and Docker/CI so a clean checkout produces the assets required at runtime.

## 2. Shared visual system

- [x] 2.1 Create token-first CRT Control Room CSS for surfaces, phosphor/amber semantics, typography, spacing, responsive breakpoints, focus states, and reduced motion.
- [x] 2.2 Create reusable styles and progressive-enhancement modules for shell navigation, panels, buttons, badges, forms, tables, alerts, loading states, empty states, and errors.
- [x] 2.3 Add explicit E-Ink overrides that remove decorative effects and preserve high-contrast, text-based status communication.
- [x] 2.4 Refactor `base.html` and the sidebar partial to semantic landmarks, shared asset loading, grouped navigation, active state, mobile navigation, and stable accessibility/data hooks.

## 3. Live feed redesign

- [x] 3.1 Refactor `index.html` into the responsive control-room composition with a bounded media stage, telemetry, overlays, and grouped actions.
- [x] 3.2 Move feed WebSocket/reconnect/control behavior into a compiled module without changing routes, actions, message formats, or E-Ink timing behavior.
- [x] 3.3 Implement accessible connection, waiting, receiving, reconnecting, media-transition, fullscreen, and E-Ink visual states.
- [x] 3.4 Verify keyboard/touch operation and narrow viewport layout for all feed controls, including fullscreen exit and E-Ink refresh.

## 4. Admin template migration

- [x] 4.1 Replace Bootstrap-dependent markup and inline presentation in dashboard, settings, schedules, devices, and datasource templates with shared semantic components.
- [x] 4.2 Migrate logs, notifications, analytics, theme, account, and remaining admin forms/tables while preserving every action, input name, value, and server error path.
- [x] 4.3 Add consistent page headers, breadcrumbs/section context where useful, empty/loading/error states, and responsive table/form behavior.
- [x] 4.4 Remove Bootstrap CDN dependencies and obsolete page-local CSS/JS after all templates consume compiled assets.

## 5. Verification and rollout

- [x] 5.1 Update existing Playwright selectors to stable semantic/data hooks and add desktop/mobile shell coverage.
- [x] 5.2 Add Playwright coverage for live-feed status/control transitions, keyboard focus, icon/manifest loading, reduced motion, and E-Ink presentation.
- [x] 5.3 Add an asset-build validation that checks generated bundles/icons and succeeds without external CDN access.
- [x] 5.4 Run Go tests, frontend build, Playwright tests, and `task pre-push`; fix regressions and document the local asset workflow.
