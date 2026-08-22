## Why

The current `api-authentication` spec and implementation expose detailed feed state (current/next source names, queue, notification history, device roster) to unauthenticated visitors via `GET /`, `GET /api/feed/current`, `GET /api/notifications`, and the public WebSocket feed. For a self-hosted wall this leaks operational detail to anyone on the network. The desired posture is minimal public surface — unauthenticated users see only basic public info (app version, build, and that the service is running) and must log in to see actual feed/queue/device details.

## What Changes

- **Gate feed details behind auth**: `GET /` (IndexHandler) returns a minimal public landing when no valid session exists — version/build, service health, and login CTA — and only renders the live feed, device selector, telemetry, and controls when the request carries a valid session cookie. `GET /api/feed/current` and `GET /api/notifications` become authenticated (session or bearer token) and return `401` for anonymous callers; their current public-allowed behavior is removed. Device-roster exposure on `/` is gated the same way.
- **Keep intended public surface**: `GET /api/health`, `GET /api/trmnl/stats`, `/static/*`, `/media/*`, `/ws/feed`, `/ws/device/:token`, `/setup`, `/login`, `/forgot-password`, `/reset-password` remain public. WebSocket feeds stay token-gated per device but the browser preview feed at `GET /` is handled by the gated IndexHandler change.
- **BREAKING**: Anonymous polling of `/api/feed/current` and `/api/notifications` will start returning `401` instead of `200`. Clients that relied on unauthenticated polling must send a session cookie or bearer token.
- **Frontend behavior**: `web/templates/index.html` gains an `isAuthenticated` flag; unauthenticated rendering hides `#media-display` / `#source-label` / `#next-label` details and feed controls, showing only public info + login link. Authenticated rendering is unchanged.

## Capabilities

### New Capabilities
- `public-feed-surface`: Minimal public information shown to unauthenticated visitors — version/build/health and login CTA on `/` and via a new `GET /api/public-info` (or reused health-equivalent) without exposing source/queue/device data.

### Modified Capabilities
- `api-authentication`: Narrow `Public display reads` requirement — only `/api/trmnl/stats` and `/api/health` (plus TRMNL/health-equivalent) stay unauthenticated; `current feed` and `notification` reads become authenticated. Add scenarios for anonymous vs authenticated access to `/api/feed/current` and `/api/notifications` and for gated `GET /` rendering.

## Impact

- **Handlers**: `handlers/handlers.go#IndexHandler`, `handlers/feed_control.go#APIFeedStatus` / `APINotificationHistory`, `handlers/server.go#setupRoutes` (move feed/notification reads from public `api` group to authenticated group or add auth middleware), `handlers/auth.go` (reuse session check for IndexHandler).
- **Templates**: `web/templates/index.html` — conditional blocks on `isAuthenticated`.
- **API clients**: Any unauthenticated polling of `/api/feed/current` or `/api/notifications` must be updated to authenticate; TRMNL and health checks unaffected.
- **Specs**: Delta to `openspec/specs/api-authentication/spec.md`; new `public-feed-surface` spec.
- **Tests**: `handlers/api_token_test.go`, `main_test.go` expectations for anonymous `200` on feed/notification reads must become `401`; new tests for gated `GET /` and for `GET /api/feed/current` / `GET /api/notifications` auth.
