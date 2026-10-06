## 1. Input Event Protocol & Ingestion

- [ ] 1.1 Add `inputs` to the v2 welcome capability list in `HandleDeviceWS` (`handlers/websocket.go:761-771`) and extend `TestDeviceProtocolNegotiation` to assert the new capability is present while `brightness`/`spectrum`/`hold` remain
- [ ] 1.2 Define the input event model in a new `handlers/input_events.go`: closed source vocabulary (`button:next`, `button:pause`, `encoder`, `nfc`, `pir`, `mmwave`, `lux`), closed event vocabulary (`press`, `rotate`, `tap`, `presence`, `lux`), per-event value validation and bounds, and a bounded frame-size cap; follow the `parseSpectrumBins` drop-don't-error style (`handlers/websocket.go:1028`)
- [ ] 1.3 Add unit tests for `ParseInputEvent`: valid events per source, unknown source/event rejected, tag ids length/charset enforced, rotate steps clamped, presence value restricted, lux non-finite/out-of-range rejected
- [ ] 1.4 Ingest `{"type":"input",...}` in the `serveFeed` control read loop beside the spectrum branch (`handlers/websocket.go:1100-1155`); accept only when `fc.protocol >= 2` and `fc.deviceID > 0`, ignore for v1 and the admin preview feed
- [ ] 1.5 Add a per-connection input rate limiter (token bucket, defaults ~20 events/s burst 40) in the read loop; excess events are debug-logged and dropped without disconnecting
- [ ] 1.6 Provide a dispatch seam from `serveFeed` to the server: register a per-device input sink from `HandleDeviceWS` (mirroring `registerDeviceFeed`/`unregisterDeviceFeed` in `handlers/feed_control.go:32-57`) so the read loop never needs a full `*Server`
- [ ] 1.7 Tests: valid event reaches the sink, v1 ignores input messages, malformed/oversized frames are dropped, rate limit drops excess while frames keep streaming, preview connections cannot dispatch

## 2. Input Binding Model & Dispatch Engine

- [ ] 2.1 Add `ent/schema/input_binding.go` (`device_id` nullable, `source`, `event`, `match`, `action` JSON text, `enabled` default true, `order` default 0, timestamps); run `go generate ./ent` and verify the additive migration on an existing database
- [ ] 2.2 Implement binding resolution in `handlers/input_bindings.go`: load enabled device-scoped plus global bindings, order by `order` then id with device-scoped first, and execute the first match exactly once
- [ ] 2.3 Implement the built-in defaults applied when no binding matches: `button:next` press → next, `button:pause` press → pause, `hold` → unchanged no-op, encoder rotate +/− → next/previous, encoder press → brightness cycle (presets 25/50/75/100); keep legacy `{"action":...}` handling in `handlers/websocket.go:1140-1154` untouched
- [ ] 2.4 Define the `InputAction` JSON struct and `ParseInputAction`/`ValidateInputAction` in the `ThenAction` style (`handlers/ruleactions.go:15-75`) for kinds `scene`, `playlist`, `feed`, `brightness`, `rule`, `greeting`, `notification`; validate targets exist and brightness is 0-100 at save time
- [ ] 2.5 Extend `FeedController` (`handlers/feed_control.go:13`) with a `Prev` flag mirroring `Skip`, add `Previous()`, and consume it symmetrically to `Skip` in `serveFeed`; unit-test that `next`/`pause`/`resume` behavior is unchanged when `Prev` is never set
- [ ] 2.6 Add a per-device source-reload request: make the device source resolver (`reschedule` closure, `handlers/websocket.go:887-919`) available to all live device feeds, add a reload flag consumed at the rotation boundary, and use it after a playlist action; test that the live feed switches playlists without a reconnect
- [ ] 2.7 Add a live brightness override registry keyed by device id, consult it in the `bFn` override tier (`handlers/websocket.go:818-880`) and in `pushBrightness.level` (`handlers/feed_render.go:134-159`), persist `brightness_override` (`ent/schema/device_settings.go:46`), and call `RestartTransportDevice` for non-WebSocket transports
- [ ] 2.8 Implement action executors: scene → `sceneFromEnt`/`PreviewScene`/`resolveSceneSource` (`handlers/scenes.go:701,764`); playlist → device update + reload (2.6); feed → `getDeviceFeed` else `GlobalFeed` with verbs including `previous`; brightness → 2.7; rule → `executeThenAction` (`handlers/ruleactions.go:77`); greeting → quiet hours/cooldown/`ResolveTemplate` + `server.AddNotification` (`handlers/greetings.go:110-199`); notification → `AddNotification`
- [ ] 2.9 Unit tests: first-match-wins, device-scoped beats global, disabled skipped, unmatched unknown tag is a debug no-op, deleted scene/rule target logged and skipped, action failure never closes the connection
- [ ] 2.10 Tests that convergence holds: a scene action cannot preempt an active notification/incident/alarm; a rule action runs the rule's `then_actions` without re-evaluating its condition; a greeting action respects quiet hours and cooldown and persists `last_triggered_at`

