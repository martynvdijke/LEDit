## 1. Schema and persistence

- [x] 1.1 Add ent schema files `MatrixLayout` (name, rows, cols, gap, background, enabled, bindings JSON), `GoogleCalendar` (name, url), `NewsFeed` (name, url), and `GenericAPI` (name, url, token, config JSON); run ent codegen and apply an additive migration.
- [x] 1.2 Register CRUD for `googlecalendar`, `newsfeed`, and `genericapi` in `handlers/datasource_registry.go` following the existing weather/F1 pattern, extending the dsEntry shape with optional config helpers for `genericapi`; wire edges into `GeneralSettings`.

## 2. New datasources

- [x] 2.1 Implement `GoogleCalendarDS` in `datasource/googlecalendar.go`: fetch via shared `apiGet`, parse with existing `parseICal`, render title + up to 4 truncated events via `RenderDict`, fallback placeholder on fetch/parse failure, with unit tests.
- [x] 2.2 Implement `NewsDS` in `datasource/news.go`: fetch comma-separated feeds, aggregate with `parseRSS`, dedupe by title, render title + up to 4 headlines with source tags, fallback placeholder when all feeds fail or are empty, with unit tests.
- [x] 2.3 Implement `GenericAPIDS` in `datasource/genericapi.go`: authenticated JSON fetch (token → `X-API-Key` header, extra headers), dot-path scalar extraction (array indices supported), render extracted label/value rows, fallback placeholder on failure and placeholder value for unresolved paths, with unit tests.

## 3. Matrix compositing renderer

- [x] 3.1 Add panel compositing helpers in `render/` (cell size computation from rows/cols/gap, placement via `image/draw`, background fill) with unit tests for layout math at 64×64 and 32×32.
- [x] 3.2 Implement `MatrixDS` in `datasource/matrix.go`: resolve JSON bindings to concrete datasources, per-panel font scaling by panel height, title-only rendering below minimum cell size, empty themed cell for unbound/unresolved bindings, with unit tests.
- [x] 3.3 Add a 60-second TTL in-memory cache for bound panel renders inside `MatrixDS`, keyed by source identity, with unit tests covering refresh-within-TTL reuse and expiry re-fetch.

## 4. Admin UI

- [x] 4.1 Add admin templates/routes for Google Calendar, News, and Custom API sources following the existing datasource form pattern; update the datasource list/dashboard to show them.
- [x] 4.2 Add the matrix layout editor (name, rows, cols, gap, background, enabled, per-cell datasource bindings with validation for out-of-range cells) and a matrix layouts list, following the shared admin UI components.
- [x] 4.3 Add a test/preview action to the Custom API form that fetches the configured URL and displays extracted rows or an error.

## 5. Feed integration and verification

- [x] 5.1 Resolve enabled matrix layouts into the feed source list so `matrix:<name>` renders and streams as one source over WebSocket; verify pause/skip/next and WebSocket streaming at 64×64 and 32×32 device resolutions.
- [x] 5.2 Add Playwright coverage for the three new datasource forms and the matrix layout editor (create, bind, validation error, delete).
- [x] 5.3 Run Go tests, frontend build, Playwright tests, and `task pre-push`; fix regressions and confirm no changes to the WebSocket message format or existing datasource behavior.

## 6. Live preview, matrix editor, and PNG template

- [x] 6.1 Add the admin-authenticated on-demand preview endpoint (`GET /admin/preview?type=&id=&w=&h=`) that resolves any datasource or matrix layout from its DB row and returns the rendered PNG; verify it uses the same resolver/renderer as the feed and does not touch feed state or display tracking.
- [x] 6.2 Add the template export variant (`?template=1`) rendering layout background, grid lines, per-cell coordinates, bound source names, and `EMPTY` marks for unbound cells, with unit tests for the template renderer.
- [x] 6.3 Add live previews to the new datasource create/edit forms (Google Calendar, News, Custom API) and the existing datasource forms, debounced so keystrokes do not fire a request per character.
- [x] 6.4 Build the matrix editor page: rows/cols/gap/background controls, per-cell datasource selectors (grouped by type, including "unbound"), validation warnings for out-of-range dimensions, and a live composite preview image updated via a debounced JS module.
- [x] 6.5 Add Playwright coverage for the preview endpoint (authenticated + unauthenticated), live preview updating on form edits, the matrix editor's live preview reacting to grid/binding changes, and template export content. Run Go tests, frontend build, Playwright tests, and `task pre-push`.
