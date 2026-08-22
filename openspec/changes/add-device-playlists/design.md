## Context

`HandleDeviceWS` (`handlers/websocket.go`) loads the single global `GeneralSettings`, builds one shared source list via `loadSources`, and cycles it for every device. Devices already differ in resolution, refresh interval, token, and get a fresh per-connection `FeedController` — only the *content list* is shared. The matrix layout feature established both the `{source_type, source_id}` reference shape (`datasource.ParseBindings`) and a reusable resolver: `buildSourceIndex` / `sourceIndex.Resolve` (`handlers/sources.go`), which indexes every configured datasource by `"<endpoint>:<id>"` with display names, and is already used by matrix nesting and the on-demand preview endpoint.

## Goals / Non-Goals

**Goals:**
- A device can cycle a named, ordered subset of datasources instead of the global list.
- Default behavior is byte-for-byte unchanged (`content_mode = "global"`).
- Playlist references resolve through the same index the matrix editor uses, so labels and keys never diverge.

**Non-Goals:**
- Time-of-day windows / programming grid (later; this change is the structural prerequisite).
- Per-device theme or timeout overrides.
- Sharing playlists between devices by reference semantics beyond picking the same playlist id.
- Reordering UX beyond a simple ordered item builder (drag-and-drop polish later).

## Decisions

### D1 — Resolve playlist items via the existing `sourceIndex`, not a new builder family
The proposal's "refactor `loadSources` into reusable per-type builders" collapses to: keep `loadSources` as the global-mode path, and compose playlist lists with `buildSourceIndex(settings, aiCfg).Resolve(type, id)` — the exact machinery matrix bindings and `/admin/preview/datasource` already use. One gap: `systemstats:0` exists in `loadSources` but not in `buildSourceIndex`/`bindingOptions`; add it there so System Stats is selectable in playlists (and the matrix editor benefits too).
- *Why:* Single source of truth for source keys and display names; smallest possible diff; no duplicated per-type construction.
- *Alternative:* Extract per-type builders from `loadSources` and rebuild both paths on them — rejected for now: large mechanical churn across ~20 types with regression risk in the hot feed path, no behavioral gain over D1.

### D2 — Items stored as a JSON column on a single `playlist` entity
Schema: `name`, `enabled`, `items` (JSON text, default `"[]"`). Item shape mirrors matrix bindings minus row/col: `{"source_type": "weather", "source_id": 3}`.
- *Why:* House precedent (matrixlayout.bindings, pixelart frames); playlists are small bounded documents written/read as a unit by the editor; SQLite handles this fine.
- *Alternative:* Normalized `playlist_item` child table — rejected: join plumbing for no query benefit at this scale.

### D3 — Device binding is `content_mode` + plain optional `playlist_id`
`DeviceSettings` gains `content_mode` (`string`, default `"global"`) and `playlist_id` (`int`, optional/nillable). Deliberately *not* an ent edge: deleting a playlist must not cascade or orphan device rows — a dangling id simply falls back to the global list at feed time.
- *Why:* Graceful degradation beats referential enforcement for a display wall; an empty wall is the worst failure mode.
- *Alternative:* `edge.To("playlist")` with `Required()`-less FK — rejected: deletion then needs explicit edge-clearing logic in every delete path for no user-visible benefit over fallback.

### D4 — Playlist mode cycles in authored order; `settings.Random` applies to global mode only
A playlist is an authored sequence (kitchen: weather → calendar → transit). Shuffling it would defeat the point of authoring order. Global mode keeps today's shuffle behavior untouched.
- *Why:* Predictability is the feature; users who want random can approximate it later with a dedicated option if ever needed.

### D5 — Fallback ladder at composition time
Playlist mode resolves as: disabled/missing playlist → global list; enabled playlist with zero resolvable items → global list + warn log; partially resolvable → cycle only the resolvable items (dangling refs skipped and logged once per connection setup, not per slot).
- *Why:* Never show a blank wall; logs make silent skips debuggable without spamming.

### D6 — Cache keys and LKG stay keyed per source
Playlist composition reuses the same `"<type>:<id>"` cache keys (and the same `feedConn.cacheKeyPrefix` rules), so a weather panel in a kitchen playlist shares last-known-good entries with the global rotation at equal resolution. Health recording is likewise per source id and unchanged.
- *Why:* Free cache sharing across devices; zero new cache semantics to reason about.

### D7 — Routes and UI follow the admin CRUD pattern
`admin.GET/POST /admin/playlists/new|:id/edit|:id/delete` (session-auth, standard mutation rules) rendering a `playlists.html` form page; the item builder lists available sources grouped by type using the existing `bindingOptions` data (extended per D1). The device form gains a Content section: mode toggle + playlist dropdown populated with enabled playlists.
- *Why:* Zero novel auth/routing concepts; reviewers diff against existing datasource CRUD pages.

## Risks / Trade-offs

- [Divergent source catalogs between loadSources and buildSourceIndex confuse users] → Task adds a parity check test asserting every loadSources entry has an index counterpart (and vice versa after adding systemstats).
- [Users expect Random to shuffle playlists] → Documented in the UI copy next to the toggle ("Playlists play in saved order"); revisit only on demand.
- [Very long playlists slow connection setup] → Resolution is O(items × map lookup) once per connection; no per-slot cost. No cap enforced beyond sanity (e.g., 64 items) validated at save.

## Migration Plan

Ent codegen adds the `playlist` table + `GeneralSettings` edge, and two additive columns on `device_settings`; auto-migration creates them on startup. Existing rows default to `content_mode = "global"` — inert until a user binds a playlist. Rollback is safe (drop unused columns/table).

## Open Questions

- Should the device status card show the active playlist name (assumed yes, cheap)?
- Per-device shuffle as an explicit per-device flag someday (deferred, YAGNI).
