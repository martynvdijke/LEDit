# Spec: rule-action-expansion

## ADDED Requirements

### Requirement: Backward-compatible then-action extension

Event-rule then-actions SHALL support, in addition to the existing `none`, `scene`, `notification`, and `webhook` kinds, the new kinds `brightness`, `source`, `playlist`, `mqtt`, and `http`. Stored rule JSON for the existing kinds SHALL continue to parse and execute unchanged, unknown kinds SHALL still be rejected at parse time, and the admin form SHALL continue to round-trip existing rules without alteration until an administrator edits them.

#### Scenario: Existing kinds unchanged

- **WHEN** a rule containing a `scene`, `notification`, or `webhook` then-action is loaded after this change
- **THEN** it SHALL parse, validate, and execute exactly as before

#### Scenario: New kinds round-trip

- **WHEN** an administrator saves a rule with a new kind and its required fields
- **THEN** the configuration SHALL be persisted in `then_actions`, validated, and re-rendered on the edit form

#### Scenario: Unknown kind rejected

- **WHEN** a rule is submitted with a kind that is not one of the supported values
- **THEN** the save SHALL be rejected with a human-readable validation error

#### Scenario: Stored rule with new kind on older binary

- **WHEN** a rule authored with a new kind is evaluated by a binary that does not support it
- **THEN** the action SHALL be skipped with a warning and the rule's condition evaluation SHALL be unaffected

### Requirement: Set brightness action

The `brightness` action SHALL accept `device_id` (0 meaning every enabled device) and a required `level` in the 0–100 range. Execution SHALL persist the level as each target device's brightness override, apply it through the same effective brightness path used by manual overrides and MQTT/HA commands, suppress ambient scene brightness exactly as a manual override does, restart push transports with the new level, and publish the HA brightness state for the target device(s).

#### Scenario: Device-targeted brightness

- **WHEN** a rule with `brightness` fires with a device id and level 25
- **THEN** that device's brightness override SHALL become 25, its live feed SHALL dim without requiring a reconnect, and its brightness state topic SHALL be published

#### Scenario: All-devices brightness

- **WHEN** a rule with `brightness` fires with device id 0
- **THEN** every enabled device SHALL receive the level through the same shared helper

#### Scenario: Out-of-range level rejected

- **WHEN** an administrator saves a `brightness` action with a level outside 0–100
- **THEN** the save SHALL be rejected with a validation error

#### Scenario: Missing device fails open

- **WHEN** a rule with `brightness` fires for a device that no longer exists or is disabled
- **THEN** the action SHALL be skipped with a log entry and rule evaluation SHALL continue

### Requirement: Pin source action

The `source` action SHALL accept `source_type` and `source_id`, forming the established `<type>:<id>` source key, and SHALL pin that source on all active feed controllers using the existing pin slot and controller fan-out. The pin SHALL be attributed to the firing rule, SHALL be ignored by controllers whose rotation does not contain the source, and SHALL be cleared by a manual or MQTT/HA next/skip exactly like any other pin.

#### Scenario: Source pinned

- **WHEN** a rule with `source` fires
- **THEN** every active feed controller SHALL be pinned to that source key with attribution to the rule

#### Scenario: Skip clears the pin

- **WHEN** next/skip is triggered while a `source` rule pin is held
- **THEN** the pin SHALL clear and rotation SHALL advance

#### Scenario: Unresolvable source no-op

- **WHEN** a rule with `source` references a source that cannot be resolved
- **THEN** the action SHALL be skipped with a log entry and other controllers SHALL be unaffected

### Requirement: Switch playlist action

The `playlist` action SHALL accept a required `device_id` and `playlist_id`, validate that the playlist exists and is enabled, then persist playlist content mode for that device mirroring the admin device-update path and restart any push transport so the change applies. WebSocket connections SHALL pick up the new content at their next rotation refresh without a forced reconnect. Unknown devices and unknown/disabled playlists SHALL be skipped with a log entry.

#### Scenario: Playlist switched

- **WHEN** a rule with `playlist` fires for an enabled device and enabled playlist
- **THEN** the device's content mode SHALL become playlist mode with that playlist, push transports SHALL restart, and the WebSocket feed SHALL reflect the new content at its next rotation refresh

#### Scenario: Disabled playlist rejected at execution

- **WHEN** a rule with `playlist` fires and the playlist has since been disabled or deleted
- **THEN** the device SHALL keep its current content and the action SHALL be logged as skipped

#### Scenario: Disabled playlist rejected at save

