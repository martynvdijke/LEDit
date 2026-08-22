# Design: web-live-preview

## Context

`HandleWS` (websocket.go:176) serves the browser preview: 400×400, shared `GlobalFeed` controller, admin-free (same-origin). `HandleDeviceWS` (websocket.go:203) serves hardware: token auth via `devicesettings.TokenEQ`, marks `last_seen_at` on connect and clears it on disconnect, per-connection `FeedController{}`, device width/height/refresh interval. `serveFeed` (websocket.go:273) is the shared render/cycle loop and is protocol-identical for both today. Admin routes live under `/admin/*` with session auth middleware; the devices admin page is `AdminDeviceSettingsList` (`devices.html`). The live feed page is `/` (`index.html` + `web/frontend/feed.ts`), which connects to `/ws/feed` and renders frames into `#media-display` with matrix overlay, source/next labels, and status line.

## Goals / Non-Goals

**Goals**
- Browser-accurate preview of any device: its resolution, its refresh interval, its own control state.
- Zero side effects on device health: no `last_seen_at` writes, no token requirement.
- Reuse the existing feed display and `serveFeed` — no second render pipeline.

**Non-Goals**
- No public/unauthenticated wall (v1 is admin-only; sharing a wall URL publicly is a future change with its own auth story).
- No push notifications, no screenshots/recording, no multi-device tiling wall.
- No changes to the hardware device feed path.

## Decisions

### 1. New endpoint, hardware path untouched
`GET /ws/device/:id/preview` behind the admin session middleware (added in the same `admin` group wiring as the devices CRUD routes, but as a WebSocket route). Handler logic:
1. Load `DeviceSettings` by `:id` — 404 if missing, 403 if disabled (mirrors `HandleDeviceWS` but keyed by id, not token).
2. Load `GeneralSettings` + edges, `loadSources`.
3. Call `serveFeed(conn, sources, random, timeout, device.Width, device.Height, &FeedController{})` — same code, same per-connection controller.
4. **Never** touch `last_seen_at`.
- **Alternative considered**: overloading `HandleDeviceWS` with a `?preview=1` query flag. Rejected — conflates hardware auth with admin auth and risks a future bug where preview mode forgets to skip the liveness write. A distinct handler is ~40 lines and unambiguous.

### 2. Reuse `serveFeed` with a connection-context flag
`serveFeed` gains one optional parameter (or a small `FeedConn` struct) carrying the `cacheKey` prefix and the connection's display metadata (resolution + optional device id) so the preview page can show "device X · 32×64 · 5s refresh" and surface stale frames from the last-known-good-cache change. The message format is unchanged; `serveFeed` behavior for `/ws/feed` and `/ws/device/:token` is byte-identical.
- **Alternative considered**: duplicating `serveFeed`. Rejected — the loop already handles notifications, pause, skip, and timeouts; duplicating it would fork behavior and rot.

### 3. Preview page reuses the feed display component
`/admin/devices/:id/preview` renders a template that includes the same feed display markup as `/` (media canvas, matrix overlay, source/next/status labels) with a `data-device-preview` attribute and the device id. `feed.ts` is extended: if `[data-device-preview]` is present, it connects to `/ws/device/{id}/preview` instead of `/ws/feed` and shows the device metadata card. The page header carries "Back to devices" and a link to edit the device.
- **Alternative considered**: an iframe of `/` with a query param. Rejected — iframes complicate the admin shell, fullscreen, and the e-ink mode body class handling.

### 4. Device selector on the main feed page
`index.html` gains a `<select>` populated from a template-injected `Devices` list (`name`, `id`). On change, `feed.ts` closes the current socket and reconnects to the chosen endpoint:
- default/`0` → `/ws/feed` (shared preview, unchanged)
- device id `N` → `/ws/device/N/preview`
Connection status and reconnection backoff logic are reused as-is; the selector only swaps the URL. The selector is hidden when no devices are configured.

### 5. Stale-frame surfacing (with last-known-good-cache)
When the LKG change lands, `serveFeed` adds `stale`/`stale_age` to messages; the preview page renders a "STALE (N s)" badge on the status line when present. This change ships the UI hook (reads the flag) so it works with or without LKG deployed (flag absent → no badge).

## Risks / Trade-offs

- [Admin session required → can't glance at the wall from a phone without logging in] → accepted for v1 (security first); a future token-scoped public wall can reuse `/api` key-auth patterns from push-to-display.
- [Many browser previews → many render loops] → same cost profile as the existing `/ws/feed` and device feeds; each connection is already an independent loop today. The device-accurate preview adds one loop per open tab. Documented; fine at single-user scale.
- [Pausing a device preview implies the device pauses] → it does not: each preview gets its own `FeedController{}` (same as hardware), so pause/skip in the preview tab only affects that tab. Stated explicitly in the preview page UI ("Preview controls don't affect the device").
- [`last_seen_at` unaffected → health dashboard stays honest] → the preview handler never writes liveness; covered by a unit test asserting no `UpdateOneID(...).SetLastSeenAt` call.

## Migration Plan

None — new routes + template only. No ent migrations, no config, no protocol change. Rollback = remove the two routes and the selector wiring; `/` and hardware feeds are untouched.

## Open Questions

- Should the preview page also offer a scaled "fit to screen" toggle distinct from the existing fullscreen? v1: existing fullscreen only.
- Should the main `/` feed page keep being anonymous, or eventually require a viewer role (PLANS phase 9)? v1: unchanged (anonymous preview); noted for the viewer-role change.
