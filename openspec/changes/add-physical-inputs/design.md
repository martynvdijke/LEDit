## Context

LEDit is a single Go/Gin/SQLite/Ent server plus a small Python device client at `device/ledit_device/`. Devices pull frames out over an authenticated WebSocket (`HandleDeviceWS`, `handlers/websocket.go:730-745`) and negotiate an optional protocol v2 with `?protocol=2`; the server replies with `{"type":"welcome","protocol":2,"capabilities":["brightness","spectrum","hold"]}` (`handlers/websocket.go:761-771`). The device→server direction currently supports only legacy control messages (`{"action":"next"|"pause"|"resume"|"dismiss_alarm"|"hold"}` handled at `handlers/websocket.go:1140-1154`) and the opt-in spectrum tap (`{"type":"spectrum","bins":[...]}` validated by `parseSpectrumBins`, `handlers/websocket.go:1028`, ingested at `handlers/websocket.go:1129-1139`). On the device, `ButtonHandler` (`device/ledit_device/buttons.py:22`) already implements a debounced short-press/long-press state machine on `LEDIT_BTN_NEXT_PIN`/`LEDIT_BTN_PAUSE_PIN` (`buttons.py:60-61`) and is wired in `__main__.py:54-66`; the client tracks negotiated capabilities in `_handle_welcome` (`device/ledit_device/client.py:165-176`) and sends `{"type":"spectrum"}` from `_send_spectrum` (`client.py:290-297`).

Server-side effect surfaces already exist and must be reused: per-device and global `FeedController` (`handlers/feed_control.go:13-111`, device registry at `:32-57`), scene activation via `globalSceneManager`/`PreviewScene`/`resolveSceneSource` (`handlers/scenes.go:678-703,764`), rule `then_actions` via `ThenAction`/`executeThenAction` (`handlers/ruleactions.go:15-153`), brightness resolution via `ResolveEffectiveBrightnessWithScene`/`SensorLevelForLux`/`FetchSensorLux` (`handlers/brightness.go:146-223,280`) with per-connection `bFn` in `HandleDeviceWS` (`handlers/websocket.go:818-880`) and the push-transport equivalent in `feed_render.go:134-159`, greetings via `GreetingWatcher`/quiet hours/cooldown/`AddNotification` (`handlers/greetings.go:43-199`), MQTT control/outbound (`handlers/mqtt.go:111-228,339-349,393-398`), and HA discovery (`handlers/ha_discovery.go`).

Constraints: one process, SQLite, no cluster state; the device stays deliberately dumb and server-rendered; hardware and Python sensor libraries are optional and often absent; no breaking change to the WebSocket protocol or to existing v1/v2 clients; input events are authenticated by the existing per-device token; feed/scene/alarm/incident precedence must not be bypassed.

Stakeholders: homelab operators attaching buttons, readers, encoders, and sensors to panels; users tapping a tag to start a mode; HA/MQTT automations that want to observe local input.

## Goals / Non-Goals

**Goals:**
- Carry tap/button/rotation/presence/lux events from the device to the server additively over the existing authenticated v2 WebSocket.
- Let an admin map any input event to an existing action (scene, playlist, feed next/previous/pause/resume, brightness, rule, greeting) without a parallel execution path.
- Make NFC/RFID tap → scene/playlist, presence → greetings, and local lux → the existing brightness resolution work end-to-end.
- Keep every input source optional so a device with no reader/encoder/sensor runs exactly as today.
- Expose input events to MQTT/HA when those integrations are enabled.

**Non-Goals:**
- No protocol v3, no new frame keys, no handshake change, no second device endpoint; the v2 negotiation model (`?protocol=2` plus welcome capabilities) is extended only additively.
- No inbound HTTP listener or port on the device; all input traffic is outbound over the existing device WS.
- No arbitrary actuator/`control/execute` pass-through from bindings (no new outbound/SSRF surface); action targets are server-side ids only.
- No sensor history or time-series storage — only bounded in-memory last-value caches (lux), consistent with the rest of the server.
- No group-scoped bindings in v1 (per-device plus global only); no time-windowed bindings (scenes and greeting rules already carry time logic).
- No camera/vision/wake-word input; the audio spectrum tap is unchanged.
- No change to alarm/incident precedence: input actions feed the same tiers and can never outrank notifications/incidents.

## Decisions

