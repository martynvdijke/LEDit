# Change Proposal: add-device-playlists

## Why

Every LEDit device sees the same content: `HandleDeviceWS` loads the single global `GeneralSettings`, builds one shared source list via `loadSources`, and cycles it — regardless of which device connected. Devices differ in resolution, refresh interval, and token, but never in *what* they show. A multi-room setup (kitchen: weather + calendar + transit; office: GitHub + crypto; workshop: system stats) is impossible today, and it is the natural next step once the wall lives in more than one room.

## What Changes

- Add a **Playlist** entity: `name`, `enabled`, and an `items` JSON column holding ordered references in the established `{source_type, source_id}` shape already used by matrix layout bindings (e.g. `[{"source_type":"weather","source_id":3},{"source_type":"builtin","source_id":"analog-clock"}]`).
- **DeviceSettings gains content selection**: a `content_mode` field (`global` | `playlist`, default `global`) plus an optional `playlist_id`. `global` preserves today's behavior byte-for-byte; `playlist` cycles only the referenced sources, in list order.
- **Refactor `loadSources`** into reusable per-type builders so a source list can be composed either from all configured sources (current path) or resolved from a playlist's `(type, id)` references. Cache keys, health recording, and the last-known-good cache stay keyed per source id — unchanged.
- **Admin UI**: full playlist CRUD page (name, enabled, ordered item builder listing available sources grouped by type) and a "Content" section on the device form (mode toggle + playlist picker).
- The web preview feed (`/ws/feed`) always uses the global list; playlists affect device feeds only.

## Capabilities

### New Capabilities
- `device-playlists`: Named, ordered collections of datasource references that a device can cycle instead of the global source list — storage format, reference resolution, device binding, feed behavior, and admin management.

### Modified Capabilities
<!-- openspec/specs/ has no archived capabilities yet; no requirement deltas. -->

## Impact

- **New code**: `ent/schema/playlist.go` (+ generated ent code), `handlers/playlists.go` (CRUD + reference resolution).
- **Modified code**: `ent/schema/device_settings.go` (`content_mode`, `playlist_id` — additive migration), `handlers/websocket.go` (`loadSources` split into builders + playlist-aware composition in `HandleDeviceWS`), `handlers/server.go` (routes), `handlers/handlers.go` (settings eager-load gains `WithPlaylists()` where needed), admin templates for playlist CRUD and the device form.
- **API surface**: New admin routes `/admin/playlists...` (session auth, standard mutation rules). Device WebSocket protocol unchanged.
- **Risk**: dangling references (a playlist item pointing at a deleted source) — resolution skips missing entries and logs; empty/invalid playlists fall back to the global list rather than a blank wall.
