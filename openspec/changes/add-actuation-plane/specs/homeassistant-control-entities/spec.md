# Spec: homeassistant-control-entities

## ADDED Requirements

### Requirement: Additive per-device light entity

The server SHALL publish a Home Assistant MQTT discovery config for a `light` component for each device when HA discovery is enabled, additive to the existing `number` brightness entity. The light SHALL use the unique id `ledit_<id>_light`, reuse the existing retained topics `ledit/device/<id>/brightness/state` and `ledit/device/<id>/brightness/set`, declare a brightness scale of 0–100, and use `ledit/device/<id>/online` as its availability topic. Brightness commands SHALL be applied through the same device brightness path used by the existing `number` entity, and the light SHALL be removed along with the other entities when the device is deleted.

#### Scenario: Light config published

- **WHEN** HA discovery is enabled and a device discovery config is published
- **THEN** the server SHALL publish `homeassistant/light/ledit_<id>_light/config` (retained) with `unique_id` `ledit_<id>_light`, `state_topic` `ledit/device/<id>/brightness/state`, `command_topic` `ledit/device/<id>/brightness/set`, a brightness scale of 0–100, and the device block and availability topic shared with the other entities

#### Scenario: Existing brightness number unchanged

- **WHEN** the light entity is added
- **THEN** `homeassistant/number/ledit_<id>_brightness/config` SHALL keep its existing topic, `unique_id`, `state_topic`, `command_topic`, and payload semantics unchanged

#### Scenario: Brightness commands share one path

- **WHEN** a level 0–100 arrives on `ledit/device/<id>/brightness/set` from either the light or the number entity
- **THEN** the level SHALL be persisted as the device's brightness override, applied to the device's live feed, and reflected on `ledit/device/<id>/brightness/state`

#### Scenario: Off and on semantics

- **WHEN** the light is turned off, turned on, or set to an absolute brightness
- **THEN** off SHALL set level 0, on SHALL set level 100, and an absolute level SHALL be applied as given and clamped to 0–100

### Requirement: Per-device content select entity

The server SHALL publish a per-device `select` entity whose options are drawn from the device's current rotation sources, enabled playlists, and enabled scenes, using stable machine-readable option values: `source:<type>:<id>`, `playlist:<id>`, and `scene:<id>`. The entity SHALL use the command topic `ledit/device/<id>/select/set` and the retained state topic `ledit/device/<id>/select/state`, which SHALL hold the last applied option value.

#### Scenario: Select config published with options

- **WHEN** HA discovery is enabled and a device discovery config is published
- **THEN** the server SHALL publish `homeassistant/select/ledit_<id>_content/config` (retained) with `command_topic` `ledit/device/<id>/select/set`, `state_topic` `ledit/device/<id>/select/state`, and options containing one `source:<type>:<id>` value per rotation source plus `playlist:<id>` for enabled playlists and `scene:<id>` for enabled scenes

#### Scenario: Source selection pins the source

- **WHEN** `source:<type>:<id>` arrives on the select topic and that source is in the device's rotation
- **THEN** the device's feed controller SHALL be pinned to that source through the existing pin mechanism, and `ledit/device/<id>/select/state` SHALL be updated

#### Scenario: Source outside rotation is ignored

- **WHEN** `source:<type>:<id>` arrives and the source is not part of the device's current rotation
- **THEN** the command SHALL be ignored with a log entry, the pin state SHALL be unchanged, and no error SHALL surface to the caller

#### Scenario: Playlist selection switches content

- **WHEN** `playlist:<id>` arrives for an enabled playlist
- **THEN** the device's content mode SHALL be set to playlist mode with that playlist, push transports SHALL restart with the new content, and the device's WebSocket feed SHALL pick it up at its next rotation refresh without requiring a reconnect

#### Scenario: Scene selection previews the scene