### D1 — Extend v2 with an `inputs` capability and one additive `{"type":"input",...}` message; do not bump the protocol

The welcome gains `inputs` in its capability list; the client enables input reporting only when it sees it. The device sends at most one new message type:

```
{"type":"input","source":"button:next|button:pause|encoder|nfc|pir|mmwave|lux",
 "event":"press|rotate|tap|presence|lux","value":<int|string|number>,"ts":<epoch-ms>}
```

The server ingests these in the existing per-connection read loop (`handlers/websocket.go:1100-1155`) only when `fc.protocol >= 2`; everything else is ignored exactly like malformed spectrum frames today.

- *Why:* The codebase's compatibility contract is "v2 negotiation, additive fields/messages, v1 fallback" (`add-device-protocol-v2`); a capability gate keeps old devices and old servers byte-identical and lets one server speak to both.
- *Alternative:* Protocol `?protocol=3` — rejected: the server recognizes exactly `"2"` (`websocket.go:757-760`), a new version means another negotiation branch and another fallback matrix for no benefit.
- *Alternative:* Overload `{"action":...}` with values — rejected: `action` is a closed v1/v2 vocabulary; adding tag/level semantics would change its meaning and risk old-server confusion.
- *Alternative:* A separate `/ws/input/<token>` endpoint — rejected: duplicates token auth, connection lifecycle, and liveness accounting (`last_seen_at`) for no gain.

### D2 — Closed event vocabulary with strict validation; malformed events are dropped, never errors

`source` and `event` are enums; `value` is validated per event (NFC tag: 1-64 printable/hex chars; rotate: integer clamped to a sane step range; presence: `present`/`absent`; lux: finite number in a physical range; button press: no value). Message size is bounded by a max frame length; unknown fields ignored; invalid events are `slog.Debug`-logged and produce no action.

- *Why:* Matches `parseSpectrumBins` (`websocket.go:1028`) — untrusted device input must never tear down the feed loop; a closed vocabulary keeps logging and MQTT payloads cardinality-bounded.
- *Alternative:* Free-form JSON forwarded as-is — rejected: unvalidated strings would reach bindings and MQTT topics and defeat cardinality discipline.

### D3 — Events ride the token-authenticated device WS with a per-connection rate limit; no new listener

Input events are only accepted on `HandleDeviceWS` after the token lookup and enabled check (`websocket.go:730-745`) and only on an `inputs`-negotiated connection. The read loop applies a simple token-bucket (defaults ~20 events/s burst 40) and drops excess with a debug log. The device never opens a port.

- *Why:* Authentication, liveness, and revocation already live at this boundary; opening a second transport would create a second auth system (explicitly avoided by `add-fleet-ops` D4).
- *Alternative:* Device-signed event envelope — rejected: the WS is already token-authenticated; a second signing scheme adds key distribution without changing the trust boundary.
- *Alternative:* Server-push polling of device GPIO — rejected: requires an inbound device service and breaks the pull-only model (`device/README.md`).

### D4 — `InputBinding` ent entity: device-nullable scope, ordered first match

New `ent/schema/input_binding.go` with `device_id` (nullable; null = any device), `source` (enum), `event` (enum), `match` (text: exact tag id/prefix or empty), `action` (JSON), `enabled` (default true), `order` (default 0), `created_at`/`updated_at`. Resolution loads enabled bindings for the device plus global ones, orders by `order` then id, and executes the first match; device-specific rows sort before global rows at equal order. No match falls through to built-in defaults (D5).

- *Why:* Mirrors existing admin-managed entities and validation style; rows can be enabled/disabled/ordered without code; global rows make "all panels do X" cheap.
- *Alternative:* One JSON blob on `GeneralSettings` — rejected: no per-row enable/order, awkward validation and admin UI.
- *Alternative:* Bindings column on `DeviceSettings` — rejected: duplicates the blob problem and cannot express shared/global bindings.
- *Trade-off:* First-match semantics mean order is meaningful; the admin UI shows and validates it.

### D5 — Action execution calls the existing functions that already implement each behavior

`InputAction` is a small JSON struct in the `ThenAction` style (`handlers/ruleactions.go:15`) with kinds `scene|playlist|feed|brightness|rule|greeting|notification`:

