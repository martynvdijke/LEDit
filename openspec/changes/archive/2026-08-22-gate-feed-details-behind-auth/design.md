## Context

LEDit currently treats feed/queue details as public. `GET /` renders the live feed, device roster, and telemetry without a session check; `GET /api/feed/current` and `GET /api/notifications` are registered on the public `api` group (see `handlers/server.go:211-212`). The archived `secure-api-mutations` change gated writes behind bearer tokens but intentionally left those reads public for display polling. The new requirement is a privacy posture change: anonymous visitors on ` /` and those two APIs should see only minimal public info (version/build, health, login CTA). Full source names, queue, notification history, and device list require an authenticated session (or bearer token for the APIs). TRMNL polling (`/api/trmnl/stats`), health (`/api/health`), and WebSocket device feeds must stay public/independently gated.

Stakeholders: browser users on the LAN, existing `api_token_test`/`main_test` polling clients, TRMNL devices, reverse-proxy / health monitors.

## Goals / Non-Goals

**Goals:**
- Gate detailed feed state (current/next source, notification history, device roster on `/`) behind auth while preserving a useful public landing.
- Make `GET /api/feed/current` and `GET /api/notifications` authenticated (session cookie or bearer token) with `401` for anonymous.
- Keep `GET /api/trmnl/stats`, `GET /api/health`, `GET /ws/feed`, `GET /ws/device/:token`, static assets, and auth/recovery routes public.
- Provide clear frontend branching on `GET /` via `isAuthenticated`.

**Non-Goals:**
- Changing WebSocket authentication model or bearer-token write protection.
- Adding new unauthenticated `GET /api/public-info` beyond what `/` and `/api/health` already expose; reuse `health` + embedded version on public `/`.
- Redesigning the setup wizard or E-Ink middleware.

## Decisions

- **Decision: Gate at handler + route grouping, not middleware rewrite**
  - Route: move `api.GET("/feed/current")` and `api.GET("/notifications")` into the authenticated subgroup (or protect inline with `AuthMiddleware` + `APITokenMiddleware` accepting either credential). Inline check `isAuthenticated(c)` in `IndexHandler` to branch template data instead of a global redirect.
  - Alternatives: global redirect for all `/` → `/login` (too aggressive, breaks public landing); new middleware that blocks `Accept: text/html` (fragile content-negotiation).
  - Rationale: minimal diff, matches existing `AuthMiddleware`/`APITokenMiddleware` patterns, easy to test with `httptest`.

- **Decision: Single `isAuthenticated` helper used by both HTML and API gates**
  - Helper checks `sessions` map for a valid `session` cookie (same as `AuthMiddleware`) and also accepts `Authorization: Bearer <token>` via `validateAPIToken` so API callers can use either mechanism. `IndexHandler` sets `isAuthenticated` for template; API handlers return `401` with `WWW-Authenticate` when neither present.
  - Alternative: duplicate logic per handler (more drift); separate `PublicInfo` endpoint (unneeded — public `/` already serves version/health).
  - Trade-off: session map is global in-memory; acceptable for single-instance LEDit. No DB lookup needed.

- **Decision: Public landing reuses existing health/version data**
  - `IndexHandler` when unauthenticated omits `devices` query or returns empty slice, omits telemetry, and sends `appVersion` (from `go.mod`/`build info` or static string) + health summary. No new Ent query.
  - Alternative: dedicated `GET /api/public-info` (extra surface to maintain, not requested).
  - Public template block shows: app name/version, brief "LEDit is running", health badge, and `Login to view feed` button. No source names, queue, or controls.

- **Decision: Keep TRMNL/health/WebSocket public**
  - `trmnl/stats` is a pull endpoint for an external display fleet; gating it would break the integration. `health` is for orchestrators. `ws/feed` and `ws/device/:token` are already token/session gated at the WS layer.
  - Alternative: gate health too (breaks k8s probes).

## Risks / Trade-offs

- **Anonymous polling breakage** → Clients polling `/api/feed/current` or `/api/notifications` unauthenticated will start receiving `401`. Mitigation: mark as **BREAKING** in proposal, document migration to send `Cookie: session=` or `Authorization: Bearer <token>`, and support both credentials on those endpoints during transition.
- **Template divergence** → Two render branches for `/` double the visual-regression surface. Mitigation: share layout/CSS, add `isAuthenticated` conditional only around telemetry/device sections; add Playwright snapshot for public vs authenticated `/`.
- **Session vs token ambiguity on APIs** → API callers could be browser or machine. Mitigation: accept either on the gated reads; write path already requires bearer token, reads accept either.
- **Caching/proxies** → Public landing should not be cached as authenticated. Mitigation: set `Cache-Control: no-store` on the gated response or at least `Vary: Cookie, Authorization`.

## Migration Plan

1. Deploy code change — no DB migration, no config.
2. Unauthenticated `GET /` now shows public landing; `GET /api/feed/current` and `GET /api/notifications` without creds return `401`.
3. Update any internal scripts/dashboards that polled those endpoints unauthenticated to authenticate (create an API token via `/admin/api-tokens` or log in).
4. Rollback: revert route registration and `IndexHandler` branching; no data loss.
5. Tests: update `handlers/api_token_test.go` expectations for anonymous reads (`200` → `401`), add handler tests for gated `GET /` (public vs authenticated) and for the two APIs.

## Open Questions

- Exact version string source for public landing: use `runtime/debug.ReadBuildInfo` Main version, fallback to `ledit` + `CHANGELOG` latest tag, or static constant in `handlers`?
- Should `GET /api/notifications` also support page/limit query params after gating, or keep current shape?
