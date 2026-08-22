## Context

LEDit is a Go (Gin + Ent/SQLite) web server that renders F1, weather, calendar, news/RSS, stocks, crypto, images, videos, text slides, and system stats into pixel images and streams them over a single WebSocket feed (`GET /ws/feed`). That feed is a **preview** surface: it loads the single `GeneralSettings` row (ID=1), collects every configured datasource, cycles through them on one global timer (`GeneralSettings.Timeout`), renders each at a hardcoded **400×400**, and pushes JSON `{format, image(base64), source, next}` to the browser.

The admin panel can already create "devices" (`DeviceSettings`: name, ip, port, username, password, width, height, enabled), but those rows are **inert** — the feed never reads them, and no client code exists to run on a Raspberry Pi Zero. There is no device library. The goal is to make LEDit actually drive physical 64×64 (or other) LED matrices attached to one or more Pi Zero devices, with content streamed from the server over WebSocket, auto-refreshing on a configurable per-device interval.

All rendering stays on the server so the device library can remain tiny and the server can be updated without touching devices.

## Goals / Non-Goals

**Goals:**
- A minimal Python device library (single file) that connects to the server, receives PNG frames, and drives an `rpi-rgb-led-matrix` panel.
- A per-device WebSocket endpoint (`GET /ws/device/<token>`) with per-device resolution, per-device cycle interval, and per-device feed control.
- Per-device identity (`token`) and refresh interval surfaced in the admin UI, with a connection URL so a device can be pointed at the server.
- Resolution-aware rendering: the datasource → render pipeline threads width/height instead of the hardcoded 400×400.

**Non-Goals:**
- Schedule-driven content switching (`Schedule` CRUD exists but wiring it to the feed is out of scope for this change).
- E-Ink device support (E-Ink remains a browser-cookie concept; the TRMNL plugin is separate).
- Changing the browser preview feed's behavior other than routing it through the shared source loader (it keeps its own preview resolution).
- TLS/`wss://` on the device connection (assumed LAN `ws://`; `wss://` via reverse proxy is possible but not built here).

## Decisions

### 1. Python single-file client in `device/`

