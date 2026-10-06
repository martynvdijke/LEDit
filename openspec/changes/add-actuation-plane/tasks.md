## 1. Shared Actuation Helpers

- [ ] 1.1 Create `handlers/actuation.go` with `ApplyDeviceBrightness(deviceID, level int, source string)`: clamp 0–100, persist `DeviceSettings.BrightnessOverride` (with a background-context fallback like the existing MQTT handler at `handlers/mqtt.go:191-195`), call `SuppressActiveScene()` for a manual override, `RestartTransportDevice` for push transports, set the live controller hint, and publish `ledit/device/<id>/brightness/state`
- [ ] 1.2 Add `ApplyDeviceSelect(deviceID int, value string)`: parse the `source:<type>:<id>` / `playlist:<id>` / `scene:<id>` grammar, resolve the device, and dispatch to pin, content-mode update, or `PreviewScene`; return a typed error/message for unknown forms so callers can log consistently
- [ ] 1.3 Add `ApplyDeviceMessage(deviceID int, text string)`: trim, enforce the 255-character cap, reject empty, and enqueue a device-targeted notification with `WithTTL(webhookDefaultTTL())`; publish `ledit/device/<id>/message/state`
- [ ] 1.4 Add a `withTargetDevice(id int)` `NotifOption` to `handlers/feed_control.go` and store the target on `notifEntry` with `json:"-"` so the public notification shape stays byte-identical
- [ ] 1.5 Filter targeted notifications in `serveFeed` (`handlers/websocket.go:1222-1240`): advance the cursor over every notification but render only when the target is 0 or equals `fc.deviceID`
- [ ] 1.6 Add a mutex-guarded ephemeral brightness hint to `FeedController` (`handlers/feed_control.go:13`) with `SetBrightnessHint`/`BrightnessHint`; consult it with top precedence in the WS `bFn` closure (`handlers/websocket.go:844-880`) before the DB override
- [ ] 1.7 Extend the WS `reschedule` closure (`handlers/websocket.go:888-912`) so it is created for every device connection, re-reads the device row at the existing 60 s throttle, and replaces `sources` when `sameSources` reports a change (covers playlist/content-mode switches without a reconnect)
- [ ] 1.8 Unit tests for `handlers/actuation.go`: brightness clamp/persist/hint, select grammar (all three forms, malformed, unknown, off-rotation source), message cap/empty, and targeted-notification filtering

## 2. Home Assistant Discovery Expansion

- [ ] 2.1 Add the per-device light config to `publishHADiscoveryForDevice` (`handlers/ha_discovery.go:23`): `homeassistant/light/ledit_<id>_light/config`, template schema, `unique_id` `ledit_<id>_light`, existing brightness state/set topics, 0–100 brightness scale, shared device block and availability topic
- [ ] 2.2 Add the content select config (`homeassistant/select/ledit_<id>_content/config`) with a builder that produces `source:<type>:<id>` options from the device's rotation (reuse the cache-key shape from `handlers/websocket.go`), `playlist:<id>` from enabled playlists, and `scene:<id>` from enabled scenes
- [ ] 2.3 Add the message text config (`homeassistant/text/ledit_<id>_message/config`) with `ledit/device/<id>/message/set` / `ledit/device/<id>/message/state` and a 255 max length
- [ ] 2.4 Extend `clearHADiscoveryForDevice` (`handlers/ha_discovery.go:197`) with the three new config topics and the `select/state` and `message/state` state topics
- [ ] 2.5 Extend `publishHAStateForDevice` (`handlers/ha_discovery.go:115`) to also publish current paused state, select state, and message state (retained) during connect/device-save republish
- [ ] 2.6 Extend `handlers/ha_discovery_test.go`: the new configs are published with the correct topics/unique ids/command topics; the existing number/switch/button/sensor configs are byte-identical; delete clears the new topics

## 3. HA State Republish Sink

- [ ] 3.1 Implement an HA state sink (in `handlers/ha_discovery.go` or a new `handlers/ha_state.go`) registered on `GlobalBus` alongside `GlobalMqttSink` (`handlers/outbound.go:543`), gated by `haDiscoveryEnabled`, handling `EventDeviceLivenessChanged`, `EventFeedPaused`, `EventFeedResumed`, `EventSourceChanged`, brightness apply, select apply, and message fired/resolved
- [ ] 3.2 Cache the last published value per topic to suppress duplicate retained publishes, and clear the cache/watch list safely on device delete
- [ ] 3.3 Republish select configs when playlists or scenes change: hook the admin playlist/scene create/update/delete/toggle handlers (`handlers/` playlist and `handlers/scene_admin.go`) to call a bounded `republishHASelectsForAll` (no-op when discovery disabled)
- [ ] 3.4 Ensure entity state publishes are independent of `outbound_settings.mqtt_publish_enabled` and no-op cleanly when MQTT is disconnected
- [ ] 3.5 Tests: event triggers publish the expected retained topics; disabled discovery publishes nothing; disconnected MQTT does not error; duplicate event values do not republish; connect republishes current state

