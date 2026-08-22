## 1. Authentication helper and route gating

- [x] 1.1 Add reusable `isAuthenticated(c *gin.Context) bool` helper (checks valid `session` cookie via `sessions` map and valid bearer token via existing `validateAPIToken`) for use by `IndexHandler` and the two gated APIs; unit-test helper directly
- [x] 1.2 Move `GET /api/feed/current` and `GET /api/notifications` from the public `api` group to an authenticated subgroup in `handlers/server.go` that accepts either session or bearer token (reuse `AuthMiddleware` logic + `APITokenMiddleware` with OR), return `401` with `WWW-Authenticate` for anonymous; verify `GET /api/trmnl/stats` and `GET /api/health` remain public
- [x] 1.3 Update `handlers/feed_control.go` — `APIFeedStatus` and `APINotificationHistory` to enforce the new auth check (return `401` when unauthenticated) and add `Cache-Control: no-store` on error path if applicable

## 2. Public landing (GET /) gating

- [x] 2.1 Update `handlers/handlers.go:IndexHandler` to branch on `isAuthenticated`: unauthenticated → minimal public data (app name/version from `debug.ReadBuildInfo` or constant, health/running status, `isAuthenticated: false`, `devices: []`, no telemetry); authenticated → current full data (`devices`, `eink_mode`, umami, etc.) with `isAuthenticated: true`
- [x] 2.2 Update `web/templates/index.html` to conditionally render: when `isAuthenticated` is false show public block (version, health badge, `Login to view feed` link) and hide `#media-display`, `#source-label`, `#next-label`, device selector, and feed controls; when true render current authenticated markup unchanged
- [x] 2.3 Ensure unauthenticated `GET /` sets `Cache-Control: no-store` and `Vary: Cookie, Authorization`

## 3. Tests and docs

- [x] 3.1 Update `handlers/api_token_test.go` — `TestAPIPublicReadsUnauthenticated` no longer expects `200` for `/api/feed/current` and `/api/notifications`; add `TestAPIFeedCurrentRequiresAuth` and `TestAPINotificationsRequiresAuth` covering anonymous `401` vs session/bearer `200`
- [x] 3.2 Add handler tests for `GET /` gating — anonymous returns public landing (contains version/login CTA, no source/device details), authenticated with valid session returns full feed markup
- [x] 3.3 Update `main_test.go` expectations that relied on anonymous `200` for the two gated endpoints; add Playwright check that unauthenticated `GET /` hides feed controls
- [x] 3.4 Update `openspec/specs/api-authentication/spec.md` delta validation and `README`/API docs to note the breaking change for anonymous polling of `/api/feed/current` and `/api/notifications`

