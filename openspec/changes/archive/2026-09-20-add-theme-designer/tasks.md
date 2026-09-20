# Tasks

## 1. Theme model and storage

- [x] 1.1 Add `ent/schema/theme.go`: name (unique, required), `bg_color`, `accent_color`, `text_color` (hex strings), `title`, `font_size` (float), `built_in` and `is_default` booleans; run Ent codegen
- [x] 1.2 Add `ent/schema/themeassignment.go`: FK to `Theme`, `target_type`, `target_id`, unique index on (`target_type`,`target_id`); run Ent codegen
- [x] 1.3 Add theme validation (non-empty unique name, `#rrggbb` hex, font-size bounds) with field-level errors, reusing the existing hex check pattern in `datasource/matrix.go`
- [x] 1.4 Add a starter that seeds `cyber`, `f1`, `untappd` as built-in themes when no themes exist, marking one as the global default

## 2. Single theme source and effective resolution

- [x] 2.1 Make `datasource.DefaultTheme()` (`datasource/datasource.go:54`) delegate to `render/themes.DefaultTheme` and drop the duplicated values
- [x] 2.2 Add `ResolveTheme(targetType, targetID)` resolving datasource override → global default → built-in default. Resolution is intentionally uncached (a local SQLite read per render beats an invalidation bug); add caching only if it measurably matters
- [x] 2.3 Add a shared `datasource.RenderThemed(ds, w, h, theme)` helper that uses `ThemedRenderer.GetPNGThemed` when available and otherwise renders the source normally (covers the sources that support themed rendering today)
- [x] 2.4 Route feed slides (`handlers/websocket.go` render loop) and preview handlers (`handlers/preview.go`) through the shared helper so both resolve themes identically

## 3. Theme CRUD, editor handlers, and routes

- [x] 3.1 Replace `AdminThemeEditor`/`AdminThemeSave` (`handlers/handlers.go:1659`,`:1681`) with theme list, create/edit, duplicate, rename, delete, and set-default handlers; redirect the legacy `/admin/theme` POST to the new editor
- [x] 3.2 Refuse edit/delete of built-in themes and refuse deleting the current default without promoting another theme first
- [x] 3.3 Clear `ThemeAssignment` rows when a custom theme is deleted
- [x] 3.4 Add a set/clear assignment handler (`POST /admin/theme/assign`) and surface assignments centrally on the theme list page (all assignable targets) instead of adding a control to every datasource form
- [x] 3.5 Extend the preview endpoints (`GET /admin/preview`, `POST /admin/preview/datasource`, `/preview/matrix`) to accept optional theme tokens (`theme_bg/accent/text/title/font_size`) or `theme_id` for unsaved-theme preview
- [x] 3.6 Register the new theme routes in `handlers/server.go` behind the existing admin session middleware

## 4. Admin UI

- [x] 4.1 Rebuild `web/templates/admin/theme_editor.html` with color pickers + title/font-size controls, a source picker, a live preview `<img>`, and a save/duplicate action, following the `datasource_form.html` preview pattern
- [x] 4.2 Add a theme list page in `web/templates/admin/` showing name, default marker, and create/duplicate/delete/set-default actions with empty and error states
- [x] 4.3 Wire debounced live preview in `web/frontend/theme_editor.ts` (new Vite entry) to the preview endpoints with the unsaved tokens, and rebuild static assets
- [x] 4.4 Surface the per-datasource theme selector/clear control in the theme list assignments panel and show built-in themes as read-only

## 5. Legacy import

- [x] 5.1 On startup, when `GeneralSettings.theme` is non-empty and no custom themes exist, create an `Imported` theme and mark it default
- [x] 5.2 Skip import when themes already exist or the legacy value is empty/unparseable, and leave the legacy field intact for rollback

## 6. Tests

- [x] 6.1 Go unit tests for theme validation, unique-name conflict, and default immutability
- [x] 6.2 Go test asserting `datasource.DefaultTheme()` equals `render/themes.DefaultTheme`
- [x] 6.3 Go handler tests for theme CRUD, set-default promotion, built-in refusal, and preview rendering with unsaved tokens
- [x] 6.4 Go test for effective-theme precedence (override → global → built-in) on both feed and preview paths
- [x] 6.5 Go test for legacy import: creates `Imported` once, is idempotent, and ignores empty values
- [x] 6.6 Playwright spec for the theme editor: edit a color, confirm the preview image updates, save, and confirm the theme appears in the list

## 7. Verification

- [x] 7.1 Run `task pre-push` (gofmt, tests, build) and fix any failures
