## Why

LEDit cycles one datasource full-screen at a time, so a user can never see weather, calendar, stocks, news, F1, and crypto at once. There is also no way to plug an arbitrary public or selfhosted API (GitHub stats, Pi-hole, Plex, ISS position, etc.) without writing Go code. A matrix/grid rendering mode turns the display into a control-room wall of live panels, and a configurable API datasource opens LEDit to the whole selfhosted/geek ecosystem.

## What Changes

- Add a **matrix grid rendering** mode that composites multiple datasources into a single image divided into a configurable grid (rows × columns), with per-panel datasource assignment, panel gaps/borders, and themed styling.
- Add a **Google Calendar datasource** (private iCal URL) reusing the existing iCal parser so Google Calendar events can be displayed as a panel.
- Add a **news datasource** that aggregates headlines from configurable RSS/Atom feeds into one panel.
- Add a **generic API datasource** ("custom public API") where users configure a URL, optional token/headers, and JSONPath-style field mappings to render any JSON API — the extensibility hook for arbitrary selfhosted/geek APIs.
- Extend the admin UI to configure the matrix grid layout (rows/cols, panel bindings, styling) and manage the new datasource types (Google Calendar, News, Custom API).
- Add a **live preview** in the admin UI that renders any datasource — including the new ones and any matrix layout — to a PNG image on demand with real fetched data, so configuration changes are visible immediately.
- Add a **matrix editor** with a live composite preview that updates as grid settings and panel bindings change, and a **PNG template export** that downloads a template image showing the grid structure and where each bound source renders.
- Matrix layout becomes selectable as a datasource/source in the feed, so it cycles and streams over WebSocket like any other source.

## Capabilities

### New Capabilities

- `matrix-grid-layout`: Defines the grid compositing renderer — grid dimensions, panel bindings to datasources, panel styling, and how the feed serves a matrix view as a single source.
- `google-calendar-datasource`: Defines the Google Calendar datasource (private iCal feed) and its event rendering contract.
- `news-datasource`: Defines the news headlines datasource aggregating multiple RSS/Atom feeds.
- `generic-api-datasource`: Defines the user-configurable JSON API datasource with URL, auth, and field-mapping configuration.
- `admin-live-preview`: Defines the admin live-preview surface — on-demand PNG preview of any datasource, the matrix editor with live composite preview, and PNG template export showing grid structure and panel placement.

### Modified Capabilities

<!-- No existing OpenSpec capability specifications are present; this change introduces all capabilities above. -->

## Impact

- `datasource/`: new `matrix.go`, `googlecalendar.go`, `news.go`, `genericapi.go` implementing the `Datasource` interface (`GetPNG(width, height)`); existing datasources must render correctly at reduced panel sizes.
- `handlers/websocket.go` feed loop and `handlers/datasource_registry.go`: matrix treated as a composable source; new datasource types registered for CRUD.
- `handlers/`: new admin preview routes that render any datasource or matrix layout to PNG on demand for the live preview, editor, and template export.
- `ent/schema/`: new schema entities for matrix layout config and the three new datasource types (persisted in SQLite).
- `render/`: panel compositing helpers (tile placement, gaps, borders) built on `image/draw`.
- `web/templates` + admin handlers: forms for matrix grid config and new datasource management; datasource list/dashboard updated.
- Tests: Go unit tests for composite rendering and new datasource parsers; Playwright coverage for admin forms; `task pre-push` must pass.
