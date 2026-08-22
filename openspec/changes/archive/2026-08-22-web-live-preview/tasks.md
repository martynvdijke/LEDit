## 1. Backend: Preview WebSocket Endpoint

- [x] 1.1 Add `WSHub.HandleDevicePreviewWS` in `handlers/websocket.go`: load device by id (404/403 guards), load settings + sources, call `serveFeed` at device width/height/refresh with `&FeedController{}`, no `last_seen_at` writes
- [x] 1.2 Register `GET /ws/device/:id/preview` behind the admin session middleware in `handlers/server.go`
- [x] 1.3 Refactor `serveFeed` to accept connection metadata (cacheKey prefix + optional device id/resolution) via a small struct without changing `/ws/feed` or `/ws/device/:token` behavior
- [x] 1.4 Unit test: preview handler never calls `SetLastSeenAt`/`ClearLastSeenAt` (assert no liveness write); 404 for missing id; 403 for disabled device

## 2. Backend: Preview Page Handler

- [x] 2.1 Add `AdminDevicePreview` handler in `handlers/` rendering a new `device_preview.html` template (device metadata + feed display markup with `data-device-preview` + device id)
- [x] 2.2 Register `GET /admin/devices/:id/preview` in the admin group
- [x] 2.3 Add "Preview" link per device row in `web/templates/admin/devices.html`
- [x] 2.4 Playwright test: preview page loads, shows device name/resolution/refresh, connects WebSocket, receives frames

## 3. Frontend: Feed Page Device Selector

- [x] 3.1 Inject the device list into the main feed page context (`index.html`) as a `<select data-device-select>` with "Shared preview (0)" + device options
- [x] 3.2 Extend `web/frontend/feed.ts`: selector change closes and reconnects the socket to `/ws/device/{id}/preview` (or `/ws/feed` for 0), reusing status/reconnect logic; hide selector when list is empty
- [x] 3.3 Add stale-frame handling to `feed.ts`/template: when `stale` flag present, show `STALE (N s)` badge on the status line
- [x] 3.4 Playwright test: selector present with devices, switching reconnects and frames continue; no selector without devices

## 4. Verification

- [x] 4.1 Unit test: `/ws/feed` and `/ws/device/:token` behavior byte-identical after `serveFeed` refactor
- [x] 4.2 Run `task pre-push` (gofmt, tests, build) and fix failures
- [x] 4.3 Confirm hardware device feed and `last_seen_at` semantics unchanged