- **Decision**: Ship one Python file (plus a short README) in a new top-level `device/` directory. It uses the official `rpi-rgb-led-matrix` Python bindings and a small WebSocket client (`websocket-client`). It decodes the received PNG and writes it to the matrix — no rendering logic of its own.
- **Rationale**: `rpi-rgb-led-matrix` ships first-class Python bindings, so a Pi Zero can run it with almost no setup. A single file keeps the library small (per the user's explicit "keep the device library small" requirement) and trivial to update by copying one file.
- **Alternatives considered**: A Go client cross-compiled for ARM was rejected — the library's only bindings are C/C++/Python; a Go client would require cgo against the C library, which is heavier and more fragile on a Pi Zero. Keeping the prior `client/` directory was considered but the new dir `device/` is clearer given the "device library" framing.

### 2. Device connects out over WebSocket (pull), identified by token

- **Decision**: The device dials `ws://<server>/ws/device/<token>`. The server looks up `DeviceSettings` by token to authorize and configure the stream. `ip`/`port`/`username`/`password` on `DeviceSettings` become optional/legacy and are no longer used for connection.
- **Rationale**: Pull avoids NAT/firewall traversal and dynamic-IP problems, and the server never needs to know where a device lives. The token gives the server a stable per-device identity for authorization and per-device config.
- **Alternatives considered**: Server-push to a device-run listener was rejected (NAT traversal, dynamic IPs, and a larger attack surface on the device). IP-based auth was rejected (spoofable, breaks behind NAT).

### 3. `refresh_interval` = per-device cycle interval (default 60s)

- **Decision**: Add `DeviceSettings.refresh_interval` (int, seconds, default 60). For a device connection, this value — not the global `GeneralSettings.Timeout` — controls how long each source is displayed before advancing.
- **Rationale**: The user wants auto-refresh ~1 min but per-device changeable. The existing global timeout cannot express per-device cadence.
- **Alternatives considered**: Reusing the global timeout was rejected (not per-device). A separate "frame rate" concept was rejected (the interval already means "seconds per source").

### 4. `Datasource.GetPNG(width, height int)` — breaking, internal

- **Decision**: Change the interface to `GetPNG(width, height int) (*render.RenderedImage, error)` and thread width/height into `render.RenderDict` / `render.RenderText` (which already accept them). Image/Video datasources rescale to the target size; `TextSlideDS` scales its font relative to height.
- **Rationale**: This is the only place where the hardcoded 400×400 lives; making the size an explicit parameter is the clearest fix and lets preview and device renders differ.
- **Alternatives considered**: Per-datasource size fields or a render-context struct were rejected as more invasive and error-prone than an explicit parameter.

### 5. Extract a shared source-loading helper

- **Decision**: Move the ~70 lines of "load `GeneralSettings` + build `[]sourceWithName`" currently inlined in `HandleWS` into a shared helper, so both the preview feed and the per-device feed use identical source collection.
- **Rationale**: Two feeds must load sources identically; duplication would drift.
- **Alternatives considered**: Duplicating the loader was rejected for the drift risk.

### 6. Per-connection feed control; notifications broadcast to all feeds

- **Decision**: Each device WebSocket connection gets its own `FeedController` (pause/skip/next scoped to that connection) instead of sharing `GlobalFeed`, so devices are independent. Priority/notification messages are broadcast to **every** connected feed (preview and all device streams) exactly once per connection, replacing the current single-consumer `PopPriorityMessage` pop.
- **Rationale**: Independent devices should not be forced into lock-step, but a notification is a global intent that must reach every surface — not whichever feed pops the shared queue first.
- **Alternatives considered**: Keeping the single-consumer pop was rejected (a notification would reach only one random device/preview). A full pub/sub with per-device subscription management was rejected as over-engineered — a shared monotonic notification sequence with a per-connection cursor is sufficient.

### 7. Preview feed keeps a fixed preview resolution (400×400)

- **Decision**: The browser preview continues to render at a fixed preview size (400×400), while the device feed renders at the device's configured `width`/`height`.
- **Rationale**: Preserves the legibility and layout of the existing admin preview; 64×64 would be illegible in the browser.
- **Alternatives considered**: Rendering the preview at `GeneralSettings.Width/Height` (64×64) was rejected for legibility.

### 8. Token generation via `crypto/rand`, backfilled for existing devices

- **Decision**: Tokens are 32-char URL-safe random hex, generated in `AdminDeviceSettingsCreate`. Existing device rows (empty token) are backfilled with generated tokens on read/first access or via a small one-time backfill.
- **Rationale**: Random, unguessable identity; deterministic migration for pre-existing rows.
- **Alternatives considered**: User-supplied tokens were rejected (predictable/weak). Reusing `username`/`password` was rejected (not designed for URL identity).

### 9. Connection status via optional `last_seen_at`

- **Decision**: Add nullable `DeviceSettings.last_seen_at`, updated when a device connects (and on disconnect/timeout). The devices list derives a best-effort "online/offline" from its recency.
- **Rationale**: Gives operators at-a-glance device health without a full heartbeat protocol.
- **Alternatives considered**: A full ping/pong heartbeat was deferred (see Open Questions).

## Risks / Trade-offs

- **Breaking `GetPNG` signature** touches all ~15 datasources. → Mitigation: mechanical, compile-time-enforced change; done in one pass.
- **Ent schema change** requires regenerating the Ent client (`go generate`). → Mitigation: explicit task, run immediately after schema edit.
- **Notification ordering under reconnect**: a device that reconnects mid-broadcast could miss a notification. → Mitigation: each connection tracks its own cursor over a monotonic sequence; a reconnect starts from the latest position (notifications are fire-and-forget, not a durable queue).
- **Token in URL** can appear in access logs. → Mitigation: use a path parameter (`/ws/device/:token`) rather than a query string, and document that logs should be protected.
- **Pi Zero performance**: PNG decode + matrix write per frame. → Mitigation: frames arrive only every `refresh_interval` (default 60s), so CPU load is negligible.
- **Empty-token legacy devices** would fail lookup. → Mitigation: backfill tokens for existing rows as part of the change.

## Migration Plan

1. Edit `DeviceSettings` schema (add `token`, `refresh_interval`, `last_seen_at`) and run `go generate ./ent`.
2. Change `Datasource` interface to `GetPNG(width, height int)` and update every datasource + render call site; compile.
3. Extract the shared source-loading helper; refactor `HandleWS` to use it (preview still 400×400).
4. Add the per-device WebSocket handler and route (`/ws/device/:token`), with token lookup, per-connection feed control, and per-device interval/resolution.
5. Update device CRUD handlers to generate tokens and persist `refresh_interval`; add backfill for existing rows.
6. Update `device_form.html` and `devices.html` (token, refresh interval, connection URL, status).
7. Add the `device/` Python client and README.
8. Add handler tests; run `task pre-push`.

Rollback: the schema changes are additive and the interface change is internal; reverting is a normal `git revert`. Devices that never connect are unaffected.

## Open Questions

- Is `last_seen_at` on connect/disconnect sufficient, or do devices need a periodic heartbeat for accurate liveness?
- Should the device endpoint support `wss://` with a client certificate for non-LAN deployments, or is plain `ws://` + token acceptable for v1?
