## 1. Legacy Asset Cleanup

- [x] 1.1 Remove all files under `web/static/admin/` (css/, js/, img/)
- [x] 1.2 Remove all files under `web/static/debug_toolbar/` (css/, js/)
- [x] 1.3 Verify no remaining references to removed asset paths in templates or Go code
- [x] 1.4 Run `task pre-push` to confirm removal doesn't break anything

## 2. Auth Hardening — Schema & Bootstrap

- [x] 2.1 Create `ent/schema/adminsettings.go` with fields: id (int), username (string), password_hash (string)
- [x] 2.2 Run `go generate ./ent` to regenerate Ent client code
- [x] 2.3 Add `golang.org/x/crypto` dependency and run `go mod tidy`
- [x] 2.4 Implement first-run bootstrap in `handlers/server.go` — on startup, if no AdminSettings record exists, create one from `LEDIT_ADMIN_PASSWORD` env var (or default "ledit"), hashed with bcrypt
- [x] 2.5 Add `LEDIT_AUTH_DISABLE` env var check in `handlers/server.go` to optionally skip auth

## 3. Auth Hardening — Middleware & Session

- [x] 3.1 Update `handlers/auth.go` — `AuthMiddleware` reads from DB `AdminSettings` instead of hardcoded `authEnabled` bool; check existence of AdminSettings record
- [x] 3.2 Update `LoginAction` — validate password against bcrypt hash in DB instead of hardcoded `adminPass`
- [x] 3.3 Add password settings page or integrate into existing settings UI (admin can change username/password)
- [x] 3.4 Run `task pre-push` and fix any issues

## 4. CRUD Consolidation — Registry & Generic Handlers

- [x] 4.1 Create a `DatasourceRegistry` type in `handlers/` that maps endpoint name to a struct with Ent create/update/delete/get functions
- [x] 4.2 Implement `registerDatasource(endpoint, ...)` setup function called during server initialization
- [x] 4.3 Register all 8 token/URL datasource types (sonarr, radarr, f1, weather, homeassistant, untappd, crypto, stock)
- [x] 4.4 Replace `createTokenURLDS` switch-case with generic handler using registry lookup
- [x] 4.5 Replace `editTokenURLDS` switch-case with generic handler
- [x] 4.6 Replace `updateTokenURLDS` switch-case with generic handler
- [x] 4.7 Replace `deleteTokenURLDS` switch-case with generic handler
- [x] 4.8 Replace `addEdge` switch-case with generic handler
- [x] 4.9 Convert all per-type thin wrappers (AdminSonarrCreate, etc.) to one-liner calls to generic handlers
- [x] 4.10 Run `task pre-push` and fix any issues

## 5. Sidebar Consolidation

- [x] 5.1 Update `base.html` — replace inline sidebar with `{{template "sidebar" .}}`
- [x] 5.2 Update `index.html` — replace inline sidebar with `{{template "sidebar" .}}`
- [x] 5.3 Add `active` field to template context for all routes, pass current page identifier
- [x] 5.4 Update `sidebar.html` partial — add `active` class logic using the passed identifier
- [x] 5.5 Add responsive hamburger toggle and off-canvas collapse to sidebar (Bootstrap off-canvas)
- [x] 5.6 Run E2E tests to verify sidebar navigation still works
- [x] 5.7 Run `task pre-push`

## 6. Theme Editor Persistence

- [x] 6.1 Add `theme` JSON text field to `GeneralSettings` Ent schema
- [x] 6.2 Run `go generate ./ent`
- [x] 6.3 Implement `AdminThemeSave` — parse form fields (bg_color, accent_color, text_color, title, font_size), serialize to JSON, save to GeneralSettings
- [x] 6.4 Update `AdminThemeEditor` (GET) — load saved theme from DB, pre-populate form; fall back to default values if none saved
- [x] 6.5 Run `task pre-push`

## 7. Notification Persistence

- [x] 7.1 Create `ent/schema/notification.go` with fields: id (int), title (string), message (string), created_at (time.Time)
- [x] 7.2 Run `go generate ./ent`
- [x] 7.3 Update `AddNotification` in `handlers/feed_control.go` — also write to DB
- [x] 7.4 Load recent 50 notifications from DB on startup into in-memory queue
- [x] 7.5 Update `AdminNotifications` handler to read from DB
- [x] 7.6 Run `task pre-push`

## 8. Form Feedback — Flash Messages

- [x] 8.1 Implement flash message helpers: `SetFlash(c, type, msg)` and `GetFlash(c)` using session cookies
- [x] 8.2 Add flash message middleware that injects flash into template context and clears after display
- [x] 8.3 Add flash message display template partial (Bootstrap toast/alert)
- [x] 8.4 Wire flash messages into general settings save handler
- [x] 8.5 Wire flash messages into all datasource CRUD handlers (create, update, delete)
- [x] 8.6 Wire flash messages into schedule, device, theme, and settings handlers
- [x] 8.7 Run `task pre-push`

## 9. Dashboard Completeness

- [x] 9.1 Update `AdminDashboard` handler to query and count RSS Feed, Calendar, Stock, and Text Slides datasources
- [x] 9.2 Add stat cards for RSS Feed, Calendar, Stock, and Text Slides to `dashboard.html`
- [x] 9.3 Ensure all 14 datasource types render correctly in the source table
- [x] 9.4 Run `task pre-push`

## 10. Input Validation

- [x] 10.1 Create reusable validation helpers: `ValidateURL`, `ValidateRequired`, `ValidateRange`, `ValidatePort`
- [x] 10.2 Add URL validation to all datasource form handlers
- [x] 10.3 Add token/required field validation to all datasource form handlers
- [x] 10.4 Add range validation to settings form (timeout, width, height)
- [x] 10.5 Add range validation to device form (port, width, height)
- [x] 10.6 Integrate validation errors with flash message system
- [x] 10.7 Run `task pre-push`

## 11. Schedule Naming Clarity

- [x] 11.1 Rename `cron` field to `time_range` in Schedule Ent schema
- [x] 11.2 Run `go generate ./ent`
- [x] 11.3 Update all Schedule handlers to use `time_range` instead of `cron`
- [x] 11.4 Update `schedule_form.html` — change field name to `time_range`
- [x] 11.5 Update `schedules.html` — change table header from "Cron / Time Range" to "Time Range"
- [x] 11.6 Add migration path: copy existing `cron` values to `time_range` on startup
- [x] 11.7 Run `task pre-push`

## 12. Security Hardening

- [x] 12.1 Update `websocket.go` — replace `CheckOrigin: return true` with origin validation against configured device URLs
- [x] 12.2 Add config for allowed WebSocket origins (device IPs from device settings)
- [x] 12.3 Update image/video upload handlers to generate unique filenames (UUID + extension)
- [x] 12.4 Add error checking to `updateTokenURLDS` — log and return flash message on DB error
- [x] 12.5 Add error checking to `deleteTokenURLDS` — log and return flash message on DB error
- [x] 12.6 Run `task pre-push`

## 13. Verification

- [x] 13.1 Run full test suite: `go test ./...` (Go unit tests)
- [x] 13.2 Run E2E tests: `task test:e2e` (Playwright)
- [x] 13.3 Run build: `go build -o ledit .`
- [x] 13.4 Run `go fmt ./... && go test ./... && go build ./...` for final validation
