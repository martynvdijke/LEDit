## 1. Audit and storage

- [x] 1.1 Inventory feed, webhook, admin, and log mutation routes.

  Audit result (handlers/server.go setupRoutes):
  - Public GET (keep public): `/`, `/ws/feed`, `/ws/device/:token`, `/eink/toggle`,
    `GET /api/feed/current`, `GET /api/notifications`, `GET /api/trmnl/stats`,
    `GET /api/health`, login/password pages.
  - Unprotected API mutations (need bearer token): `POST /api/feed/next`,
    `POST /api/feed/pause`, `POST /api/feed/resume`, `POST /api/feed/priority`,
    `POST /api/webhook/notify`.
  - Admin routes: all `/admin/*` already session-protected via AuthMiddleware
    (datasource CRUD, log settings, email/AI/alert settings, log viewer).
  - Log writes: `AdminLogsAPI` is GET (read-only); log settings save is POST
    under `/admin` (session-protected). No unprotected log writes exist.
- [x] 1.2 Add hashed-token persistence, indexes, and reversible migration.

  ent/schema/apitoken.go: ApiToken entity with unique token_hash, token_prefix,
  owner_id, created_at, optional expires_at/revoked_at/last_used_at; index on
  owner_id. Codegen via `go generate ./ent`. Migration is ent auto-create on
  startup; reversible by removing the schema (documented in the schema file).

## 2. Middleware and lifecycle

- [x] 2.1 Implement bearer validation, expiry, revocation, and owner matching.

  handlers/api_token.go: APITokenMiddleware validates `Authorization: Bearer`
  against the stored SHA-256 hash, rejects revoked/expired tokens, records
  last_used_at, and establishes the token's owner as the authenticated user.

- [x] 2.2 Protect every mutation while preserving public GET routes.

  handlers/server.go: `/api` GET reads (feed/current, notifications,
  trmnl/stats, health) stay public; POST mutations (feed/next, feed/pause,
  feed/resume, feed/priority, webhook/notify) moved behind APITokenMiddleware.

- [x] 2.3 Implement create/list/revoke/rotate token operations with one-time secrets.

  handlers/api_token.go + web/templates/admin/api_tokens.html: session-protected
  `/admin/api-tokens` routes. Secret generated as 32 random bytes, shown once on
  create/rotate, stored only as SHA-256 hash; list/revoke/rotate never return it.

## 3. Tests and rollout

- [x] 3.1 Test public reads and anonymous/token-only/malformed/expired/revoked writes.

  handlers/api_token_test.go: public GETs return 200 unauthenticated; anonymous
  writes 401; valid token write succeeds; malformed/expired/revoked tokens 401.

- [x] 3.2 Test ownership and secret non-disclosure.

  handlers/api_token_test.go: lifecycle endpoints require session; created
  tokens bind to the admin owner; stored value is the SHA-256 hash, never the
  secret; list/revoke/rotate responses never disclose the secret; rotate issues
  a fresh secret.

- [x] 3.3 Document client migration and run LEDit tests.

  README.md: documented bearer-token auth flow (create token in admin UI, send
  `Authorization: Bearer <secret>` on writes) and marked each endpoint public vs
  bearer-token. Existing main_test.go mutation tests updated to send tokens.
  `task pre-push` (gofmt, go test ./..., npm build, go build) passes.