| Kind | Convergence point |
| --- | --- |
| `scene` | load `Scene`, `sceneFromEnt`, `PreviewScene(..., resolveSceneSource, ttl)` — same as rule scene actions (`ruleactions.go:98-120`) |
| `playlist` | set `DeviceSettings.playlist_id` + `content_mode=playlist`, then request a live source reload (D8); `RestartTransportDevice` (`output_runner.go:53`) for non-WebSocket transports |
| `feed` | `getDeviceFeed(deviceID)` when connected, else `GlobalFeed`; verbs `next`, `previous`, `pause`, `resume`, `toggle` |
| `brightness` | clamp 0-100; set the live override consulted by the connection's brightness `bFn` and persist `brightness_override` (`ent/schema/device_settings.go:46`); `RestartTransportDevice` for pushes |
| `rule` | load `DisplayRule`, `executeThenAction` (`ruleactions.go:77`) — fires the rule's existing `then_actions` |
| `greeting` | load `GreetingRule`, apply quiet hours/cooldown/`ResolveTemplate`, call `server.AddNotification` — same code path as the watcher (`greetings.go:110-199`) |
| `notification` | `server.AddNotification(title, message, WithTTL(...))` |

Built-in defaults apply only when no binding matches: `button:next` press → `feed next`, `button:pause` press → `feed pause`, `hold` → unchanged no-op, `encoder` rotate +1/−1 → `feed next`/`feed previous`, `encoder` press → brightness cycle (default presets 25/50/75/100). Existing legacy `{"action":...}` handling in `websocket.go:1140-1154` is untouched so a v2 device without the new client code behaves exactly as today.

- *Why:* "Converge, not duplicate" — every action is a thin lookup plus a call into the module that owns the semantics, so precedence, observability, and state restoration stay in one place.
- *Alternative:* A binding-specific action interpreter — rejected: would drift from feed/scene/brightness behavior and duplicate precedence and cleanup logic.
- *Alternative:* Reuse `ThenAction` verbatim — rejected: its kinds (`scene|notification|webhook`) cannot express playlist/feed/brightness/greeting; a new struct with aligned style is clearer than overloading it.

### D6 — One device-side input hub with best-effort sources and capability gating

`device/ledit_device/inputs.py` defines the event encoding and an `InputHub` that owns optional source objects (`nfc.py`, `rotary.py`, `sensors.py`) and a `sender(str)` callback. Each source is constructed in a `try/except` for missing imports/pins, logs one informative line, and then stays inert — the exact pattern of `ButtonHandler.start()` (`buttons.py:187-221`). The hub activates when `_handle_welcome` reports the `inputs` capability; until then, buttons keep sending legacy actions. `buttons.py` keeps its gesture state machine and routes callbacks through the hub when active.

- *Why:* Optional hardware is the norm; a per-source degrade keeps one broken sensor from affecting rendering or other inputs, and the capability gate preserves the v1/v2 fallback contract.
- *Alternative:* Import sensors eagerly — rejected: breaks `pip install ledit` on machines without I2C/GPIO libraries.
- *Alternative:* Shell out to external daemons — rejected: extra processes and assumptions for a device the project deliberately keeps simple.

### D7 — NFC/RFID, rotary, presence, and lux device semantics

- `nfc.py`: reader abstraction for a PN532-class reader over I2C/UART/serial (`nfcpy`/`pyserial` optional); normalizes a UID to lowercase hex and emits `{"event":"tap","source":"nfc","value":"<uid>"}`. The same UID within `LEDIT_NFC_DEDUPE_MS` (default 30000) is suppressed.
- `rotary.py`: CLK/DT polling or interrupt counting (`gpiod` optional) emits `{"event":"rotate","source":"encoder","value":+1|-1}` with `LEDIT_ENCODER_DEBOUNCE_MS`; the switch emits a `press`.
- `sensors.py`: PIR GPIO (`LEDIT_PRESENCE_PIN`) or mmWave (`LEDIT_PRESENCE_KIND=mmwave`) emits `presence` only on edges (`present`/`absent`); a BH1750/TSL2591-class I2C lux sensor (`smbus2` optional) emits `lux` at `LEDIT_LUX_INTERVAL_MS` (default 30000) and only when the reading changed meaningfully.
- *Why:* The event names and value shapes are exactly what D2 validates, and edge-only reporting keeps traffic low.
- *Alternative:* Report level-triggered presence/lux every tick — rejected: wakes the server and bindings constantly for no new information.

### D8 — Playlist switches take effect on the live feed through the existing reschedule plumbing