## 3. Device Client Input Sources

- [ ] 3.1 Add config helpers to `device/ledit_device/config.py` (`LEDIT_NFC_DEDUPE_MS`, `LEDIT_ENCODER_CLK_PIN`/`LEDIT_ENCODER_DT_PIN`/`LEDIT_ENCODER_SW_PIN`, `LEDIT_ENCODER_DEBOUNCE_MS`, `LEDIT_PRESENCE_PIN`/`LEDIT_PRESENCE_KIND`, `LEDIT_LUX_INTERVAL_MS`, `LEDIT_LUX_CHANGE_THRESHOLD`, `LEDIT_INPUTS`) using the existing `env_int`/`env_bool` helpers with safe defaults
- [ ] 3.2 Add `device/ledit_device/inputs.py`: event encoding (`{"type":"input","source":...,"event":...,"value":...,"ts":...}`), an `InputHub` with `sender(str)`, capability gating, idempotent `start()`/`stop()`, and per-source construction in try/except that logs once and stays inert on missing pins/libraries (mirror `ButtonHandler.start()`, `buttons.py:187-221`)
- [ ] 3.3 Extend `client.py` `_handle_welcome` (`client.py:165-176`) to track the `inputs` capability, expose a `send_input` that emits input events when negotiated and legacy `{"action":...}` otherwise, and stop the hub in `close()`
- [ ] 3.4 Update `buttons.py` to route short-press/long-press callbacks through the hub when `inputs` is active while preserving the existing legacy sender and gesture state machine; keep `test_buttons.py` green and add an inputs-mode case
- [ ] 3.5 Update `__main__.py:54-66` to construct the hub and optional sources best-effort, pass it to the client, and close it in `finally` alongside `buttons.close()`
- [ ] 3.6 Add `device/ledit_device/nfc.py`: PN532-class reader abstraction over I2C/UART/serial behind an optional import, lowercase hex UID normalization, and same-UID suppression for `LEDIT_NFC_DEDUPE_MS`
- [ ] 3.7 Add `device/ledit_device/rotary.py`: CLK/DT rotation with debounce emitting `rotate` (+1/−1) and switch emitting `press`; no-op when gpiod is unavailable
- [ ] 3.8 Add `device/ledit_device/sensors.py`: edge-only presence for PIR GPIO and mmWave (`present`/`absent`) plus I2C lux sampling at `LEDIT_LUX_INTERVAL_MS` with range filtering and change threshold
- [ ] 3.9 Add optional extras (`nfc`, `sensors`) to `device/pyproject.toml`; confirm the default install and every hardware-absent path behave exactly as today
- [ ] 3.10 Python unit tests with fake sources: event encoding and capability gating, encoder/tag/presence/lux reporting, duplicate-tag suppression, missing-library no-op, one broken source not affecting others, and hub stop on client close
- [ ] 3.11 Add a device integration test (harness in `device/tests/integration/`) covering connect → receive `inputs` capability → send a tap and a rotate event against an ephemeral server and observe dispatch

