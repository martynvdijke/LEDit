## Why

Theming is hardcoded and nearly invisible: three baked Go presets (`render/themes/*.go`) plus a duplicated default theme (`datasource.DefaultTheme()` repeats the "cyber" values from `render/themes/default.go`), and the only user-editable theme is an opaque, unvalidated JSON string in `GeneralSettings.theme` edited through a bare three-`<input type=color>` form with no live preview. Users cannot see how a theme looks on real data before saving, cannot keep more than one custom theme, and cannot apply a theme to a single datasource. Phase 8 of PLANS.md.

## What Changes

- Introduce a named, validated theme model with a small token set (background, accent, text, title, font size) shared by editor, storage, render, and preview.
- Replace the duplicated/baked theme sources with that single model; seed the existing built-in presets (`cyber`, `f1`, `untappd`) as built-in themes.
- Add a theme editor page with color pickers and token controls, plus a debounced **live preview** that renders a real datasource (or matrix layout) through the unsaved theme, matching the live-feed program path.
- Support saving multiple named custom themes: create, duplicate, rename, delete, and set one as the global default.
- Allow a per-datasource theme override, resolved with precedence **datasource override → global default → built-in default** by both the feed and previews.
- Import any existing `GeneralSettings.theme` JSON as a custom theme on first run so current installs keep their colors (no data loss; the old editor is superseded).

## Capabilities

### New Capabilities
- `theme-designer`: named theme storage and lifecycle (create/duplicate/edit/delete/default), the color-picker editor with live preview, and effective-theme resolution for the feed, previews, and per-datasource overrides.

### Modified Capabilities
- `admin-live-preview`: adds a theme-editor live preview requirement — an unsaved theme plus a selected source renders through the same preview endpoint family, inheriting the existing authentication and feed-isolation requirements.

## Impact

- **Render**: `render/theme.go` (extended `Theme`), `render/themes/*`, `render/render.go`, `render/panel.go` consumers.
- **Datasource/feed**: `datasource/datasource.go` (`DefaultTheme`, `ThemedRenderer`), effective-theme resolution where slides are rendered.
- **Storage**: new Ent schema for themes, `ent/schema/generalsettings.go` default-theme reference, one-time import of the legacy JSON blob.
- **Handlers/UI**: `handlers/handlers.go` (theme editor/save), new theme CRUD + preview handlers in `handlers/preview.go`, routes in `handlers/server.go`, `web/templates/admin/theme_editor.html` and a theme list page, `web/frontend/app.ts`/`layout_editor.ts`.
- **Tests**: Go handler + render unit tests, Playwright spec for the editor/live preview, and coverage for override precedence.
- **Out of scope**: frontend/CSS app chrome tokens (`control-room-visual-system`) and render-pipeline internals beyond color/font changes.
