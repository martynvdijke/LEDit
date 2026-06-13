## Context

LEDit is a self-hosted LED matrix display server with a Go/Gin backend, Ent ORM/SQLite database, and server-rendered Bootstrap 5 frontend (no JS framework). The admin panel is accessible at `/admin/` — currently unauthenticated by default. The codebase has grown organically with significant repetition in CRUD handlers, leftover artifacts from a prior Django migration, and several half-implemented features (theme editor, input validation).

The fix surface covers 11 capability areas across the backend (`handlers/`, `datasource/`), database schema (`ent/schema/`), and frontend templates (`web/templates/`, `web/static/`). Changes are largely independent, allowing sequential implementation with minimal interdependency risk.

## Goals / Non-Goals

**Goals:**
- Remove all unused Django admin and debug toolbar static assets (~40 files)
- Implement bcrypt-hashed admin credentials stored in DB, configurable via env var on first run, with auth enabled by default
- Consolidate ~500 lines of repetitive CRUD handlers into a generic pattern (~100-150 lines)
- Make all pages use the shared `sidebar.html` template partial with active-state highlighting and responsive collapse
- Implement actual save functionality for the custom theme editor
- Persist notification history to SQLite instead of in-memory slice
- Add server-side input validation to all admin form handlers
- Add flash message feedback (success/error toasts) after form submissions
- Complete dashboard stat cards to cover all 14 datasource types
- Rename schedule `cron` field to `time_range` to match actual behavior
- Tighten WebSocket origin checking, add unique filenames for uploads, surface DB errors
- All changes must pass `task pre-push` (gofmt, tests, build)

**Non-Goals:**
- Frontend JS framework migration (no React/Vue — sticking with server-rendered templates)
- CSS redesign or theming overhaul (Bootstrap 5 CDN stays, no build pipeline)
- Additional datasource types beyond the current 14
- API versioning or REST API redesign
- Adding a testing framework beyond existing Playwright E2E and Go unit tests
- CRDT, shared state, or multi-user collaboration
- Email notification sending (SMTP settings page exists but sending is out of scope)
- PWA caching strategy overhaul (minimal changes only)

## Decisions

### 1. Auth: bcrypt + DB storage + env-var bootstrap
- **Decision**: Store hashed admin password in a new `AdminSettings` Ent schema. On first run (no admin settings exist), read `LEDIT_ADMIN_PASSWORD` env var (or default to `ledit` for backward compat). Hash with bcrypt and persist. Auth middleware enabled by default when admin settings exist.
- **Rationale**: Bcrypt is the standard for password hashing in Go. DB storage allows the user to change the password via the admin panel later. Env var bootstrap covers headless/Docker deployments. Defaulting to enabled maintains security posture.
- **Alternative considered**: Environment variable only — rejected because user cannot change password without restart. File-based config — rejected because it adds complexity for Docker users.

### 2. CRUD consolidation: registry-based generic handlers
- **Decision**: Create a `DatasourceRegistry` that maps endpoint names to a `DatasourceType` struct containing Ent client methods (Create/Update/Delete/Get). The generic handlers call `registry[endpoint].Create(...)` etc. The per-type thin wrappers (`AdminSonarrNew`, `AdminSonarrCreate`) become one-liner calls to the generic handler.
- **Rationale**: A registry pattern is simpler than reflection and more Go-idiomatic. It keeps type safety while eliminating the switch-case repetition. Each datasource type registers its Ent client methods in an `init()` or setup function.
- **Alternative considered**: Reflection-based generic handler — rejected because it loses type safety and makes code harder to follow. Code generation — unnecessarily heavyweight for this scope.

### 3. Form feedback: flash messages via session cookie
- **Decision**: Store flash messages (success/error strings) in the session cookie as JSON. Use a Gin middleware to check for flash messages on each request and inject them into the template context. Clear after display.
- **Rationale**: Go's `html/template` doesn't have built-in flash messaging. Session cookies are the simplest approach that works without modifying the database schema. The existing session cookie mechanism in `auth.go` can be extended.
- **Alternative considered**: DB-backed flash messages — overengineered for transient UI state. Query parameter — awkward for redirect chains.

### 4. Sidebar consolidation: single partial + JS toggle
- **Decision**: Make `base.html` and `index.html` use `{{template "sidebar" .}}` like admin templates already do. Add an `active` field to the template context. Add a hamburger button and responsive CSS to collapse the sidebar on small screens using Bootstrap's off-canvas pattern.
- **Rationale**: The `sidebar.html` partial already exists and admin templates use it. The two main pages just need to be converted. Bootstrap 5 has built-in off-canvas support — no additional JS library needed.
- **Alternative considered**: Inline SVG hamburger + custom CSS — reinventing Bootstrap's off-canvas. Server-side detection of mobile — unnecessary, CSS media queries handle this perfectly.

### 5. Theme editor: JSON annotation on GeneralSettings
- **Decision**: Store theme settings as a JSON blob in a new `theme` text field on `GeneralSettings`. Parse/serialize in the handlers. Keep the existing `render.Theme` struct for the rendering engine.
- **Rationale**: Theme is a single set of preferences per instance. A dedicated table is overkill. JSON annotation keeps the schema migration simple (one new column).
- **Alternative considered**: New `CustomTheme` Ent schema — too much overhead for 5 scalar fields.

### 6. Notification persistence: new Ent schema
- **Decision**: Create `ent/schema/notification.go` with fields: id, title, message, created_at. Keep the in-memory queue for active feed display but persist all notifications to DB on write. Startup loads recent 50 from DB.
- **Rationale**: Notifications are audit-worthy (priority messages, webhook alerts). DB persistence means they survive restarts. The in-memory queue remains for the live WebSocket feed to avoid DB reads on every display cycle.
- **Alternative considered**: Full DB-only approach — unnecessary DB load on every feed cycle. File-based — worse querying capability.

### 7. Input validation: reusable validation helpers
- **Decision**: Create a `validation.go` or integrate validation into the generic handler pattern with a `Validate()` function on each form struct. Return validation errors as flash messages.
- **Rationale**: Consistent validation across all 14 datasource types. Reusable URL, token, number-range validators.
- **Alternative considered**: Third-party validation library — unnecessary dependency for simple validations.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| **Auth default-enabled breaks existing headless deployments** that rely on no-auth access | Env var `LEDIT_AUTH_DISABLE=true` provides opt-out; document migration in changelog |
| **CRUD consolidation introduces regression** in one of 14 datasource types | Each datasource type has existing Playwright E2E tests verifying form load + submission; add a generic CRUD integration test |
| **Sidebar refactor breaks page layout** if template context is missing `active` field | All templates using sidebar need `active` passed; catch missing template variables at build time with Go's `template.Must` |
| **Schedule field rename (`cron` → `time_range`)** breaks existing DB rows | Migration path: add `time_range` column, copy `cron` values, deprecate `cron` column, remove in next release |
| **bcrypt import** adds a new Go dependency | `golang.org/x/crypto` is a Google-maintained sub-repo, widely used and stable |
| **Flash messages depend on cookie secret** predictability | Use a random HMAC key generated at server startup for cookie signing |