## 4. Presence, Lux & Greeting Integration

- [ ] 4.1 Add `source` (`"ha"` default, `"device"`) to `SensorConfig` (`handlers/brightness.go:29-33`) with `ValidateSensorConfig` plus parse tests confirming existing configs keep HA behavior
- [ ] 4.2 Add a bounded in-memory device-lux cache (per device id, TTL 60 s matching `SensorFetchState.IsStale`, `handlers/brightness.go:304-308`), populated from accepted `lux` input events; consult it in the `HandleDeviceWS` `bFn` and `pushBrightness.level` when `source: device` and the reading is fresh, otherwise fall back to `FetchSensorLux`/schedule
- [ ] 4.3 Tests: fresh device lux selects the mapped level, stale reading falls back to HA/schedule, manual override and alarm/scene tiers still win, push transport resolves identically, out-of-range lux never produces a level
- [ ] 4.4 Track a small per-device input registry (known sources, last event, last seen) in the dispatcher for the admin surface, bounded and cleared on disconnect like the feed registry

## 5. MQTT & Home Assistant Exposure

- [ ] 5.1 Publish each accepted input event via `PublishOutbound` (`handlers/mqtt.go:393-398`) on `ledit/device/<id>/input/<event>` with a compact bounded JSON payload; confirm the existing no-op when MQTT is disconnected
- [ ] 5.2 Extend `handlers/ha_discovery.go` with a per-device input `event` entity (`state_topic ledit/device/<id>/input/event`, `event_types` from the closed vocabulary) and republish it with the existing device discovery lifecycle
- [ ] 5.3 Tests: publish topic/payload with a stubbed controller, no-op when unconfigured, discovery payload contains the closed vocabulary and no token/secret

## 6. Admin UI, Docs & Wiring Examples

- [ ] 6.1 Add admin-session CRUD routes for input bindings in `handlers/server.go` (list/create/edit/delete/toggle) with `handlers/input_bindings.go` handlers, reusing the existing validation from 2.4
- [ ] 6.2 Add `web/templates/admin/input_bindings.html` plus form include and sidebar entry; listing shows device scope, source, event, match, action, order, enabled, with add/edit/delete and order controls
- [ ] 6.3 Show known input sources and last event on the device detail surface (`web/templates/admin/device_form.html` or devices page) without exposing tokens
- [ ] 6.4 Playwright E2E: create a binding, assert it appears in the list and can be edited/disabled, and assert an invalid action target is rejected with a visible error
- [ ] 6.5 Update `README.md` ("Push-to-Display" device paragraph) and `device/README.md` with the input env-var table, event vocabulary, binding examples (tag → scene, tag → playlist, presence → greeting, rotate → next/previous), and hardware wiring examples (NFC reader bus/address, encoder CLK/DT/SW pins with pull-ups, PIR/mmWave wiring, lux I2C address) including one worked example of each
- [ ] 6.6 Document the MQTT topics and HA event entity so automations can consume input events, and note that input publishing is a no-op when MQTT is disabled

## 7. Verification

- [ ] 7.1 Go unit tests for the protocol/ingestion/binding/dispatch/lux/greeting/MQTT paths added or extended (`handlers/device_protocol_v2_test.go`, new `handlers/input_bindings_test.go`, `handlers/input_events_test.go`, `handlers/brightness_sensor_source_test.go`) and `go test ./handlers/...`
- [ ] 7.2 Python tests: `python -m unittest discover -s device/tests -v` (or the repo's pytest task) with the new input tests and no hardware, keeping coverage above the configured threshold
- [ ] 7.3 Run `task pre-push` (gofmt, tests, build) and fix failures
- [ ] 7.4 Run `openspec validate --strict add-physical-inputs` and confirm it passes
