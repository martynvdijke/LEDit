# Spec: input-bindings

## ADDED Requirements

### Requirement: Input binding model and storage

The system SHALL store admin-managed input bindings as `InputBinding` rows with: an optional device scope (null means any device), a `source` and `event` from the closed input vocabulary, an optional `match` value (for example an NFC tag id), an `action` JSON document, an `enabled` flag, and an `order` integer. Bindings SHALL be creatable, editable, listable, deletable, enableable, and orderable from an admin-session surface, and an invalid action (unknown kind, missing/invalid target, out-of-range brightness) SHALL be rejected with a 400 rather than stored.

#### Scenario: Administrator creates a binding

- **WHEN** an administrator saves a binding for `source=nfc`, `event=tap`, `match=04a1b2c3`, action `scene:<id>`
- **THEN** the binding SHALL persist, appear in the binding list, and become active for resolution

#### Scenario: Invalid action rejected

- **WHEN** an administrator saves a binding whose action names a non-existent scene or an out-of-range brightness level
- **THEN** the server SHALL reject the request with a 400 and SHALL NOT store the binding

#### Scenario: Disabled binding inert

- **WHEN** a binding is disabled
- **THEN** it SHALL NOT be evaluated and its action SHALL never run

### Requirement: Ordered first-match resolution

Input dispatch SHALL resolve an incoming event against enabled bindings scoped to that device plus global bindings, ordered by `order` and then id, with device-scoped bindings sorting ahead of global ones, and SHALL execute at most the first matching binding. When no binding matches, the server SHALL apply the built-in defaults defined for the event (or ignore the event when no default exists) and SHALL log unmatched events at debug. A failing action SHALL be logged and SHALL NOT close the connection or stop frame delivery.

#### Scenario: First match wins and runs once

- **WHEN** two enabled bindings both match an event
- **THEN** only the first in resolution order SHALL execute and the second SHALL be skipped

#### Scenario: Device-scoped binding takes precedence over global

- **WHEN** a global binding and a device-scoped binding both match the same event at the same order
- **THEN** the device-scoped binding SHALL execute

#### Scenario: No match falls back safely

- **WHEN** an event matches no binding and has no built-in default, such as an unbound NFC tag
- **THEN** the server SHALL ignore it with a debug log and SHALL NOT disturb the feed

#### Scenario: Action failure is non-fatal

- **WHEN** a matched binding's action fails (for example its scene row was deleted)
- **THEN** the server SHALL log a warning, skip the action, and keep the device connection streaming

### Requirement: Actions converge on existing behaviour

Every binding action SHALL execute by calling the subsystem that already owns that behaviour, with no parallel action path: `scene` through the scene manager and scene source resolution; `playlist` by updating the device's playlist/content mode and requesting a live source reload (or restarting a non-WebSocket transport); `feed` through the device's `FeedController` (or the global controller when the device has no live feed) with `previous` symmetric to `next`; `brightness` through the live override tier and persisted `brightness_override`; `rule` through the existing rule `then_actions` executor; `greeting` through the greeting rule template, quiet-hours, and cooldown logic plus the existing notification path; and `notification` through the existing notification queue.

#### Scenario: Scene action respects display precedence

- **WHEN** a binding runs a scene while a notification, incident, or wake alarm is active
- **THEN** the scene SHALL NOT preempt the higher tier, exactly as when a scene activates from its own triggers

#### Scenario: Playlist action switches the live feed

- **WHEN** a binding selects a playlist for a connected device
- **THEN** the device content mode and playlist SHALL update and the live feed SHALL switch to the new playlist without requiring a device reconnect

#### Scenario: Feed previous is symmetric to next

- **WHEN** a binding runs the `previous` feed action
- **THEN** the device feed SHALL step to the previous source in its rotation order, using the same per-device controller as `next`, `pause`, and `resume`

#### Scenario: Brightness action is visible immediately and persists

- **WHEN** a binding sets brightness to a level 0-100
- **THEN** the live device brightness hint SHALL reflect the new level and the persisted override SHALL hold it across reconnects

#### Scenario: Rule action fires the rule's then-actions

- **WHEN** a binding targets an enabled display rule
- **THEN** the server SHALL execute that rule's configured `then_actions` exactly as the event evaluator would, without re-evaluating the rule's polling condition

### Requirement: NFC/RFID tap to scene or playlist

Tap events SHALL be matchable by tag id, so an administrator can bind a specific tag to a scene or playlist action ("tap to start dinner mode"). A tag with no matching binding SHALL be ignored, and rapid repeat reads of the same tag SHALL be protected by the device-side dedupe window so a single physical tap cannot fire an action twice.

