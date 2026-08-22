## Context

LEDit is a Go application whose pages are rendered from templates in `web/templates`. The shared shell and live feed currently load Bootstrap from a CDN, mix page-specific CSS and JavaScript into templates, and use a mostly empty light canvas. The application already has PWA metadata and local fonts, plus behavior that must remain intact: Go form actions, WebSocket feed messages, fullscreen mode, and E-Ink mode.

The redesign is intentionally not a backend or SPA rewrite. It introduces a conventional, reproducible frontend asset pipeline while keeping server-rendered HTML as the routing and data boundary.

## Goals / Non-Goals

**Goals:**

- Create a coherent CRT Control Room design system for every admin page and the live feed.
- Make the UI responsive from phone to desktop, with a compact navigation pattern on narrow screens.
- Move shared styles and behavior out of templates into maintainable source files and compiled static assets.
- Preserve route names, form names/values, template data contracts, WebSocket protocol, E-Ink behavior, and keyboard accessibility.
- Produce a recognizable LEDit mark and all browser/PWA icon sizes from one source asset.
- Keep the design legible and restrained: retro cues support information hierarchy rather than making the UI noisy.

**Non-Goals:**

- Replacing Go templates with React, Vue, or a client-side router.
- Changing datasource behavior, persistence, authentication semantics, APIs, or WebSocket payloads.
- Redesigning the LED matrix rendering pipeline or display output themes.
- Adding new dashboard metrics or admin features unrelated to presentation.

## Decisions

### 1. Use Vite as a build tool, not as an application framework

Add a small TypeScript/CSS frontend source tree (for example `web/frontend/`) and use Vite to compile named entrypoints into the existing static directory. Templates remain responsible for server data and page structure; compiled assets provide shared tokens, components, navigation behavior, feed controls, and progressive enhancement.

Vite is preferred over CDN CSS, ad-hoc scripts, or a full SPA because it provides lockfile-backed dependencies, bundling, source maps for development, and a standard npm workflow without moving routing and form handling into the browser.

### 2. Establish a token-first CSS system

Define colors, spacing, type scale, radii, borders, shadows, breakpoints, focus styles, and motion preferences as CSS custom properties. Use semantic component classes (`app-shell`, `nav-section`, `panel`, `status-chip`, `form-grid`, `feed-stage`, `button`) rather than Bootstrap utility markup. Use the existing Pixelify Sans for display accents only and a readable system/sans face plus monospace telemetry for body/data text; avoid forcing pixel typography onto long form labels.

The CRT treatment uses subtle scanlines/grid/noise through CSS gradients and low-opacity overlays. It MUST be disabled or reduced under `prefers-reduced-motion` and simplified in E-Ink mode.

### 3. Keep templates semantic and behavior-compatible

Refactor templates to use landmarks, headings, button/link semantics, labels, stable `data-*` hooks, and reusable partials. Do not use CSS selectors that depend on Bootstrap class names. Existing URLs and form controls remain available so the migration can be incremental and rollback-friendly.

### 4. Make the live feed a first-class control surface

The feed page gets a responsive two-zone composition: telemetry/header and action rail around a bounded media stage. Connection state, current source, next item, clock, marquee, media, pause/skip/fullscreen, E-Ink, and refresh controls remain available, but their grouping and visual priority become explicit. The existing WebSocket controller is moved to a compiled module with the same message handling and reconnect policy.

### 5. Generate branding assets from one SVG source

Create a simple LED matrix-inspired LEDit mark as an SVG source. Add a deterministic npm generation command to rasterize the source into favicon and PWA sizes, and wire those files into `base.html`, `index.html`, the manifest, and Apple touch metadata. The mark must remain recognizable at 16px and work on both dark and light backgrounds.

### 6. Validate through browser and build checks

Add Playwright coverage for the shared shell at desktop/mobile widths, live-feed control visibility and state transitions, icon/manifest loading, keyboard focus, and reduced-motion/E-Ink classes. Add a build check that fails if frontend compilation or icon generation is not reproducible. Preserve existing Go tests and run the repository pre-push task before delivery.

## Risks / Trade-offs

- **[Risk] Template migration breaks an existing form or selector.** → Keep route/action/name contracts unchanged, migrate page groups incrementally, and add selector-stable Playwright assertions before removing Bootstrap markup.
- **[Risk] Vite output is not available in a minimal Docker/runtime image.** → Build assets in the image build stage and commit/use only generated static output at runtime; document the exact task dependency.
- **[Risk] Retro effects reduce readability or performance.** → Keep effects low contrast, avoid large animated layers, honor reduced motion, and test at mobile widths.
- **[Risk] Generated icons drift from the source mark.** → Make generation a single script used by CI and verify expected output files exist.
- **[Risk] E-Ink mode inherits dark UI styling.** → Add explicit E-Ink overrides that remove gradients, animation, shadows, and color-only status meaning.

## Migration Plan

1. Add the frontend build and icon-generation toolchain without changing rendered pages.
2. Introduce tokens/components and migrate the shared shell/sidebar.
3. Migrate the live feed and move its controller into the compiled entrypoint.
4. Migrate admin template groups, preserving server contracts and adding responsive/keyboard states.
5. Remove Bootstrap CDN dependencies and obsolete inline styles/scripts after coverage passes.
6. Build generated assets in CI/Docker, run Go tests, Playwright tests, and `task pre-push`.

Rollback is a git revert of the template/assets change; because routes, handlers, forms, and WebSocket payloads remain unchanged, reverting presentation assets does not require data migration.

## Open Questions

- Confirm the preferred icon rasterization dependency available in the project’s build image (a Node package or system ImageMagick).
- Confirm whether the existing deployment serves generated static assets from the repository checkout or requires embedding them into the Go binary.
