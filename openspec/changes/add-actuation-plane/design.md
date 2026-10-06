## Context

LEDit already has most of the machinery an actuation plane needs, just not connected. Home Assistant discovery is hand-rolled: `publishHADiscoveryForDevice` (`handlers/ha_discovery.go:23`) publishes six per-device configs (`binary_sensor` online, `number` brightness, `switch` paused, `button` next, `sensor` firmware, `sensor` transport) and `publishGlobalConfigs` (`handlers/ha_discovery.go:133`) adds three globals (`current_source`, `paused`, `next`); the package comment at `handlers/ha_discovery.go:10` records the two known limitations — brightness state is override-or-100 without schedule/sensor resolution, and discovery is republished only on connect/device-save, not on state change. Publication is gated by `outbound_settings.ha_discovery_enabled` (`haDiscoveryEnabled`, `handlers/ha_discovery.go:12`) and lands through `PublishOutbound` (`handlers/mqtt.go:393`), which is retained QoS 0 and connected-only.

Inbound MQTT lives in `MQTTController` (`handlers/mqtt.go:17`). `subscribeAll` (`handlers/mqtt.go:111`) subscribes the configured control topic, display topic, optional NL topic, and three per-device wildcard topics (`brightness/set`, `paused/set`, `next/set`); `handlePerDeviceCommand` (`handlers/mqtt.go:162`) parses the device id and suffix. Pause/resume/next already resolve the per-device `FeedController` via `getDeviceFeed` (`handlers/feed_control.go:52`) and call `Pause`/`Resume`/`Next`; brightness updates `DeviceSettings.BrightnessOverride` and calls `RestartTransportDevice` (`handlers/output_runner.go:53`), which is a no-op for `transport == "websocket"`. The control topic maps payloads through `HandleControlPayload` (`handlers/mqtt.go:339`) to `GlobalFeed`; the display topic pushes a global notification via `AddNotification` (`handlers/mqtt.go:360`, `handlers/feed_control.go:479`). Notifications are process-global: `NotificationsAfter(cursor)` (`handlers/feed_control.go:455`) is consumed by every feed in `serveFeed` (`handlers/websocket.go:1224`), and they outrank incidents, alarms, scenes, and pins.

Feed precedence is notifications > incident > alarm > scene > pinned rule > rotation (`handlers/websocket.go:1242-1286`, `selectTierSource`, `handlers/scenes.go:669`). Pins are single-slot per controller (`FeedController.Pin`, `handlers/feed_control.go:151`) and are set process-wide by `pinAll` (`handlers/eventrules.go:45`); `Next` clears the pin (`handlers/feed_control.go:91`). Effective brightness is `ResolveEffectiveBrightnessWithScene` (`handlers/brightness.go:184`): manual override > alarm ramp > scene > lux sensor > schedule > 100. `effectiveBrightnessConfig` (`handlers/effective_policy.go:53`) layers device < group. `Pause` calls `SuppressActiveScene` (D6: a manual action reclaims the wall from ambient automation, `handlers/feed_control.go:75`), and the admin device-save path does the same for a brightness override (`handlers/handlers.go:1780`).

Event rules evaluate a condition on a state-capable source and, on a rising edge, call `pinAll` then `executeThenAction` inside a 5 s context (`handlers/eventrules.go:459-466`). `ThenAction` (`handlers/ruleactions.go:15`) is a small struct serialized into the `display_rules.then_actions` text column; `ParseThenAction` (`handlers/ruleactions.go:26`) accepts `none|scene|notification|webhook` and `validateThenAction` (`handlers/ruleactions.go:47`) checks per-kind fields. The admin form posts flat fields consumed by `buildThenJSON` (`handlers/eventrules.go:630`) and re-rendered by `eventRuleFormVarsWithThen` (`handlers/eventrules.go:605`). Outbound webhooks already sign with HMAC-SHA256 and set `X-LEDit-Signature: sha256=<hex>` (`handlers/outbound.go:368`); delivery retry/backoff and the delivery log live in `WebhookSink.sendToTarget` (`handlers/outbound.go:359`). A single MQTT broker connection is shared by `PublishOutbound`; there is no inbound HTTP surface for device commands.