`reschedule` is currently built only for scheduled devices (`websocket.go:887-912`) and polled at a 60 s cadence (`websocket.go:1215-1217`). Extend it to any live device feed, and add a per-feed `reload` request set by the dispatcher after a playlist action; the feed loop consumes it at the rotation boundary and swaps in the freshly resolved sources from the same closure (`composeDeviceSources`). Non-WebSocket transports use `RestartTransportDevice`.

- *Why:* Reuses the tested source-resolution path instead of re-implementing playlist resolution in the dispatcher; the device never needs a reconnect.
- *Alternative:* Force a WebSocket reconnect — rejected: jarring and slow.
- *Alternative:* Dispatcher resolves the playlist itself and injects a pin — rejected: duplicates `tryComposePlaylist`/`resolveScheduledPlaylist` semantics (`websocket.go:453,496`).

### D9 — Local lux joins the existing brightness resolution as a second sensor source, with the same staleness rule

`SensorConfig` (`handlers/brightness.go:30`) gains an optional `source` field (`"ha"` default = today; `"device"` = use the device's own lux readings). A bounded in-memory cache keyed by device id stores `{lux, at}`; `"device"` readings are used when fresh (≤60 s, matching `SensorFetchState.IsStale`, `brightness.go:304-308`) and mapped with `SensorLevelForLux` (`brightness.go:212`); when stale or absent the resolution falls back to the HA fetch and schedule as today. Precedence (`ResolveEffectiveBrightnessWithScene`, `brightness.go:184`) is unchanged. The live per-device brightness override (D5) layers in the existing override tier, so manual override still wins.

- *Why:* The resolver already models "fresh sensor reading vs stale vs schedule"; feeding it a local reading is a source change, not a new pipeline, and keeps HA users unaffected.
- *Alternative:* Replace the HA sensor with device lux unconditionally — rejected: users with a room-level lux sensor would lose it.
- *Alternative:* Auto-pick local whenever fresh — rejected: an explicit `source` makes behavior predictable and lets the admin opt in.

### D10 — Presence drives greetings through the greeting-rule machinery, not a second watcher

Presence events are edges by construction (D7). A `greeting` binding references a `GreetingRule`; execution reuses the watcher's quiet-hours check, persisted/in-memory cooldown, and template resolution (`greetings.go:110-199`), then calls `server.AddNotification`. Existing `entity_path`-based HA rules are untouched, and presence can also use any other binding action (scene, notification, brightness).

- *Why:* No duplicate cooldown/quiet-hours/template logic; the wall shows presence greetings exactly like HA-driven ones.
- *Alternative:* Inject synthetic HA states — rejected: requires HA configured and corrupts the HA watcher's edge state.
- *Alternative:* New local-presence watcher — rejected: duplicate logic and a second source of truth.

### D11 — MQTT/HA exposure reuses the existing outbound helpers and stays a no-op when disabled

After dispatch, the event is published as `ledit/device/<id>/input/<event>` with a compact JSON payload via `PublishOutbound` (`handlers/mqtt.go:393-398`, already a no-op when MQTT is unconfigured). HA discovery (`handlers/ha_discovery.go`) adds an `event` entity per device with `state_topic ledit/device/<id>/input/event` and `event_types` from the closed vocabulary. No new subscription or broker dependency.

- *Why:* Matches the existing `ledit/device/<id>/*` topic layout and discovery lifecycle (republish on connect/save); automations can trigger on the event entity.
- *Alternative:* A bespoke topic hierarchy/schema — rejected: inconsistency for no gain.
- *Alternative:* Always retain event state — rejected: inputs are momentary; retain only as the last-event convenience already used for HA state.

### D12 — Security posture: events are untrusted data; actions are server-resolved

Validation (D2) makes payloads inert data. Bindings resolve targets server-side by id (scene/rule/greeting/playlist) and reject missing/disabled targets at save time and at execution time. Admin routes are session-authenticated like the rest of `/admin` and `/api` (`handlers/server.go:770+`). Logging and MQTT payloads contain enum values, device ids, and tag ids only — never tokens.

- *Why:* Keeps the trust boundary at the device token and prevents input payloads from becoming a command surface.
- *Alternative:* Allow bindings to carry arbitrary HTTP/actuator params — rejected for v1 (SSRF/abuse surface; explicit non-goal).

### D13 — Additive data model and inert-by-default rollout

The only schema addition is the `InputBinding` table; lux and live-override state are in-memory per process; `SensorConfig.source` is additive JSON. With no bindings configured, only the legacy button behaviors remain (which are unchanged) and new sources are simply logged. Rollback is disabling/deleting bindings and unsetting device env vars; the table can remain empty.

- *Why:* Zero-migration risk for a running homelab server; feature discovery is explicit.
- *Alternative:* Seed default bindings — rejected: surprises existing installs; defaults live in code (D5) and only apply after an input event arrives.

## Risks / Trade-offs

- [Noisy GPIO bounce or a flapping PIR floods the WS] → Device-side debounce and edge-only presence (`sensors.py`), per-connection token-bucket (D3), and server debug-drop; the feed loop is never blocked by input handling.
- [Duplicate NFC reads fire a scene twice] → Per-device UID dedupe window on the device (default 30 s) plus first-match single execution server-side; repeated taps while a scene holds are still bounded by scene TTL/precedence.
- [A binding fights an alarm, incident, or notification] → Actions converge on the existing tiers (D5) and never bypass precedence; scenes/alarms/incidents keep their current precedence rules.
- [Local lux disagrees with an HA lux sensor] → Explicit `source` (`ha` default) and the same 60 s freshness rule; manual override and alarms/scenes still win (D9).
- [Playlist switch lands up to a rotation late] → Reload flag consumed at the next rotation boundary (D8); acceptable for tap-to-start flows and avoids tearing a frame mid-render.
- [Encoder "previous" semantics change feed behavior] → `Prev` is only reachable through an encoder/binding; `Next`/`Pause`/`Resume` are untouched, and default mappings are tested against existing button tests.
- [Optional libraries missing on the Pi] → Every source degrades to a one-line log and no-op (D6), matching `ButtonHandler.start()`; unit tests inject fakes so CI needs no hardware.
- [MQTT/HA exposure surprises operators] → Publishing is a no-op when MQTT is off and adds only an event entity when HA discovery is on; payloads stay bounded (D11).
- [In-memory lux/override state lost on restart] → Acceptable for a single-instance homelab server; persisted `brightness_override` and the next lux report restore behavior within seconds of reconnect.
- [Binding targets deleted after configuration] → Execution re-resolves and skips with a warning; the admin UI validates on save.

## Migration Plan

1. Add `ent/schema/input_binding.go` and run `go generate ./ent`; auto-migration only adds a table, so an existing database is untouched with zero bindings.
2. Add the `inputs` welcome capability and `{"type":"input",...}` ingestion/validation in `handlers/websocket.go`; deploy the server first — v1 devices and installed v2 clients are unaffected (they simply never see `inputs`).
3. Add the binding dispatcher and action executors (`handlers/input_bindings.go` + action helpers), wired to existing FeedController/scene/rule/greeting/brightness functions; ship with an empty binding table and built-in defaults only for the inputs the device actually sends.
4. Add local lux (`SensorConfig.source=device`, in-memory cache, brightness `bFn` consultation) and the greeting action; both are inert until configured.
5. Add MQTT/HA exposure (no-op when MQTT is disabled) and the admin CRUD page/templates; document the event vocabulary and hardware wiring in `README.md` and `device/README.md`.
6. Ship the device client with the optional input modules and env vars; buttons keep legacy behavior until the server advertises `inputs`. Bump the client version as an OTA release.
7. Rollback: disable or delete bindings (events become debug-logged, legacy buttons still work), unset the new device env vars, and leave the additive table in place; no protocol or frame changes need reverting.

## Open Questions

- Binding scope: per-device plus global in v1, or per-device-group as well? Assumed per-device + global; group scope is a follow-up.
- Rotary "previous" semantics: step the current rotation list backwards (wrap) or keep a bounded history? Assumed stepping the list backwards with wrap, adding only a `Prev` flag.
- Should local lux require an explicit `source: device`, or be automatic whenever a fresh device reading exists? Assumed explicit (default `ha`) so existing HA-driven brightness is unchanged.
- Encoder press default: brightness cycle presets or pause/resume toggle? Assumed brightness cycle (25/50/75/100), fully overridable by a binding.
- Should input events be exposed on MQTT whenever the broker is connected, or behind a setting? Assumed always-on when connected, matching per-device state publishes.
- Should bindings support day/time windows? Assumed no in v1 — scenes and greeting rules already carry time logic.
