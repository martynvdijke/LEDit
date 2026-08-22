## Why

LEDit already renders F1, weather, calendar, news/RSS, stocks, crypto, images, videos, and text slides into pixel images and streams them over a WebSocket — and it already stores per-device configuration (name, IP, port, matrix size). But there is no device library: no client exists for a Raspberry Pi Zero to connect to the server, receive frames, and drive an LED matrix. The devices configured in the admin panel are inert rows — the single global `/ws/feed` stream ignores device records entirely, renders every datasource at a hardcoded 400×400, and cycles sources on one shared global timer. Users can manage "devices" in the UI but cannot actually put content on a physical matrix.

## What Changes

- Add a **small Python device library** (single file) that connects to the server over WebSocket, receives PNG frames, and drives an `rpi-rgb-led-matrix` panel. All rendering stays on the server; the device is a dumb frame renderer.
- Add a **per-device WebSocket endpoint** `GET /ws/device/<token>` so each device gets its own stream, rendered at its configured matrix resolution and cycled on its own interval.
- Add a **`refresh_interval`** field (default 60 seconds) and a **`token`** (device identity) to `DeviceSettings`; generate the token when a device is created.
- Thread **matrix resolution (width × height)** through the datasource → render pipeline. **BREAKING (internal only):** `Datasource.GetPNG()` gains `(width, height int)` parameters; every datasource implementation is updated.
- Refactor the datasource-collection logic currently inlined in `HandleWS` into a shared helper used by both the browser preview feed and the per-device feed.
- Wire the device management page to show each device's token, connection URL, refresh interval, and (where known) connection status.
- Broadcast priority/notification messages to **all** connected feeds (preview + every device) instead of popping once.

## Capabilities

### New Capabilities

- `device-client`: The Python device library — a single-file WebSocket client that receives PNG frames from the server and renders them onto an rpi-rgb-led-matrix panel. Covers connection, reconnection, frame decoding, and matrix sizing.
- `device-streaming`: Per-device WebSocket streaming on the server — token-authenticated device connections, per-device resolution and cycle interval, per-device feed control, and resolution-aware datasource rendering.
- `device-management`: Device identity and configuration on the admin panel — token generation, refresh-interval configuration, connection-URL display, and connection status.

### Modified Capabilities

- *(none)* — no existing spec covers device behavior today; device CRUD exists only as implementation with no spec-level contract.

## Impact

- **Go handlers**: new device WebSocket handler + route registration in `handlers/server.go`; extract shared datasource-collection helper from `handlers/websocket.go`.
- **Datasource interface**: `datasource.Datasource.GetPNG()` changes signature to accept width/height; ~15 datasource implementations updated.
- **Ent schema**: `DeviceSettings` gains `token` (unique string) and `refresh_interval` (int, default 60); optional `last_seen_at` for status.
- **New `device/` directory**: Python client script + a short README documenting setup on the Pi Zero.
- **Web templates**: `device_form.html` and `devices.html` updated to expose token, refresh interval, and connection URL.
- **Dependencies**: none new for the server; the Python client depends on `rpi-rgb-led-matrix` and `websocket-client` (device-side only, not part of the Go build).
- **Tests**: handler tests for the device WebSocket endpoint, token lookup, and resolution threading.