#### Scenario: Tag starts a scene

- **WHEN** a tap event matches a binding whose action is scene "Dinner"
- **THEN** the scene action SHALL run through the normal scene path and the wall SHALL show that scene subject to existing precedence

#### Scenario: Tag switches a playlist

- **WHEN** a tap event matches a binding whose action selects a playlist
- **THEN** the device's live feed SHALL switch to that playlist

#### Scenario: Unknown tag ignored

- **WHEN** a tap event arrives for a tag id with no matching enabled binding
- **THEN** the server SHALL take no action and log the event at debug

### Requirement: Presence integration with the greeting pipeline

Presence edge events SHALL be usable by bindings, including a greeting action that references an existing greeting rule. The greeting action SHALL reuse the greeting rule's template resolution, quiet-hours suppression, and persisted/in-memory cooldown before calling the existing notification path, and SHALL update the rule's last-triggered timestamp, so local presence greetings behave like HA-driven greetings. Existing `entity_path`-based greeting rules SHALL be unaffected.

#### Scenario: Local presence fires a greeting once

- **WHEN** a presence `present` edge matches a greeting binding whose rule has a welcome template
- **THEN** exactly one notification SHALL be pushed with the resolved template, subject to quiet hours and cooldown

#### Scenario: Quiet hours and cooldown suppress local greetings

- **WHEN** the edge arrives during the rule's quiet hours or within its cooldown
- **THEN** no notification SHALL be pushed and the rule's last-triggered state SHALL follow the same semantics as the HA watcher

#### Scenario: HA greeting rules unchanged

- **WHEN** no presence binding exists
- **THEN** the existing HA entity-based greeting watcher SHALL behave exactly as before

### Requirement: Ambient lux feeds the brightness resolution

The brightness pipeline SHALL accept device-reported lux as a sensor source alongside the existing Home Assistant sensor fetch. Sensor configuration SHALL select the source (`ha` default, or `device` for local readings). A device lux reading SHALL be used only while fresh, using the same staleness bound as the existing sensor cache, and SHALL be ignored when stale or absent so resolution falls back to the HA sensor and then the schedule. The existing precedence (manual override > active alarm > active scene > sensor > schedule > 100) SHALL be unchanged, and both WebSocket and push-transport devices SHALL resolve device lux the same way.

#### Scenario: Fresh local lux drives brightness

- **WHEN** a device configured with `source: device` reports a lux value and its brightness resolution runs
- **THEN** the level SHALL be derived from that reading through the existing lux-to-level mapping

#### Scenario: Stale local lux falls back

- **WHEN** the last device lux reading is older than the staleness bound
- **THEN** the resolution SHALL ignore it and fall back to the HA sensor (when configured) and schedule as today

#### Scenario: Manual override still wins

- **WHEN** a brightness override is set while a fresh device lux reading exists
- **THEN** the override SHALL win exactly as it does for HA sensor readings

### Requirement: Optional MQTT and HA exposure

When the MQTT control plane is connected, the server SHALL publish each accepted input event to `ledit/device/<id>/input/<event>` through the existing outbound publish helper, and HA discovery (when enabled) SHALL expose a per-device input event entity whose state topic reflects the last input event. Publishing SHALL be a no-op when MQTT is not connected, payloads SHALL contain only bounded enum/label/id values, and no token or secret SHALL ever appear in an input payload or topic.

#### Scenario: Event published to MQTT

- **WHEN** an accepted input event is dispatched while MQTT is connected
- **THEN** a message SHALL be published on `ledit/device/<id>/input/<event>` with a bounded JSON payload

#### Scenario: MQTT disabled is a no-op

- **WHEN** MQTT is not configured or not connected
- **THEN** input dispatch SHALL proceed normally with no publish attempt and no error

#### Scenario: HA event entity discovered

- **WHEN** HA discovery is enabled for a device
- **THEN** an input event entity SHALL be published with the closed event vocabulary and the device's state topic

### Requirement: Input bindings admin surface

The admin UI SHALL provide a bindings page listing each binding's device scope, source, event, match, action, order, and enabled state, with add/edit/delete and ordering controls, and the device admin surface SHALL show the input sources known for that device. The event vocabulary and hardware wiring SHALL be documented for operators.

#### Scenario: Binding list rendered

- **WHEN** an administrator opens the input bindings page
- **THEN** all bindings SHALL be listed with their scope, match, action, order, and enabled state and SHALL be editable or deletable

#### Scenario: Device input status visible

- **WHEN** an administrator views a device that has reported input events
- **THEN** the device surface SHALL show which input sources have been seen without exposing any token or secret
