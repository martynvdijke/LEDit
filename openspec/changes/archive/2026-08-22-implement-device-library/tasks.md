## 1. Schema & code generation

- [x] 1.1 Add `token` (unique string), `refresh_interval` (int, default 60), and `last_seen_at` (nullable time) fields to `ent/schema/device_settings.go`
- [x] 1.2 Run `go generate ./ent` to regenerate the Ent client
- [x] 1.3 Add a token-generation helper (crypto/rand URL-safe hex) and wire it into device creation

## 2. Resolution-aware datasource rendering

- [x] 2.1 Change `datasource.Datasource.GetPNG()` to `GetPNG(width, height int) (*render.RenderedImage, error)`
- [x] 2.2 Update every datasource implementation to thread width/height into `render.RenderDict`/`render.RenderText` (image/video rescale; textslide scales font)
- [x] 2.3 Update all `GetPNG()` call sites and remove hardcoded 400×400 values

## 3. Shared source loading & device streaming

- [x] 3.1 Extract the source-collection logic from `handlers/websocket.go` `HandleWS` into a shared `loadSources` helper
- [x] 3.2 Refactor `HandleWS` (browser preview) to use the shared helper, still rendering at preview resolution (400×400)
- [x] 3.3 Add a per-device WebSocket handler for `GET /ws/device/:token` that looks up the device, rejects unknown/disabled tokens, and updates `last_seen_at`
- [x] 3.4 Give each device connection its own feed controller and use the device's `refresh_interval` + `width`/`height`
- [x] 3.5 Register the `/ws/device/:token` route in `handlers/server.go`
- [x] 3.6 Replace single-consumer `PopPriorityMessage` with a shared notification sequence + per-connection cursor so every feed (preview and devices) receives each notification once

## 4. Device management UI

- [x] 4.1 Update `AdminDeviceSettingsCreate`/`Update` to persist `refresh_interval` and generate/backfill tokens
- [x] 4.2 Add token backfill for existing device rows
- [x] 4.3 Update `device_form.html` with a refresh-interval field
- [x] 4.4 Update `devices.html` to show token, connection URL, and online/offline status
- [x] 4.5 Surface `last_seen_at` updates on device connect/disconnect

## 5. Python device client

- [x] 5.1 Add `device/` directory with a single-file Python client (rpi-rgb-led-matrix bindings + websocket-client) that connects to `/ws/device/<token>`, decodes PNG frames, and renders to the panel
- [x] 5.2 Add `device/README.md` documenting Pi Zero setup, dependencies, and CLI/env parameters

## 6. Tests & verification

- [x] 6.1 Add handler tests for the device WebSocket endpoint (valid token, unknown token, disabled device)
- [x] 6.2 Add tests for resolution threading and token generation
- [x] 6.3 Run `task pre-push` (gofmt + tests + build)
