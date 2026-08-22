## Context

LEDit renders datasources as full-canvas PNGs through a single interface — `datasource.Datasource.GetPNG(width, height int) (*render.RenderedImage, error)` — and streams them over WebSocket from the feed loop in `handlers/websocket.go`, which cycles `sources` sequentially with pause/resume/skip. All datasources (weather, crypto, F1, stocks, calendar, RSS, Sonarr/Radarr, Home Assistant, Untappd) are registered in `handlers/datasource_registry.go` as uniform token/URL CRUD entries persisted via ent (SQLite). Rendering uses `render.RenderDict` with themed colors and the PixelifySans pixel font.

"Matrix rendering" here means a **grid-of-panels dashboard** on the LED matrix display (each cell showing one datasource), not a digital-rain effect. Today there is no way to see multiple sources simultaneously, and no way to consume an arbitrary public/selfhosted API without writing Go code.

Constraints: the WebSocket feed protocol and `GetPNG` contract must not change; rendering must remain self-contained Go (no SPA); the admin surface is server-rendered Go templates.

## Goals / Non-Goals

**Goals:**

- Render multiple datasources simultaneously in a configurable rows × columns grid composited into a single image.
- Add Google Calendar (private iCal URL), a multi-feed news headlines source, and a user-configurable generic JSON API source.
- Keep every new feature working through the existing `Datasource` interface so the feed loop, WebSocket protocol, pause/skip, and device streaming remain unchanged.
- Expose grid layout and the new datasource types in the admin UI with CRUD matching the existing datasource pattern.
- Scale gracefully on small panels (font/rows adapted per panel) and avoid hammering external APIs when many panels render together.

**Non-Goals:**

- No OAuth/Google Calendar API v3 integration in this change (private iCal URL only).
- No client-side SPA rendering of the matrix; composition happens server-side in Go.
- No new rendering primitives (no charts/sparklines) — panels reuse existing `RenderDict`-style output.
- No changes to the WebSocket message format, device protocol, or existing datasource behavior.
- No generic datasource "plugin" runtime — extensibility comes from configuring the generic API source, not uploading code.

## Decisions

### 1. Matrix layout is a composite `Datasource` (`MatrixDS`)

A `MatrixDS` implements `GetPNG(width, height)` by splitting the canvas into `rows × cols` cells, calling each bound datasource's `GetPNG(panelW, panelH)`, and compositing the results onto an RGBA canvas with `image/draw` using configurable gaps and a background color. The feed sees one source named e.g. `matrix:live` and streams it exactly like today — zero changes to the feed loop or WebSocket protocol.