Constraints: one process, SQLite, no cluster state; MQTT is optional and can be disconnected at any time; the device is deliberately dumb and server-rendered; existing HA entities, MQTT topics, and stored rule JSON must not break. Stakeholders: homelab operators wiring LEDit into Home Assistant automations and administrators authoring event rules.

## Goals / Non-Goals

**Goals:**
- Expose per-device brightness as a real HA light, plus select (source/playlist/scene) and text (message) entities, additively and byte-compatibly with today's entities.
- Republish HA configs and retained entity state on the events that change them, gated by `ha_discovery_enabled`, without polling the wall or the broker.
- Accept inbound MQTT brightness/select/message commands and route them to the same per-device `FeedController` the WS and push transports use, with offline-safe no-ops.
- Expand rule then-actions with brightness, source pin, playlist switch, MQTT publish, and signed outbound HTTP while keeping the four existing kinds and stored rules unchanged.
- Keep every actuation path subject to existing precedence (notifications > pinned rule > rotation) and effective brightness resolution.

**Non-Goals:**
- No new HTTP API routes or device-facing endpoints; actuation stays admin/MQTT-configured.
- No new authentication layer for MQTT commands — broker ACLs remain the authority (unchanged trust model).
- No `media_player` now-playing entity in v1: `datasource/nowplaying` tracks playback state (`CurrentNowPlaying`, `datasource/nowplaying/nowplaying.go:460`) but has no stable per-device HA state contract yet.
- No color/effect support on the light in v1; scene selection on the `select` entity covers ambient effects.
- No removal or deprecation of the existing `number` brightness entity, global entities, or any MQTT topic.
- No new persistence model for actuation: no new ent schemas, no rule-execution history table.
- No arbitrary-source takeover tier: `source:` selection uses the existing pin slot and therefore only renders sources already in the device's rotation.

## Decisions

### D1 — Three new capabilities, zero modified deltas

`homeassistant-control-entities`, `mqtt-actuation`, and `rule-action-expansion` are all ADDED. No tracked spec covers HA discovery, MQTT commands, or then-actions: `mqtt-control` (`openspec/changes/push-to-display/specs/mqtt-control/spec.md`) and `event-driven-switching` (`openspec/changes/add-event-driven-switching/specs/event-driven-switching/spec.md`) are unarchived deltas, and `device-management`/`outbound-alerting`/`api-authentication` requirements are not contradicted by anything here. Writing new capabilities avoids a delta-spec collision if those changes archive first, and matches the `add-fleet-ops` precedent.

- *Why:* Each capability is independently applicable and testable: the HA contract can ship without rule actions, and rule actions can ship with MQTT/HA disabled.
- *Alternative:* Modify `device-management` for the light entity — rejected: the spec says nothing about HA, and modifying an unrelated tracked capability would imply a requirement change that does not exist.
- *Alternative:* One `actuation-plane` capability — rejected: specs would be huge and the proposal/spec mapping would not match the natural seams.

### D2 — One shared actuation layer in `handlers/actuation.go`

All three surfaces (MQTT commands, HA entity commands, rule then-actions) call the same helpers:
- `ApplyDeviceBrightness(deviceID, level, source)` — clamps 0–100, writes `DeviceSettings.BrightnessOverride`, sets the live controller hint (D7), suppresses the active scene like a manual override, restarts push transports, and publishes HA brightness state.
- `ApplyDeviceSelect(deviceID, value)` — parses the `source:`/`playlist:`/`scene:` grammar and dispatches to pin/content-mode/scene application (D4).
- `ApplyDeviceMessage(deviceID, text)` — creates a device-targeted notification through the existing notification queue (D5).

- *Why:* The root-cause rule applies: the MQTT command path, the HA entity path, and the rule action path are three callers of one behavior; fixing it once in the shared helper means all three get validation, clamping, state publishing, and offline safety for free. It also directly satisfies "every command path must land on the same feed controller".
- *Alternative:* Duplicate logic per surface — rejected: three places to drift on brightness clamping, pin semantics, and message TTL.
- *Alternative:* Route rule actions through the MQTT broker (publish to `ledit/...` and let the subscriber act) — rejected: adds a broker round-trip and a configuration dependency to a local action, and breaks when MQTT is disabled.