- **WHEN** an administrator saves a `playlist` action with no playlist id or a missing device
- **THEN** the save SHALL be rejected with a validation error

### Requirement: MQTT publish action

The `mqtt` action SHALL accept a required `topic`, an optional `payload`, and an optional `retain` flag defaulting to false. Topic validation SHALL reject empty topics and topics containing `+` or `#` wildcards, and payloads SHALL be bounded in size. Execution SHALL publish through the existing outbound publish path and SHALL be a logged no-op when MQTT is unconfigured or disconnected.

#### Scenario: Message published

- **WHEN** a rule with `mqtt` fires while MQTT is connected
- **THEN** the topic and payload SHALL be published with the configured retain flag

#### Scenario: Wildcard topic rejected at save

- **WHEN** an administrator saves an `mqtt` action with a `+` or `#` wildcard in the topic or with an empty topic
- **THEN** the save SHALL be rejected with a validation error

#### Scenario: Disconnected broker no-op

- **WHEN** a rule with `mqtt` fires while MQTT is unconfigured or disconnected
- **THEN** the action SHALL log and return without error and without affecting rule evaluation

### Requirement: Signed outbound HTTP action

The `http` action SHALL accept a required `url` with an `http` or `https` scheme and optional `method` (default POST), `secret`, `event`, and JSON-object `payload`. Execution SHALL build a JSON body, and when a secret is configured SHALL sign the raw body with HMAC-SHA256 and send `X-LEDit-Signature: sha256=<hex>` plus the event header, matching the existing outbound webhook signing convention. Delivery SHALL be dispatched asynchronously so a slow or unreachable endpoint never blocks the rule evaluator, SHALL reuse the existing retry/backoff and delivery-log behavior, and SHALL never log the configured secret.

#### Scenario: Signed request

- **WHEN** a rule with `http` and a secret fires
- **THEN** the receiver SHALL get a POST with a JSON body and an `X-LEDit-Signature` that verifies as HMAC-SHA256 of the raw body under the secret

#### Scenario: Unsigned request when no secret

- **WHEN** a rule with `http` and no secret fires
- **THEN** the request SHALL be sent without a signature header

#### Scenario: Slow endpoint does not block evaluation

- **WHEN** a rule fires and the configured endpoint is unreachable
- **THEN** the action SHALL enqueue the delivery, rule evaluation SHALL proceed, and the failure SHALL be recorded in the delivery log without exposing the secret

#### Scenario: Invalid URL rejected

- **WHEN** an administrator saves an `http` action with a missing URL, a non-http(s) scheme, or a malformed payload
- **THEN** the save SHALL be rejected with a validation error

### Requirement: Admin configuration and validation

The event-rule admin form SHALL expose fields for the new action kinds and SHALL persist and pre-populate them alongside the existing fields. Validation errors SHALL be shown inline on the form, and re-saving a rule that uses one of the four existing kinds SHALL not introduce new fields or change the stored JSON shape.

#### Scenario: New-kind fields rendered

- **WHEN** an administrator opens the event-rule form and chooses a new kind
- **THEN** the fields required for that kind SHALL be available and submitted with the rule

#### Scenario: Validation feedback

- **WHEN** a new-kind action fails validation on save
- **THEN** the form SHALL be re-rendered with the error message and the administrator's entered values preserved

#### Scenario: Existing rule re-save unchanged

- **WHEN** an administrator edits and saves a rule using `scene`, `notification`, or `webhook` without changing the action
- **THEN** the stored `then_actions` JSON SHALL remain semantically identical

### Requirement: Execution safety and failure isolation

Then-action execution SHALL remain best-effort: missing targets, disabled resources, disconnected MQTT, and HTTP failures SHALL be logged and skipped, SHALL NOT panic, and SHALL NOT block, blank, or interrupt any feed. Action execution SHALL stay within the existing bounded action context, and controller operations SHALL use the existing mutex-protected controller APIs so concurrent rule evaluation cannot corrupt feed state.

#### Scenario: Nil database client safe

- **WHEN** an action executes without a database client
- **THEN** it SHALL log and return without panicking and without changing feed state

#### Scenario: Feed never blocked

- **WHEN** any new action fails
- **THEN** the rule evaluator SHALL continue to the next rule and every feed SHALL keep rendering

#### Scenario: Controller state remains consistent

- **WHEN** multiple rules and MQTT/HA commands act on the same device concurrently
- **THEN** pin, pause, and brightness updates SHALL go through the existing controller locking and SHALL not corrupt controller state
