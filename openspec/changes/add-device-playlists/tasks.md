## 1. Schema and codegen

- [x] 1.1 Add `ent/schema/playlist.go`: `name` (string), `enabled` (bool, default true), `items` (text, default `"[]"`, comment documenting the `{source_type, source_id}` shape); add `edge.To("playlists", Playlist.Type)` on GeneralSettings; run ent codegen
- [x] 1.2 Add `content_mode` (string, default `"global"`) and optional nillable `playlist_id` (int) fields to `ent/schema/device_settings.go`; run ent codegen and verify auto-migration is additive
- [x] 1.3 Add `ParsePlaylistItems(string) []PlaylistItem` + validation helper in a new `datasource/playlist.go` (reuse the parsing style of `ParseBindings`; reject unknown `source_type` values against the known endpoint set, cap items at 64)

## 2. Feed composition

- [x] 2.1 Extend `buildSourceIndex` and `bindingOptions` in `handlers/sources.go` with `systemstats:0` ("System Stats") so built-ins are selectable in playlists and the matrix editor
- [x] 2.2 Add `composeDeviceSources(device *ent.DeviceSettings, settings *ent.GeneralSettings) []sourceWithName` in `handlers/websocket.go`: global mode → `loadSources(settings)` unchanged; playlist mode → load playlist, resolve each item via `buildSourceIndex(...).Resolve`, skip-and-log dangling refs once per setup, apply the fallback ladder (missing/disabled/empty → global list)
- [x] 2.3 Wire `HandleDeviceWS` to use `composeDeviceSources`; leave `HandleWS` (preview) and `HandleDevicePreviewWS` on the global list; confirm playlist mode ignores `settings.Random` while global mode keeps shuffling
- [x] 2.4 Add a catalog-parity test asserting every `cacheKey` produced by `loadSources` exists in `buildSourceIndex` and vice versa

## 3. Routes and admin UI

- [x] 3.1 Add playlist CRUD handlers in new `handlers/playlists.go` (list/new/create/edit/update/delete) validating items via the 1.3 helper; register session-authed admin routes in `handlers/server.go`
- [x] 3.2 Add `web/templates/admin/playlists.html` (list + form with ordered item builder fed by `bindingOptions`-style grouped selectors, up/down remove controls serializing to the hidden `items` input)
- [x] 3.3 Extend the device form template with a Content section (mode toggle + enabled-playlist dropdown) and update the device save handler to persist `content_mode`/`playlist_id`; show the active playlist name on the device status card
- [x] 3.4 Add nav entry for Playlists next to the existing datasource pages

## 4. Tests

- [x] 4.1 Unit-test `ParsePlaylistItems` (valid, unknown type, malformed JSON, over-cap) and `composeDeviceSources` fallback ladder (missing id, disabled, empty-after-resolution, partial resolution ordering) with table-driven tests in `handlers/websocket_test.go`
- [x] 4.2 Integration test in `main_test.go`: two devices, one `global` one bound to a 2-item playlist — assert slot sequence over a short timeout window and that preview still cycles globally
- [x] 4.3 Handler tests for playlist CRUD auth (anonymous redirected/401, session OK) mirroring existing datasource CRUD tests

## 5. Docs and validation

- [x] 5.1 Note in README (devices section): content modes, authored-order semantics, Random applies to global only
- [x] 5.2 Run `go build ./... && go test ./...` and `task pre-push` before pushing