Alternatives considered: a special-cased render path in the feed loop (rejected: forks the protocol and complicates pause/skip/next), and multiple concurrent WebSocket frames per panel (rejected: breaks the device client's one-image-per-message model).

### 2. Grid config persisted as an ent entity with JSON panel bindings

New `MatrixLayout` schema entity: `name`, `rows`, `cols`, `gap`, `background` color, `enabled`, plus a `bindings` JSON field (`[{row, col, source_type, source_id}]`). Polymorphic binding via ent edges (an edge per datasource type) was considered and rejected — it would add nine relationship types for one feature. JSON bindings keep the schema small, are validated on save, and are resolved to concrete `Datasource` instances at render time by a resolver that builds each datasource from its DB row.

### 3. New datasource entities follow the existing registry pattern, with one extension

`GoogleCalendar` (URL, Name), `NewsFeed` (URL, Name), and `GenericAPI` (URL, Token) are added to `datasource_registry.go` exactly like weather/F1/crypto: token/URL CRUD wired into `GeneralSettings` edges, forms mirroring existing templates.

`GenericAPI` additionally needs structured config (optional headers, field mappings, row cap). The registry's uniform `(token, url)` signature is retained and extended with an optional `ConfigJSON` text column plus optional `GetConfig/SetConfig` helpers on the dsEntry; the generic-API form is the only consumer. This keeps the registry shape uniform while allowing one richer type.

### 4. Google Calendar reuses the existing iCal parser

`GoogleCalendarDS` is a thin specialization of `CalendarDS`: same `parseICal`, same `RenderDict` output, default title "GOOGLE CAL", and the private iCal URL format (`https://calendar.google.com/calendar/ical/<calendarId>/private-<hash>/basic.ics`) documented in the form. No token required (private URLs carry their own auth). A dedicated type (rather than a renamed calendar) exists so it is discoverable and separately manageable in the admin UI.

### 5. News aggregates multiple feeds with dedupe

`NewsDS` takes comma-separated feed URLs (v1), fetches each via the shared `apiGet`, parses with the existing `parseRSS`, interleaves the newest headlines, dedupes by title, caps at 4 rows (matching `RenderDict`'s comfortable row count at 32px–64px panels), labels rows with a short source tag, and falls back to a static placeholder if every feed fails.

### 6. Generic API uses a minimal dot-path extractor — no JSONPath dependency

Mapping config is a JSON list of `{label, path}` rows plus optional `title`; `path` uses dot notation (`data.btc.usd`, `items.0.name`) resolved by a ~30-line recursive getter. A full JSONPath library was considered and rejected: LEDit's render surface only needs scalar extraction, and avoiding the dependency keeps the build lean and the surface auditable. Optional headers support `X-API-Key`/`Bearer`/arbitrary headers for selfhosted APIs.

### 7. TTL cache inside `MatrixDS` protects upstream APIs

Existing datasources fetch on every `GetPNG`. A matrix cycling 6 panels would multiply upstream calls per cycle. `MatrixDS` wraps each bound source in a small in-memory TTL cache (default 60s, configurable per binding later) keyed by source identity, so panel refreshes within a cycle reuse the last successful render. Datasources outside the matrix are unaffected.

### 8. Panel legibility via font scaling and row caps

Panels render through their own datasource at `panelW × panelH`. For small cells the existing fixed 24px font would overflow; the matrix wrapper passes a per-panel theme whose `FontSize` is scaled to panel height (roughly `max(8, panelH/4)`) and datasources that accept a theme are rendered with it. Fallback/placeholder renders already accept width/height. If a panel is smaller than a minimum usable size (e.g. < 16px on a side), the cell renders the source title only.

### 9. Server-side on-demand preview endpoint

A single admin route, e.g. `GET /admin/preview?type=<datasource|matrix>&id=<id>&w=<px>&h=<px>`, builds the requested source from its DB row (the same resolver the feed uses), calls `GetPNG(w, h)`, and returns the PNG bytes with `Content-Type: image/png` and a no-store cache header. Because the resolver and renderer are identical to the feed path, the preview always shows exactly what the display will show.

Alternatives considered: rendering previews client-side (rejected: LEDit renders server-side in Go and the pixel font/themes are not available in the browser) and a WebSocket preview per source (rejected: an HTTP GET per preview is simpler for forms and editor polling; the existing 400×400 browser feed on the feed page remains unchanged).

### 10. Matrix editor with live composite preview

The matrix editor is a single admin page: numeric inputs for rows/cols/gap, a color picker for background, and a per-cell source selector populated with all configured datasources (grouped by type) plus an "unbound" option. The composite preview is an `<img>` whose `src` points at the preview endpoint with query parameters mirroring the current form state (`rows`, `cols`, `gap`, `bg`, and cell bindings). A small compiled JS module debounces edits (~300 ms) and rewrites the image URL, so the preview updates live as the user edits without a page reload. The preview URL is a POST-only render path or a dedicated GET with a signed/nonce query parameter to avoid CSRF-style abuse of arbitrary datasource rendering.

### 11. PNG template export

A `?template=1` variant of the matrix preview renders a deterministic template image at the requested resolution: layout background, cell grid lines, per-cell coordinates (e.g. `R1C1`), and the bound source's short name in each bound cell; unbound cells show `EMPTY`. This gives the user a reference PNG of "where each panel renders" that they can save while designing a layout or a physical matrix wall.

## Risks / Trade-offs

- **[Risk] Small panels become illegible.** → Scale font size by panel height, cap rows per panel, and render title-only below a minimum cell size; validate in Playwright at 64×64 and 32×32 device resolutions.
- **[Risk] Matrix rendering multiplies upstream API load.** → TTL cache in `MatrixDS` (Decision 7); document rate-limit guidance for weather/coin/stock APIs in the admin help text.
- **[Risk] Live previews trigger real upstream fetches on every keystroke.** → Debounce editor preview updates, keep preview sizes small (default 400×400 like the existing browser feed, or the device resolution), and reuse the per-source TTL cache where available so rapid edits do not hammer external APIs.
- **[Risk] Unauthenticated preview route could render arbitrary sources or be abused as a proxy.** → Preview route requires admin session auth (same middleware as other admin routes) and only renders configured sources by ID; URL fetches stay inside the existing datasource config, never from query parameters.
- **[Risk] JSON bindings allow invalid references (deleted datasource, out-of-range cell).** → Validate on save and at render time; unresolved bindings render as an empty themed cell rather than failing the whole matrix.
- **[Risk] Generic API responses vary wildly; dot-path may not match.** → Clear form fields with a live "test" preview in the admin form; on extraction failure the panel shows the fallback render and a logged warning.
- **[Risk] ent schema additions require codegen and migration.** → Follow the existing pattern used by weather/F1/crypto schema additions: add schema files, run ent codegen, apply additive migration; rollback is a git revert with no destructive migration.
- **[Risk] Google private iCal URLs expire/change.** → Form documents how to obtain the URL; refresh errors fall back to the existing calendar placeholder behavior.

## Migration Plan

1. Add ent schema files (`MatrixLayout`, `GoogleCalendar`, `NewsFeed`, `GenericAPI`), run ent codegen, apply additive migration.
2. Implement `GenericAPIDS`, `GoogleCalendarDS`, `NewsDS` with tests; register in `datasource_registry.go`.
3. Implement `MatrixDS` + panel compositing helpers in `render/` with unit tests (cell layout, gaps, font scaling, TTL cache, unresolved bindings).
4. Add admin templates/routes for the three datasource types and the matrix layout editor (grid config + bindings) following the existing datasource CRUD pattern.
5. Add the on-demand preview endpoint, live previews on datasource forms, the matrix editor with live composite preview, and the PNG template export.
6. Wire matrix layouts into the sources list so `matrix:<name>` cycles in the feed; verify WebSocket streaming at 64×64 and 32×32.
7. Add Playwright coverage for the new forms, matrix editor, live previews, and template export; run Go tests, frontend build, and `task pre-push`.

Rollback: revert the change commit. All additions are new tables and new registry entries; existing rows, routes, and the WebSocket protocol are untouched, so no data migration is required to revert.

## Open Questions

- Should matrix layouts support a "focus mode" (tap/command to enlarge one panel full-screen)? Deferred; revisit after v1 usage.
- Should the TTL cache duration be per-binding configurable in v1, or fixed at 60s? Fixed 60s in v1 unless usage data suggests otherwise.
- Does the news source need Atom parsing (some feeds are Atom-only), or is RSS sufficient for v1? RSS only unless a concrete Atom-only feed is reported.
