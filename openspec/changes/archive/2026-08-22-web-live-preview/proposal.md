# Change Proposal: web-live-preview

## Why

LEDit has two WebSocket feeds, and neither lets you *see what a specific wall is showing* from the browser. `/ws/feed` (handlers/websocket.go:176) streams a shared 400×400 preview with a global feed controller — it never reflects a device's real resolution, refresh interval, or independent pause state. `/ws/device/:token` (websocket.go:203) is device-accurate but is designed for hardware: it requires the device token, marks `last_seen_at` (a browser mirror would fake device liveness and corrupt the source-health-monitoring badges), and is controlled per-connection by the device itself. There is no way to open a browser tab and watch *your kitchen wall* at its true 32×64 resolution and 5-second refresh — the thing you'd want for a monitor screen, a second room, or a quick check on your phone.

## What Changes

1. **Device-accurate preview feed** — a new admin-authenticated WebSocket endpoint `/ws/device/:id/preview` that streams *exactly what the device would see*: the device's configured width/height, refresh interval, and its own independent feed controller (pause/skip in the preview does not touch the physical device). It does **not** mark `last_seen_at` and does **not** require the device token — a browser preview must never impersonate hardware. Authentication is the existing admin session, same as every other admin route.

2. **Per-device live preview page** — a new admin page `/admin/devices/:id/preview` that reuses the live feed display (media canvas, matrix overlay, source/next labels, connection status) pointed at the device-accurate endpoint. It shows the device's configured resolution and refresh interval alongside the frame, plus a badge when a frame is served from the last-known-good cache (stale).

3. **Device selector on the main feed page** — the existing `/` live feed page (`index.html`, `feed.ts`) gains a device dropdown: "Shared preview (400×400)" (current behavior) plus one entry per configured device. Selecting a device switches the WebSocket to the device-accurate endpoint at that device's real cadence. This makes the wall *remotely inspectable* without touching the hardware connection or its stats.

4. **Shared feed code path** — `serveFeed` (websocket.go:273) is reused unchanged for the preview endpoint; only the connection wrapper differs (auth + resolution + no liveness side effects). No changes to the device protocol, message format, or device client.

## Capabilities

### New Capabilities
- `web-live-preview`: browser-accessible, device-accurate live preview of any configured device, without affecting device liveness or requiring the device token.

### Modified Capabilities
- (none — existing capabilities keep their requirements; this is additive)

## Impact

- New handler wiring: `WSHub.HandleDevicePreviewWS` + route `/ws/device/:id/preview` (admin session); `AdminDevicePreview` page handler + route `/admin/devices/:id/preview`; device selector JS on the feed page.
- Reuses `serveFeed`, `loadSources`, the existing feed display component (`feed.ts`), and the devices page (`devices.html` gains a "Preview" link per row).
- No ent schema changes, no DB migration, no new dependencies, no changes to `HandleDeviceWS` (hardware path untouched), no WebSocket protocol changes.
- Pairs with `source-health-monitoring` (stale badge shows cached-frame delivery) and `last-known-good-cache` (stale flag surfaced in preview); independent of both.