- **WHEN** `scene:<id>` arrives for an enabled scene
- **THEN** the scene SHALL be applied transiently through the scene preview path (using the scene's TTL when set, otherwise the default hold), and the state topic SHALL be updated

#### Scenario: Options refresh when configuration changes

- **WHEN** playlists or scenes are created, updated, deleted, or enabled/disabled, or a device's save changes its content
- **THEN** the select config for affected devices SHALL be republished so Home Assistant's option list tracks the change

### Requirement: Per-device message text entity

The server SHALL publish a per-device `text` entity that renders arbitrary messages on that device. The entity SHALL use the command topic `ledit/device/<id>/message/set` and the retained state topic `ledit/device/<id>/message/state`, with a maximum input length of 255 characters. A non-empty payload SHALL create a notification targeted at that device using the existing notification TTL and lifecycle, so it outranks pinned rules and rotation and follows the normal notification precedence.

#### Scenario: Message config published

- **WHEN** HA discovery is enabled and a device discovery config is published
- **THEN** the server SHALL publish `homeassistant/text/ledit_<id>_message/config` (retained) with `command_topic` `ledit/device/<id>/message/set`, `state_topic` `ledit/device/<id>/message/state`, and a maximum length of 255

#### Scenario: Message renders only on the target device

- **WHEN** a non-empty message arrives on `ledit/device/<id>/message/set`
- **THEN** a notification containing the text SHALL be created for that device only, rendered by that device's feed, and reflected on `ledit/device/<id>/message/state`

#### Scenario: Empty payload ignored

- **WHEN** an empty or whitespace-only payload arrives on the message command topic
- **THEN** no message SHALL be created and no state SHALL change

#### Scenario: Message state clears on expiry

- **WHEN** the targeted message expires or is resolved
- **THEN** `ledit/device/<id>/message/state` SHALL be cleared (empty retained payload)

### Requirement: State republish on change

The server SHALL republish retained Home Assistant entity state when the underlying value changes, in addition to republishing configs and current state on MQTT connect/reconnect and on device create/update/delete. At minimum it SHALL publish on: brightness apply, pause/resume, current source change, device liveness change, select apply, and message fire/expiry. Republishing SHALL be idempotent per topic and value so duplicate triggers do not produce repeated retained publishes of an unchanged value.

#### Scenario: Brightness state republished on apply

- **WHEN** a brightness command or rule action changes a device's effective level
- **THEN** `ledit/device/<id>/brightness/state` SHALL be republished with the new level

#### Scenario: Pause state republished

- **WHEN** a feed is paused or resumed by any path
- **THEN** `ledit/device/<id>/paused` and the global `ledit/status/paused` SHALL be republished with the new state

#### Scenario: Liveness state republished

- **WHEN** a device connects or its liveness becomes stale
- **THEN** `ledit/device/<id>/online` SHALL be republished with `true` or `false`

#### Scenario: Current source republished

- **WHEN** a feed's current source changes
- **THEN** `ledit/status/current_source` SHALL be republished with the new source

#### Scenario: Connect republishes current state

- **WHEN** the MQTT client connects or reconnects
- **THEN** every enabled device's discovery configs and current entity state SHALL be published so Home Assistant converges on the current values

### Requirement: Discovery gating and backwards compatibility

HA entity publishing SHALL occur only when outbound MQTT settings have HA discovery enabled and the MQTT client is connected; otherwise publishes SHALL be no-ops and SHALL NOT create, modify, or delete Home Assistant entities. Publishing SHALL NOT depend on the generic outbound MQTT event toggle. Deleting a device SHALL clear the new light, select, and text configs and their state topics along with the existing ones, and no existing entity topic or unique id SHALL be renamed or removed.

#### Scenario: Discovery disabled

- **WHEN** HA discovery is disabled and any state-change trigger fires
- **THEN** the server SHALL NOT publish discovery configs or entity state

#### Scenario: MQTT disconnected

- **WHEN** MQTT is not configured or the connection is down and a state-change trigger fires
- **THEN** the publish SHALL be a no-op, and the next successful connect SHALL republish the current state

#### Scenario: Device deletion clears new entities

- **WHEN** an administrator deletes a device
- **THEN** the light, select, and text config topics and their retained state topics SHALL be cleared together with the existing device entity topics

#### Scenario: Generic MQTT event toggle does not gate HA entities

- **WHEN** HA discovery is enabled but the generic outbound MQTT event publishing toggle is disabled
- **THEN** HA entity configs and state SHALL still be published and remain current