## 4. Inbound MQTT Command Expansion

- [ ] 4.1 Extend `MQTTController.subscribeAll` (`handlers/mqtt.go:111`) with `ledit/device/+/select/set` and `ledit/device/+/message/set` using the existing wildcard-subscribe pattern, so `reconnect` re-establishes them automatically
- [ ] 4.2 Extend `handlePerDeviceCommand` (`handlers/mqtt.go:162`) with `select/set` and `message/set` cases that call `ApplyDeviceSelect` / `ApplyDeviceMessage`; keep `brightness/set`, `paused/set`, and `next/set` behavior and logging unchanged
- [ ] 4.3 Route the existing `brightness/set` case through `ApplyDeviceBrightness` so MQTT, HA, and rules share clamping/persistence/state/controller behavior (preserving current payload parsing and `0..100` clamp tests)
- [ ] 4.4 Enforce offline-safe no-ops: resolve `getDeviceFeed(id)`/device row first; unknown id, disabled device, or no controller logs and returns without DB writes or state publishes
- [ ] 4.5 Publish retained state feedback on accepted commands (`select/state`, `message/state`; `brightness/state` already published); publish nothing on rejected commands
- [ ] 4.6 Extend `handlers/mqtt_test.go` (fake client pattern): new subscriptions after connect/reconnect; select/playlist/scene payloads; malformed/unknown selection no-op; message targeting; empty/long message rejection; brightness clamp through the shared helper; offline no-op for a missing device

## 5. Rule Then-Action Model

- [ ] 5.1 Extend `ThenAction` (`handlers/ruleactions.go:15`) with optional `device_id`, `level`, `source_type`, `source_id`, `playlist_id`, `topic`, `payload`, `retain`, `url`, and `method` fields, keeping existing JSON field names and omitempty behavior
- [ ] 5.2 Extend `ParseThenAction` (`handlers/ruleactions.go:26`) to accept the five new kinds while still mapping `""`/`{}` to `none` and rejecting unknown kinds
- [ ] 5.3 Extend `validateThenAction` (`handlers/ruleactions.go:47`): level range, required device/playlist/source fields, MQTT topic syntax (non-empty, no `+`/`#`), payload JSON-object checks, and http/https URL validation
- [ ] 5.4 Implement the new executor cases in `executeThenAction` (`handlers/ruleactions.go:77`): `brightness` via `ApplyDeviceBrightness` (device_id 0 = all enabled), `source` via `pinAll("<type>:<id>", "rule:"+rule.Name)`, `playlist` via the shared content update path, `mqtt` via `PublishOutbound` with validation and a logged no-op when disconnected
- [ ] 5.5 Implement the `http` action: build the JSON envelope, sign the raw body with `hmac.New(sha256.New, secret)` and `X-LEDit-Signature: sha256=<hex>` matching `handlers/outbound.go:359-392`, dispatch asynchronously through a bounded worker that reuses the existing retry/backoff policy and records the delivery status, and never log the secret
- [ ] 5.6 Keep every new case best-effort: nil DB/controller, unknown device/playlist/source, and HTTP failures log and return without panicking or blocking the evaluator
- [ ] 5.7 Extend `buildThenJSON` (`handlers/eventrules.go:630`) and `eventRuleFormVarsWithThen` (`handlers/eventrules.go:605`) and the create/update handler form parsing (`handlers/eventrules.go:790-1000`) for the new fields, preserving the existing-kind JSON shape
- [ ] 5.8 Extend `web/templates/admin/eventrule_form.html` with kind-specific field groups for brightness/source/playlist/mqtt/http and client-side show/hide, without breaking the existing four-kind rendering
- [ ] 5.9 Tests: `handlers/ruleactions_test.go` parse/validate for every new kind and rejection cases; executor tests for brightness persistence/hint, pin attribution and skip-clearing, playlist switch, MQTT publish (fake client), and HTTP signing headers/body; backward-compatibility test that an existing `scene`/`notification`/`webhook` JSON round-trips unchanged

## 6. Wiring, Docs & Verification

- [ ] 6.1 Register the HA state sink and wire the new republish hooks in `handlers/server.go` initialization (or the existing `GlobalDispatcher.Register` site in `handlers/outbound.go`)
- [ ] 6.2 Confirm no new routes are needed; verify admin event-rule create/edit/update flows render and persist the new kinds end to end
- [ ] 6.3 Update `README.md`: Home Assistant section (light/select/text entities, topics, republish behavior), Push-to-Display section (select/message MQTT topics), and Event-Driven Switching section (new then-action kinds and semantics)
- [ ] 6.4 Add a short help note on the MQTT settings page (`web/templates/admin/mqtt.html` or the integrations settings template) documenting the new per-device topics and that broker ACLs are the authority
- [ ] 6.5 Run `task pre-push` (gofmt, tests, build) and fix failures; ensure no changes to `device/` are needed
- [ ] 6.6 Run `openspec validate add-actuation-plane --strict` and confirm it passes