### D3 — HA entity surface is additive: keep the `number`, add a `light`, `select`, and `text`

New per-device discovery configs:
- `homeassistant/light/ledit_<id>_light/config` — template light, unique_id `ledit_<id>_light`, `state_topic` = `ledit/device/<id>/brightness/state`, `command_topic` = `ledit/device/<id>/brightness/set`, `brightness_scale: 100`, availability on `ledit/device/<id>/online`. Off sends `0`; on sends `100` (or the supplied brightness); state renders `on` when level > 0.
- `homeassistant/select/ledit_<id>_content/config` — options from the device's current rotation plus enabled playlists plus enabled scenes (D4), `command_topic` = `ledit/device/<id>/select/set`, `state_topic` = `ledit/device/<id>/select/state`.
- `homeassistant/text/ledit_<id>_message/config` — `command_topic` = `ledit/device/<id>/message/set`, `state_topic` = `ledit/device/<id>/message/state`, max length 255.

The existing six per-device and three global configs keep their exact topics, unique IDs, and payload semantics; `clearHADiscoveryForDevice` (`handlers/ha_discovery.go:197`) is extended with the three new config topics and the new state topics.

- *Why:* HA users expect a light for a dimmable wall, and `unique_id` stability means the new entities coexist with the old ones instead of HA deleting and recreating them. Reusing the brightness state/set topics keeps retriggers consistent between the number and the light.
- *Alternative:* Replace the `number` with the `light` — rejected: breaking (entity removal, automations referencing `number.ledit_*_brightness` break).
- *Alternative:* HA legacy `schema: json` light — rejected: HA deprecated the JSON schema; template is the supported path for a brightness-only light.
- *Alternative:* `command_topic` on a new `ledit/device/<id>/light/set` topic — rejected: it would create a second brightness path, and "every command path must land on the same feed controller" argues for one topic accepting 0–100.

### D4 — The `select` entity is a bounded content picker with a stable machine-readable grammar, refreshed on config change

