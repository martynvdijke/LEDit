# Spec: mqtt-actuation

## ADDED Requirements

### Requirement: Inbound per-device command topics

The MQTT controller SHALL subscribe, in addition to the existing control, display, and per-device `brightness/set`, `paused/set`, and `next/set` topics, to the wildcard topics `ledit/device/+/select/set` and `ledit/device/+/message/set`. Subscriptions SHALL be re-established on every reconnect and SHALL NOT alter the meaning or handling of any existing topic.

#### Scenario: New topics subscribed on connect

- **WHEN** the MQTT client connects with a non-empty broker
- **THEN** it SHALL subscribe to `ledit/device/+/select/set` and `ledit/device/+/message/set` in addition to the existing subscriptions

#### Scenario: Subscriptions restored after reconnect

- **WHEN** the broker connection drops and the client reconnects
- **THEN** the new and existing subscriptions SHALL be re-established before commands are expected to work

#### Scenario: Existing topics unchanged

- **WHEN** a message arrives on the control topic, display topic, or `brightness/set`, `paused/set`, `next/set`
- **THEN** it SHALL be handled exactly as before this capability existed

### Requirement: Commands route to the device's feed controller with offline-safe no-ops

Every per-device command SHALL resolve the target device's registered feed controller before acting. Unknown device ids, disabled devices, or devices without a registered controller SHALL be logged and ignored without mutating database state, publishing state, or producing an error. pause/resume/next SHALL map to the controller's pause/resume/next methods; brightness, select, and message SHALL map to the same shared actuation helpers used by the HA entities and rule actions, so all paths converge on one controller and one brightness resolution.

#### Scenario: Offline device no-op

- **WHEN** a command arrives for a device id that does not exist, is disabled, or has no active feed controller
- **THEN** the command SHALL be ignored with a debug log and no database write, no state publish, and no error response

#### Scenario: Pause and resume reach the controller

- **WHEN** a `paused/set` payload indicating pause or resume arrives for a device with a registered controller
- **THEN** that controller's pause or resume SHALL be applied (including the existing manual-pause scene suppression) and the device's paused state topic SHALL be updated

#### Scenario: Next reaches the controller

- **WHEN** a `next/set` message arrives for a device with a registered controller
- **THEN** that controller's next SHALL be applied, clearing any pin exactly as a manual or HA-triggered skip does

#### Scenario: Brightness reaches the live feed

- **WHEN** a `brightness/set` value arrives for a device with a registered controller
- **THEN** the value SHALL be validated, persisted as the device brightness override, applied to the live feed through the effective brightness path, and published on the brightness state topic

### Requirement: Select command grammar and effects

The `select/set` payload SHALL accept `source:<type>:<id>`, `playlist:<id>`, and `scene:<id>`, trimmed of surrounding whitespace. Unknown forms, unknown resources, disabled playlists, or sources outside the device's rotation SHALL be logged and ignored without changing controller or device state. Applied selections SHALL update the retained `ledit/device/<id>/select/state` topic and SHALL use the same semantics as the HA select entity: source pins, playlist persists content mode and restarts push transports, scene previews transiently.

#### Scenario: Source selected

- **WHEN** a `source:<type>:<id>` payload arrives and the source is in the device's rotation
- **THEN** the controller SHALL be pinned to that source and the select state topic SHALL be updated

#### Scenario: Playlist selected

- **WHEN** a `playlist:<id>` payload arrives for an existing enabled playlist
- **THEN** the device SHALL switch to playlist content mode with that playlist and the select state topic SHALL be updated

#### Scenario: Scene selected

- **WHEN** a `scene:<id>` payload arrives for an enabled scene
- **THEN** the scene SHALL be applied transiently through the scene preview path and the select state topic SHALL be updated

#### Scenario: Malformed or unknown selection ignored

- **WHEN** a payload is not one of the three forms, references an unknown id, or references a disabled playlist
- **THEN** no pin, content-mode change, or scene activation SHALL occur, and the command SHALL be logged

### Requirement: Message command semantics

The `message/set` payload SHALL be treated as plain text. A non-empty payload of up to 255 characters SHALL create a notification targeted at the topic's device using the webhook default TTL and the existing notification queue, history, and message lifecycle. Empty or whitespace-only payloads SHALL be ignored, and the retained `ledit/device/<id>/message/state` topic SHALL hold the last accepted text and SHALL be cleared when the message expires or resolves.

#### Scenario: Message rendered on the target device

- **WHEN** a non-empty message arrives for a device with a registered controller
- **THEN** a device-targeted notification SHALL be created and rendered by that device's feed, and the message state topic SHALL be updated

#### Scenario: Long message rejected

- **WHEN** a message payload exceeds the maximum length
- **THEN** the command SHALL be rejected with a log entry and no notification SHALL be created

#### Scenario: Empty message ignored

- **WHEN** an empty or whitespace-only payload arrives
- **THEN** no notification SHALL be created and no state SHALL change

### Requirement: Command validation and retained state feedback

Command payloads SHALL be validated before application: brightness values SHALL be integers clamped to 0–100, malformed numbers SHALL be ignored, select payloads SHALL conform to the documented grammar, and message length SHALL be capped. Rejected commands SHALL publish no state. Accepted commands SHALL use retained publishes on the documented state topics (`brightness/state`, `paused`, `select/state`, `message/state`) and SHALL NOT publish when MQTT is disconnected.

#### Scenario: Brightness clamped

- **WHEN** `brightness/set` carries a value below 0 or above 100
- **THEN** it SHALL be clamped to 0 or 100 respectively and applied at the clamped level

#### Scenario: Malformed brightness ignored

- **WHEN** `brightness/set` carries a non-integer payload
- **THEN** no brightness change or state publish SHALL occur

#### Scenario: Retained feedback

- **WHEN** an accepted command changes device state
- **THEN** the corresponding state topic SHALL be published retained so a later subscriber sees the current value

### Requirement: Authority model unchanged

Inbound MQTT command authorization SHALL remain solely the broker's ACL; the server SHALL NOT add or require a second application-level credential for these topics. Commands SHALL NOT bypass feed precedence, pins, pause semantics, or effective brightness resolution, and unknown topics or suffixes SHALL be ignored without side effects.

#### Scenario: No new credential

- **WHEN** a broker-authorized client publishes a valid select or message command
- **THEN** it SHALL be accepted without any additional token or secret

#### Scenario: Unknown suffix ignored

- **WHEN** a message arrives on `ledit/device/<id>/<unknown>/set`
- **THEN** it SHALL be ignored with a debug log and no state change

#### Scenario: Precedence preserved

- **WHEN** a select pin is active and a notification, scene, or alarm outranks it
- **THEN** the existing precedence SHALL still apply and the notification/scene/alarm SHALL display instead
