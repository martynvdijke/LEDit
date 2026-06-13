## 1. Legacy Asset Cleanup

- [ ] 1.1 Remove all files under `web/static/admin/` (css/, js/, img/)
- [ ] 1.2 Remove all files under `web/static/debug_toolbar/` (css/, js/)
- [ ] 1.3 Verify no remaining references to removed asset paths in templates or Go code
- [ ] 1.4 Run `task pre-push` to confirm removal doesn't break anything

## 2. Auth Hardening — Schema & Bootstrap

- [ ] 2.1 Create `ent/schema/adminsettings.go` with fields: id (int), username (string), password_hash (string)
- [ ] 2.2 Run `go generate ./ent` to regenerate Ent client code
- [ ] 2.3 Add `golang.org/x/crypto` dependency and run `go mod tidy`
- [ ] 2.4 Implement first-run bootstrap in `handlers/server.go` — on startup, if no AdminSettings record exists, create one from `LEDIT_ADMIN_PASSWORD` env var (or default "ledit"), hashed with bcrypt
- [ ] 2.5 Add `LEDIT_AUTH_DISABLE` env var check in `handlers/server.go` to optionally skip auth

## 3. Auth Hardening — Middleware & Session

- [ ] 3.1 Update `handlers/auth.go` — `AuthMiddleware` reads from DB `AdminSettings` instead of hardcoded `authEnabled` bool; check existence of AdminSettings record
- [ ] 3.2 Update `LoginAction` — validate password against bcrypt hash in DB instead of hardcoded `adminPass`
- [ ] 3.3 Add password settings page or integrate into existing settings UI (admin can change username/password)
- [ ] 3.4 Run `task pre-push` and fix any issues

## 4. CRUD Consolidation — Registry & Generic Handlers

- [ ] 4.1 Create a `DatasourceRegistry` type in `handlers/` that maps endpoint name to a struct with Ent create/update/delete/get functions
- [ ] 4.2 Implement `registerDatasource(endpoint, ...)` setup function called during server initialization
- [ ] 4.3 Register all 8 token/URL datasource types (sonarr, radarr, f1, weather, homeassistant, untappd, crypto, stock)
- [ ] 4.4 Replace `createTokenURLDS` switch-case with generic handler using registry lookup
- [ ] 4.5 Replace `editTokenURLDS` switch-case with generic handler
- [ ] 4.6 Replace `updateTokenURLDS` switch-case with generic handler
- [ ] 4.7 Replace `deleteTokenURLDS` switch-case with generic handler
- [ ] 4.8 Replace `addEdge` switch-case with generic handler
- [ ] 4.9 Convert all per-type thin wrappers (AdminSonarrCreate, etc.) to one-liner calls to generic handlers
- [ ] 4.10 Run `task pre-push` and fix any issues

## 5. Sidebar Consolidation

- [ ] 5.1 Update `base.html` — replace inline sidebar with `{{template "sidebar" .}}`
- [ ] 5.2 Update `index.html` — replace inline sidebar with `{{template "sidebar" .}}`
- [ ] 5.3 Add `active` field to template context for all routes, pass current page identifier
- [ ] 5.4 Update `sidebar.html` partial — add `active` class logic using the passed identifier
- [ ] 5.5 Add responsive hamburger toggle and off-canvas collapse to sidebar (Bootstrap off-canvas)
- [ ] 5.6 Run E2E tests to verify sidebar navigation still works
- [ ] 5.7 Run `task pre-push`

## 6. Theme Editor Persistence

- [ ] 6.1 Add `theme` JSON text field to `GeneralSettings` Ent schema
- [ ] 6.2 Run `go generate ./ent`
- [ ] 6.3 Implement `AdminThemeSave` — parse form fields (bg_color, accent_color, text_color, title, font_size), serialize to JSON, save to GeneralSettings
- [ ] 6.4 Update `AdminThemeEditor` (GET) — load saved theme from DB, pre-populate form; fall back to default values if none saved
- [ ] 6.5 Run `task pre-push`

## 7. Notification Persistence

- [ ] 7.1 Create `ent/schema/notification.go` with fields: id (int), title (string), message (string), created_at (time.Time)
- [ ] 7.2 Run `go generate ./ent`
- [ ] 7.3 Update `AddNotification` in `handlers/feed_control.go` — also write to DB
- [ ] 7.4 Load recent 50 notifications from DB on startup into in-memory queue
- [ ] 7.5 Update `AdminNotifications` handler to read from DB
- [ ] 7.6 Run `task pre-push`

## 8. Form Feedback — Flash Messages

- [ ] 8.1 Implement flash message helpers: `SetFlash(c, type, msg)` and `GetFlash(c)` using session cookies
- [ ] 8.2 Add flash message middleware that injects flash into template context and clears after display
- [ ] 8.3 Add flash message display template partial (Bootstrap toast/alert)
- [ ] 8.4 Wire flash messages into general settings save handler
- [ ] 8.5 Wire flash messages into all datasource CRUD handlers (create, update, delete)
- [ ] 8.6 Wire flash messages into schedule, device, theme, and settings handlers
- [ ] 8.7 Run `task pre-push`

## 9. Dashboard Completeness

- [ ] 9.1 Update `AdminDashboard` handler to query and count RSS Feed, Calendar, Stock, and Text Slides datasources
- [ ] 9.2 Add stat cards for RSS Feed, Calendar, Stock, and Text Slides to `dashboard.html`
- [ ] 9.3 Ensure all 14 datasource types render correctly in the source table (especially RSS with Name/URL, Calendar with Name/URL, Stock with Token/URL, Text Slides with Content/Color)
- [ ] 9.4 Run `task pre-push`

## 10. Input Validation

- [ ] 10.1 Create reusable validation helpers: `ValidateURL`, `ValidateRequired`, `ValidateRange`, `ValidatePort`
- [ ] 10.2 Add URL validation to all datasource form handlers
- [ ] 10.3 Add token/required field validation to all datasource form handlers
- [ ] 10.4 Add range validation to settings form (timeout, width, height)
- [ ] 10.5 Add range validation to device form (port, width, height)
- [ ] 10.6 Integrate validation errors with flash message system
- [ ] 10.7 Run `task pre-push`

## 11. Schedule Naming Clarity

- [ ] 11.1 Rename `cron` field to `time_range` in Schedule Ent schema
- [ ] 11.2 Run `go generate ./ent`
- [ ] 11.3 Update all Schedule handlers to use `time_range` instead of `cron`
- [ ] 11.4 Update `schedule_form.html` — change field name to `time_range`
- [ ] 11.5 Update `schedules.html` — change table header from "Cron / Time Range" to "Time Range"
- [ ] 11.6 Add migration path: copy existing `cron` values to `time_range` on startup
- [ ] 11.7 Run `task pre-push`

## 12. Security Hardening

- [ ] 12.1 Update `websocket.go` — replace `CheckOrigin: return true` with origin validation against configured device URLs
- [ ] 12.2 Add config for allowed WebSocket origins (device IPs from device settings)
- [ ] 12.3 Update image/video upload handlers to generate unique filenames (UUID + extension)
- [ ] 12.4 Add error checking to `updateTokenURLDS` — log and return flash message on DB error
- [ ] 12.5 Add error checking to `deleteTokenURLDS` — log and return flash message on DB error
- [ ] 12.6 Run `task pre-push`

## 13. Verification

- [ ] 13.1 Run full test suite: `task test` (Go unit tests)
- [ ] 13.2 Run E2E tests: `task test:e2e` (Playwright)
- [ ] 13.3 Run build: `task build`
- [ ] 13.4 Run `task pre-push` for final validation