Option values are exactly `source:<type>:<id>` (one per source in the device's active rotation, using the same `<type>:<id>` key as `cacheKey`), `playlist:<id>` (enabled playlists), and `scene:<id>` (enabled scenes). Application:
- `source:` → `FeedController.Pin("<type>:<id>", "select")` on the target device's controller only. Because `serveFeed` only honours a pin whose key exists in that connection's source list (`handlers/websocket.go:1277-1285`), a source outside the rotation is a logged no-op with no controller state change.
- `playlist:` → persist `ContentMode="playlist"` + `PlaylistID` mirroring the admin update path (`handlers/handlers.go:1761-1783`), then `RestartTransportDevice` for push transports; WS connections pick it up through the extended reschedule refresh (D7).
- `scene:` → `PreviewScene(scene, resolveSceneSource, hold)` (`handlers/scenes.go:701`) with the scene's `TTLSeconds` when set, else 60 s (the existing then-action fallback, `handlers/ruleactions.go:113-116`).

Select configs are republished on MQTT connect, device save/delete, and admin changes to playlists or scenes (create/update/delete/enable), so HA's option list tracks the config; state is the last applied value.

- *Why:* One grammar serves HA, raw MQTT clients, and rule actions, and keeps the HA option list bounded by admin-configured entities rather than traffic. Pin/content-mode/preview are the existing semantics for "show this", so no new tier is invented.
- *Alternative:* A `text` entity with free-form input instead of `select` — rejected as the primary: HA users get no picker; the `select/set` topic still accepts the same grammar for scripted clients.
- *Alternative:* A new "selected source" tier on `FeedController` that can render sources outside the rotation — rejected for v1: new precedence semantics and render-path special-casing for a use case (pinning an off-rotation source) that is better solved by adding it to the playlist.
- *Alternative:* Bundle options from groups or all sources — rejected: options would explode and mean nothing for the target device.

### D5 — The message entity targets one device by riding the existing notification tier

`ledit/device/<id>/message/set` creates a notification via a new internal option on the existing queue (`withTargetDevice(id)` on `notifEntry`, `handlers/feed_control.go:340`). `serveFeed` advances its cursor over all notifications but only renders ones whose target is 0 (broadcast) or its own `fc.deviceID`; the global display/control topic keeps target 0. State (`ledit/device/<id>/message/state`, retained) holds the last message text and is cleared when that message expires or resolves; the `ledit/message/<id>/*` and `ledit/messages/active` lifecycle topics (`handlers/outbound.go:236`) are unchanged and still carry the message.

- *Why:* Per-device messages are a filter on the existing notification model — same TTL, history, delivery-log, `emitMessageFired`, and message-id namespace — instead of a parallel message system. Precedence stays "notifications > pinned rule > rotation"; a targeted message is still a notification.
- *Alternative:* A new per-controller message override on `FeedController` — rejected: duplicates TTL/resolve/lifecycle machinery and invents a precedence question.
- *Alternative:* Accept per-device topics but broadcast globally — rejected as misleading: HA would claim a device-scoped message while every wall showed it.
- *Risk:* The notification cursor is per connection and advances even over skipped targets, so a device that was offline when a targeted message fired will not see it on reconnect. This matches existing notification semantics (notifications are transient) and is accepted.
- *Implementation note:* `UnreadMessagesFor` (`handlers/messages.go:323`) and the notification admin history already filter per device; targeted messages appear there naturally.

### D6 — HA state republish is event-driven through a dedicated HA sink, gated by `ha_discovery_enabled`

A `HAStateSink` registered on `GlobalBus` alongside `GlobalMqttSink` (`handlers/outbound.go:543`) publishes retained HA entity state when `haDiscoveryEnabled(s)` is true: `EventDeviceLivenessChanged` → `online`, `EventFeedPaused`/`EventFeedResumed` → global `ledit/status/paused` and per-device `paused`, `EventSourceChanged` → `ledit/status/current_source`, brightness apply → `brightness/state`, select apply → `select/state`, message fired/expired → `message/state`. Configs and state are also republished together by `PublishHADiscoveryForAll` on connect and by `publishHADiscoveryForDevice` on device save. The last value per topic is cached to suppress duplicate retained publishes. This addresses the limitation at `handlers/ha_discovery.go:10` without polling.

Crucially, HA entity state is independent of `outbound_settings.mqtt_publish_enabled`: that toggle governs the generic event fan-out (`MqttSink`, `handlers/outbound.go:132`), while HA entities must stay coherent when discovery is on. When both are on, duplicate retained publishes of the same value are harmless and deduped by the sink cache.

- *Why:* The bus already carries every state transition the entities need; a sink keeps discovery code and state code together and gated by the same setting.
- *Alternative:* Extend `MqttSink` and require `mqtt_publish_enabled` too — rejected: operators enabling HA discovery would silently lose entity state when they have not enabled generic MQTT event publishing.
- *Alternative:* Re-read the DB periodically and diff state — rejected: polling cost and latency for events the process already emits synchronously.
- *Risk:* A state event fired before the MQTT connection is up is lost. The connect republish path re-publishes all current state, so the retained values converge on reconnect.

### D7 — Brightness reaches live feeds through a controller hint; content-mode switches through the existing reschedule refresh

`FeedController` gains an ephemeral `Brightness int` hint (with getter/setter under the existing mutex). `ApplyDeviceBrightness` writes the durable `brightness_override` and sets the hint on the target device's registered controller (`getDeviceFeed`); a rule action with `device_id = 0` sets it on every registered device controller. The WS `bFn` closure (`handlers/websocket.go:844-880`) consults the hint as the top-precedence manual override before the DB-derived override, so a command dims a live WS connection immediately instead of waiting for reconnect. Push transports restart with the persisted value via `RestartTransportDevice` (`handlers/output_runner.go:53`).

For content-mode/playlist changes, the WS connection's existing `reschedule` closure (`handlers/websocket.go:888-912`) is created for every device connection (not only `scheduled`), re-reading the device row at the existing 60 s throttle and replacing `sources` when they differ; a playlist switch therefore applies within one rotation boundary, while push transports restart immediately. `sameSources` (`handlers/websocket.go:676`) prevents churn.

- *Why:* The process already has a per-device controller registry (`registerDeviceFeed`, `handlers/feed_control.go:32`) and a reschedule seam; using both gives immediate brightness and ≤60 s content switches with no new connection registry and no forced reconnects.
- *Alternative:* Close the device WS to force a reconnect — rejected: disruptive, thundering-herd on reconnect, and it would drop notifications.
- *Alternative:* Re-read brightness from the DB every frame — rejected: per-frame DB cost for a value that changes rarely.
- *Risk:* A hint applied to a controller that disconnects before the DB write is lost; the DB write is the durable source and the next connection re-derives from it.

### D8 — `ThenAction` grows optional fields; parsing and validation stay strict but backward compatible

New optional JSON fields: `device_id` (int, 0 = all), `level` (int 0–100), `source_type`/`source_id`, `playlist_id`, `topic`/`payload`/`retain` (MQTT), `url`/`method`/`secret` (HTTP; `event` and `payload` reused). `ParseThenAction` keeps accepting the current four kinds and `{}`; new kinds join the accepted set. `validateThenAction` gains per-kind checks: level range, device/playlist existence is checked at execution (DB), `topic` non-empty and free of `+`/`#` wildcards, `url` parseable with `http`/`https` scheme, payload valid JSON object where required. `executeThenAction` gets new cases that call the shared helpers; unknown kinds remain rejected at parse time.

- *Why:* The column is free-form JSON, so additive fields need no migration; strict validation at save time keeps broken rules out of the DB, and runtime checks (device/playlist missing) fail open.
- *Alternative:* A new `ThenActionV2` envelope or a kind registry table — rejected: breaking for stored rules and unnecessary for five new kinds.
- *Alternative:* Validate device/playlist existence only at execution — rejected: admins would get no feedback; validate at save when cheap, ignore at execution when the target has since disappeared.

### D9 — Brightness action is a durable manual override with immediacy

`brightness` requires `level` (0–100); `device_id` 0 applies to every enabled device (including global controls), otherwise one device. Execution calls `ApplyDeviceBrightness`, which reuses the manual-override precedence and the D6 "manual override reclaims the wall from ambient automation" behavior (`SuppressActiveScene`, as at `handlers/handlers.go:1780`). A missing device is a logged no-op. Level is clamped defensively at execution even though validation rejects out-of-range saves.

- *Why:* HA light and rule brightness must mean the same thing as the admin override, or precedence becomes unexplainable; persisting it also survives restarts.
- *Alternative:* Transient-only brightness (until next rotation) — rejected: surprising for HA users and inconsistent with how `brightness_override` works today.
- *Risk:* Rule actions overwrite an admin's manual override. Accepted: the rule is admin-authored config; last writer wins and the state topic/admin form show the effective value.

### D10 — Source pin action uses the single pin slot with rule attribution

`source` requires `source_type` and `source_id`; execution resolves to `<type>:<id>` and calls `pinAll(key, "rule:"+rule.Name)` (the existing controller fan-out, `handlers/eventrules.go:45`). It obeys every existing pin rule: `Next` clears it, controllers where the source is not in rotation ignore it, and a later notification/scene/alarm outranks it. It does not release the condition pin of the firing rule; if both apply, the pinned key is the last one written and the rule evaluator's own pin lifecycle still governs the rule target.

- *Why:* A pin is the only existing mechanism that holds a source without touching content config; reusing it means skip/pause/notification semantics stay uniform.
- *Alternative:* Persist a per-device `pinned source` setting — rejected: a new durable state with new precedence questions for a transient "show this" intent.
- *Risk:* Pin single-slot collisions between a condition pin and a `source` action are possible. Documented behavior: the then-action is applied after the condition pin, so it wins until the next skip or evaluator release; the admin rule-simulate endpoint (`APIEventRuleSimulate`, `handlers/eventrules.go:670`) helps authors see this.

### D11 — Playlist action mirrors the admin content update and validates targets

`playlist` requires `device_id` (nonzero) and `playlist_id`; execution loads the playlist, rejects unknown/disabled ones with a log, then persists `ContentMode="playlist"` + `PlaylistID` exactly like the admin update path (`handlers/handlers.go:1761-1783`) and restarts push transports. WS connections apply at the next rotation boundary via D7's reschedule.

- *Why:* One content-switch implementation means device forms, HA select, and rules cannot disagree about what `playlist` mode means (including the existing fallback behavior when a playlist is missing).
- *Alternative:* A transient playlist override — rejected: the device already has exactly one durable content model and scheduled mode would fight an ephemeral one.
- *Risk:* An automation switching a device's playlist changes durable config. Accepted and visible in the device form; the rule name is not persisted (no schema change), so attribution is via logs and the MQTT/HA state.

### D12 — MQTT publish action is fire-and-forget through `PublishOutbound`

`mqtt` requires `topic`; `payload` defaults to the rule-fired envelope when empty; `retain` defaults false. Execution validates the topic (non-empty, no `+`/`#`, no leading `$`, ≤ 256 bytes), caps payload at 4 KB, and calls `PublishOutbound` (`handlers/mqtt.go:393`). When MQTT is unconfigured/disconnected the action logs at debug and returns — the rule is still considered fired.

- *Why:* Publishing is already connected-only, non-blocking, and retained-capable; a rule action should not invent retry or a delivery log for fire-and-forget publishes.
- *Alternative:* Route through `GlobalMqttSink`/`GlobalDispatcher` — rejected: those are event-type fan-outs, not arbitrary-topic publishing.
- *Alternative:* Reject wildcard topics only at execution — rejected: validate at save too so admins get immediate feedback.
- *Risk:* Rules can publish anywhere the broker allows, including `homeassistant/...`. The broker ACL is the boundary; spec documents that no additional topic allowlist is imposed in v1 (same trust as a broker client).

### D13 — Signed outbound HTTP action reuses the webhook signing convention, dispatched asynchronously

`http` requires `url` (http/https) and supports `method` (default POST; GET/PUT), `secret` (optional), `event` (reused), and `payload` (JSON object, reused). The body is a JSON envelope `{event, timestamp, data:{rule, ...}}`; when `secret` is set, the request carries `X-LEDit-Signature: sha256=<hex>` over the raw body and `X-LEDit-Event`, matching the existing outbound convention (`handlers/outbound.go:359-392`). Execution must not block the evaluator: the action enqueues a bounded async delivery that reuses the existing retry/backoff policy (1 s/5 s/25 s, 4xx stop) and records status in the delivery log; the evaluator's 5 s action context only covers enqueueing.

- *Why:* Two signing implementations in one process would inevitably diverge; the existing outbound sender is already tested and records deliveries. Async dispatch protects the rule evaluator from a slow endpoint.
- *Alternative:* Synchronous call inside `executeThenAction` — rejected: a dead endpoint would consume the 5 s action context and delay rule evaluation/pinning.
- *Alternative:* Force rules to reference `OutboundWebhook` targets — rejected: rule-scoped URL/secret is the point, and coupling rule intent to unrelated alerting targets is confusing; an admin who wants shared targets can still use the existing `webhook` kind.
- *Risk:* SSRF reachability for admin-authored URLs. Admin-only configuration, parity with existing `OutboundWebhook` targets, and scheme validation are the accepted v1 boundary; secrets are never logged and are omitted from read APIs (`ThenActions` is only rendered in the admin edit form).

### D14 — Every new path is fail-open and failure-isolated

`ApplyDevice*` helpers and every new `executeThenAction` case resolve targets defensively: unknown device/playlist/scene/source logs a warning and returns; notification/brightness/controller operations cannot panic on nil DB/controller; the `http` action records a failed delivery instead of returning an error; the `mqtt` action no-ops when disconnected. The evaluator's existing behavior (dangerous deliverables are best-effort, `rs.pinned` state unaffected by action failure) is preserved.

- *Why:* Actuation must never be able to blank or stall the wall, matching the existing evaluator rule that failures never block or blank any feed.
- *Alternative:* Propagate action errors into the evaluator and retry the rising edge — rejected: the pin already fired; repeated side effects (multiple HTTP calls) are worse than a dropped action.

## Risks / Trade-offs

- [HA select options go stale] Playlist/scene/device changes republish select configs, and configs are republished on connect → worst case a user sees stale options for one connection; choosing a removed option is a logged no-op.
- [Retained topic growth] Three new entity state topics per device plus configs → one bounded topic per entity per device, cleared on device delete by the extended `clearHADiscoveryForDevice`; message state cleared on expiry/resolve.
- [Broker ACL is the only command authority] A broker client can now select content and message devices, not just pause/skip → same trust boundary as today's brightness/pause topics; documented as a non-goal to add a second auth layer.
- [Rule actions persist device config] Brightness overrides and playlist switches are durable and can overwrite admin edits → last-writer-wins is documented; HA state topics reflect the effective value; manual skip/pause still work.
- [Pin collisions] A `source` action and the rule's condition pin share one slot → then-actions are applied after `pinAll`, so they win; `Next` clears everything; the decide-in-design note is in D10.
- [HTTP action SSRF / slow endpoints] Admin-only URL config, http/https only, async delivery with existing retry policy and timeout → a hostile endpoint cannot stall rule evaluation or expose secrets in logs.
- [Targeted messages lost while a device is offline] Notifications are transient and the cursor advances → matches existing notification behavior; durable delivery is out of scope.
- [WS playlist switch latency] Up to one 60 s rotation boundary before a WS device picks up new content → bounded and documented; push transports switch immediately.
- [Rollback binary skips new rule kinds] `executeThenAction` logs and skips unknown kinds, and the old admin form would drop new fields on re-save → revert before authoring new-kind rules, or accept skipped actions and re-author after rollback.

## Migration Plan

1. Add `handlers/actuation.go` (shared brightness/select/message helpers) and the `FeedController` brightness hint; cover with unit tests before anything calls them.
2. Extend `handlers/ha_discovery.go` with the light/select/text configs and the `HAStateSink` registered on `GlobalBus`; wire the new state triggers. Everything stays behind `ha_discovery_enabled` (default false), so releasing is inert for existing installs.
3. Extend `MQTTController.subscribeAll` with `select/set` and `message/set`; route through `handlePerDeviceCommand` → shared helpers. Reconnect already re-subscribes (`handlers/mqtt.go:245-258`).
4. Extend `ThenAction`/`ParseThenAction`/`validateThenAction`/`executeThenAction`; add the HTTP delivery helper. Existing stored rules are byte-unchanged and parse exactly as before.
5. Add admin form fields and `buildThenJSON`/`eventRuleFormVarsWithThen` handling; re-saving an old rule produces the same JSON for the four existing kinds.
6. Update README (`Push-to-Display`, `Event-Driven Switching`) and the MQTT settings page help; run `task pre-push` and the device-side tests are untouched.
7. Rollback: disable MQTT/HA toggles or revert the binary; no schema was added, retained HA configs can be cleared by deleting devices or disabling discovery; rules with new kinds are skipped safely by an older binary.

## Open Questions

- Should a `source:` select outside the device's rotation take over the screen (new tier) or stay a logged no-op? Assumed no-op for v1; adding the source to the playlist is the supported path.
- Should the rule `http` secret live inline on the rule row or reference a central integration secret? Assumed inline for v1 (rules are admin-only rows); revisit if secret rotation needs to be centralized.
- Should `brightness` rules target device groups in addition to device/all? Assumed device/all for v1; groups would reuse `effectiveBrightnessConfig` but need group-aware hint fan-out.
- Should the legacy `number` brightness entity eventually be marked diagnostic/deprecated in favor of the light? Assumed keep both indefinitely to avoid breaking automations.
- Should a later `media_player` entity expose `datasource/nowplaying` state? Deferred until the now-playing state contract is stable per device.
