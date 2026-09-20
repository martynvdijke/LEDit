## Context

Colors in LEDit are hardcoded and duplicated. `render.Theme` (`render/theme.go:3`) is a 5-field struct (background, accent, text, title, font size). Three presets are baked as Go vars (`render/themes/*.go`), and `datasource.DefaultTheme()` (`datasource/datasource.go:54`) repeats the `cyber` values, so the same colors live in two places. The only editable theme is an opaque JSON string in `GeneralSettings.theme` (`ent/schema/generalsettings.go:21`), rendered by a bare 3-color form (`web/templates/admin/theme_editor.html`) with no preview and no validation. The admin UI is Go `html/template` + vanilla TS (`web/frontend/app.ts`), not an SPA.

Per-cell theming already exists for Matrix and Compositor (`datasource/matrix.go:21` `CellTheme`, `applyCellTheme` at `:122`), and previews already render unsaved data through `POST /admin/preview/datasource|matrix|composition` (`handlers/preview.go:135`). Renderers that accept a theme implement `ThemedRenderer.GetPNGThemed` (`datasource/datasource.go:38`).

## Goals / Non-Goals

**Goals:**
- One authoritative theme model, validated, stored, and reused by editor, feed, and previews.
- Multiple named custom themes with a single global default and a per-datasource override.
- Live preview of unsaved theme values against real datasource data.
- Zero color loss for installs that already customised the legacy JSON theme.

**Non-Goals:**
- Admin-app CSS tokens (`control-room-visual-system`) and the CRT chrome — untouched.
- Gradients, custom fonts/families, per-element styling, animation. The token set stays the existing five fields.
- Reworking Matrix/Compositor per-cell `CellTheme` into named references (possible follow-up; `CellTheme` keeps working as an inline override).
- Per-device or per-playlist themes, theme export/import files.
- Permission changes: existing admin session auth applies.

## Decisions

**1. Persist themes as an Ent schema, not a JSON map.**
Add `Theme` (name unique, tokens as explicit columns: `bg_color`, `accent_color`, `text_color`, `title`, `font_size`, plus `built_in`, `is_default`). Alternative — keep a JSON map in `GeneralSettings.theme` — rejected: no unique names, no validation, no FK target for assignments, and no list query. Ent is the existing idiom for every other persisted entity.

**2. Assignments in one join table, not a column per datasource.**
Add `ThemeAssignment(theme_id FK, target_type, target_id, unique(target_type,target_id))`. Alternative — add `theme_id` to each of the ~30 datasource schemas — rejected as 30 migrations for one feature. `target_type="global"` is not used; the global default lives on `Theme.is_default`. Resolution: **datasource override → global default → built-in `cyber`**.

**3. One render entry point for an effective theme, applied where sources support it.**
Only three sources implement `ThemedRenderer` today (`ClockDS` `datasource/clock.go:16`, `SystemStatsDS` `systemstats.go:42`, `CompositorDS` `compositor.go:121`); the other ~35 render through `render.RenderDict(..., DefaultTheme(), ...)` and ignore a passed theme. The change therefore adds a `datasource.RenderThemed(ds, w, h, theme)` helper: sources implementing `ThemedRenderer` are rendered with the theme, everything else renders normally. `MatrixDS` gains a `BaseTheme` so matrix cells inherit the matrix's effective theme. Feed and previews call the same helper so previews cannot drift from the feed. Extending themed rendering to the remaining datasources is an explicit follow-up, not part of this change. Resolution is done per render with no cache — a local SQLite lookup per slide is cheaper than an invalidation bug; add caching only if it measurably matters. The duplicated `datasource.DefaultTheme()` values are reduced to a reference to `render/themes.DefaultTheme`; a unit test asserts they agree.

**4. Extend existing preview endpoints instead of adding a theme-preview endpoint.**
`POST /admin/preview/datasource` and `/preview/matrix` accept optional theme tokens (or `theme_id`). The theme editor posts unsaved token values through the same ephemeral, DB-free path as datasource forms. Alternative — a dedicated `POST /admin/preview/theme` — rejected: duplicates the ephemeral-DS logic. The existing `admin-live-preview` requirements for auth and feed isolation already cover these endpoints.

**5. Seed built-in presets; keep them immutable.**
`cyber`, `f1`, `untappd` are seeded on first boot as `built_in` rows. They cannot be edited or deleted; "editing" a built-in offers duplicate-then-edit. This preserves current default behaviour while giving users a safe starting point.

**6. Legacy import is one-shot and non-destructive.**
On startup, if `GeneralSettings.theme` is non-empty and no custom theme exists, create a theme named `Imported` from it and make it the default. The legacy field is left in place (not dropped) so a rollback loses nothing.

## Risks / Trade-offs

- [Preview drifts from the feed] → both go through one resolver/render helper; override-precedence test covers feed and preview.
- [Two sources of default colors drift again] → `datasource.DefaultTheme()` delegates to `render/themes.DefaultTheme`; test asserts equality.
- [Deleting a theme still referenced by an assignment or set as default] → refuse deletion of the default theme; reassign or clear assignments inside the same transaction; built-ins are undeletable.
- [Startup import re-runs every boot and creates duplicates] → guarded on "no themes exist"; safe to re-run.
- [Assignment cache staleness] → in-memory cache invalidated on every theme/assignment write; single-process app, no cross-node concern.
- [Font size / color typos break rendering] → validate hex (`#rrggbb`) and font size bounds at the model boundary, returning field-level form errors, matching the existing 4-hashtag scenario style.
- [`CellTheme` vs named themes confusion] → documented: `CellTheme` remains an inline per-cell override and is out of scope.

## Migration Plan

1. Add `Theme` and `ThemeAssignment` schemas and generate Ent code.
2. On startup: seed built-ins if no themes exist; import legacy `GeneralSettings.theme` into `Imported` and mark it default if the legacy value is non-empty.
3. Ship editor UI/preview; old `/admin/theme` POST redirects to the new editor.
4. Rollback: drop the two tables; the legacy `GeneralSettings.theme` field still holds the previous values, so behaviour reverts to the existing renderer defaults.

## Open Questions

- None blocking. Follow-up (not this change): should Matrix/Compositor per-cell themes upgrade from inline `CellTheme` to named-theme references?
