## ADDED Requirements

### Requirement: Playlist storage and management
The system SHALL provide a `Playlist` entity with `name`, `enabled` (default true), and an ordered `items` JSON column whose entries use the established `{"source_type": "<endpoint>", "source_id": <id>}` reference shape. Admins SHALL be able to create, edit, list, and delete playlists via session-authenticated admin routes following the existing CRUD pattern.

#### Scenario: Create a playlist with ordered items
- **WHEN** an admin submits a playlist named "Kitchen" with items `[{"source_type":"weather","source_id":3},{"source_type":"builtin","source_id":"analog-clock"}]`
- **THEN** the playlist is persisted with items in the submitted order and appears in the playlist list

#### Scenario: Built-in sources are referenceable
- **WHEN** an admin builds a playlist item picker
- **THEN** built-in ambience sources (`analog-clock:0`, `matrix-rain:0`) and System Stats (`systemstats:0`) are offered alongside configured datasources

#### Scenario: Delete a playlist
- **WHEN** an admin deletes a playlist that is bound to one or more devices
- **THEN** the deletion succeeds, devices keep their settings rows, and those devices fall back to the global source list on their next feed composition

### Requirement: Playlist references resolve through the shared source index
Playlist items SHALL be resolved via the same `buildSourceIndex`/`Resolve` machinery used by matrix bindings and datasource preview, so cache keys (`"<type>:<id>"`), display names, and available-source catalogs cannot diverge between features.

#### Scenario: Reference resolution matches matrix editor labels
- **WHEN** a playlist references `weather:3` and the matrix editor offers `weather:3`
- **THEN** both resolve to the same datasource instance configuration and display name

### Requirement: Devices select content mode
`DeviceSettings` SHALL gain `content_mode` (`global` | `playlist`, default `global`) and an optional `playlist_id`. The default value SHALL preserve today's behavior exactly. The device admin form SHALL expose a Content section with the mode toggle and a playlist picker of enabled playlists.

#### Scenario: New and existing devices default to global
- **WHEN** a device connects with no explicit content mode set
- **THEN** its feed is composed from the global source list identically to before this change

#### Scenario: Binding a device to a playlist
- **WHEN** an admin sets a device's content mode to `playlist` and selects "Kitchen"
- **THEN** subsequent feed compositions for that device use only the playlist's resolvable items

### Requirement: Device feeds cycle playlist sources in authored order
When a device in `playlist` mode connects, its feed SHALL cycle only the playlist's resolvable sources, in list order. The global `Random` shuffle setting SHALL NOT reorder playlist mode; it applies to global mode only. Pause/skip/next controls continue to operate on the device's own feed controller.

#### Scenario: Authored order preserved
- **WHEN** a playlist contains weather → calendar → transit and a bound device cycles three slots
- **THEN** the device shows weather, then calendar, then transit, repeating

#### Scenario: Per-device control still independent
- **WHEN** an operator sends skip on one playlist-bound device's connection
- **THEN** only that device advances to its next playlist item

### Requirement: Invalid or empty playlists fall back to the global list
Feed composition SHALL fall back to the global source list when the bound playlist is missing, disabled, or yields zero resolvable items, logging a warning. Partially resolvable playlists SHALL cycle only the resolvable items, skipping dangling references with a log entry emitted once per connection setup rather than per slot.

#### Scenario: Dangling reference skipped
- **WHEN** a playlist references `weather:99` which no longer exists alongside valid items
- **THEN** the device cycles only the valid items and a warning names the skipped reference

#### Scenario: Fully invalid playlist falls back
- **WHEN** a bound playlist has no resolvable items at all
- **THEN** the device receives the global source list instead of an empty rotation

### Requirement: Preview feed remains global
The browser preview feed (`/ws/feed`) SHALL always compose from the global source list regardless of any device's content mode.

#### Scenario: Preview unaffected by playlists
- **WHEN** all devices are bound to playlists and an operator opens `/ws/feed`
- **THEN** the preview cycles the full global source list

### Requirement: Source catalog parity
After this change, every source `loadSources` can emit SHALL have a counterpart entry in `buildSourceIndex`/`bindingOptions` (including `systemstats:0`), verified by a test, so playlist pickers never offer a divergent catalog.

#### Scenario: System Stats selectable everywhere
- **WHEN** the playlist item builder or matrix editor lists available sources
- **THEN** System Stats appears as `systemstats:0` with label "System Stats"
