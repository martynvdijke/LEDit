## 1. Shared Sidebar Partial

- [x] 1.1 Extract the admin sidebar nav into `web/templates/admin/sidebar.html` template partial
- [x] 1.2 Update all existing admin templates to use `{{template "sidebar" .}}` instead of inline nav
- [x] 1.3 Add "Logs" nav item to the shared sidebar partial (`/admin/logs`)

## 2. Database Schema — LogEntry

- [x] 2.1 Create `ent/schema/logentry.go` with fields: id (int), timestamp (time.Time), level (string), source (string), message (string), metadata (JSON string)
- [x] 2.2 Add database index on `(level, timestamp)` for filtered queries
- [x] 2.3 Run `go generate ./ent` to regenerate Ent client code

## 3. Database Schema — LogSettings

- [x] 3.1 Create `ent/schema/logsettings.go` with fields: id (int), verbosity (string, default "warn"), retention_days (int, default 7), otel_endpoint (string), otel_protocol (string), otel_enabled (bool)
- [x] 3.2 Run `go generate ./ent` to regenerate Ent client code

## 4. Database Schema — EmailSettings

- [x] 4.1 Create `ent/schema/emailsettings.go` with fields: id, host, port, username, password (encrypted), from_address, use_tls
- [x] 4.2 Add edge from GeneralSettings to EmailSettings
- [x] 4.3 Run `go generate ./ent` to regenerate Ent client code

## 5. Database Schema — AISettings

- [x] 5.1 Create `ent/schema/aisettings.go` with fields: id, provider, api_key (encrypted), model, endpoint
- [x] 5.2 Add edge from GeneralSettings to AISettings
- [x] 5.3 Run `go generate ./ent` to regenerate Ent client code

## 6. Central Structured Logging System

- [x] 6.1 Create `logging/slog.go` — custom `slog.Handler` that writes to both DB (buffered channel) and OTEL exporter
- [x] 6.2 Create `logging/store.go` — buffered batch inserter for LogEntry persistence
- [x] 6.3 Create `logging/otel.go` — OTEL log exporter integration (disabled when no endpoint configured)
- [x] 6.4 Create `logging/cleanup.go` — background goroutine for retention-based log cleanup
- [x] 6.5 Create `logging/levels.go` — helper functions for slog level <-> string conversion, verbosity filtering
- [x] 6.6 Wire the logging system into the Server on startup (`handlers/server.go`)

## 7. Replace Existing log.Printf Calls

- [x] 7.1 Replace `log.Printf`/`log.Fatalf` in `handlers/server.go` with `slog` calls
- [x] 7.2 Replace `log.Printf` in `handlers/websocket.go` with `slog` calls
- [x] 7.3 Replace `log.Printf`/`log.Fatalf` in `main.go` with `slog` calls
- [x] 7.4 Replace `log.Printf`/`log.Fatalf` in any other Go files

## 8. Admin Log Viewer Page

- [ ] 8.1 Create `handlers/logs.go` with `AdminLogs` handler: query LogEntry with level/source/search/pagination filters
- [ ] 8.2 Create `web/templates/admin/logs.html` — log viewer UI with table, filters, severity badges, pagination
- [ ] 8.3 Register route `GET /admin/logs` in `handlers/server.go`
- [ ] 8.4 Add query helper methods on the Server for filtered log queries

## 9. Admin Log Settings Page

- [ ] 9.1 Create `handlers/log_settings.go` with `AdminLogSettings` (GET) and `AdminLogSettingsSave` (POST)
- [ ] 9.2 Create `web/templates/admin/log_settings.html` — verbosity selector, retention field, OTEL config
- [ ] 9.3 Register routes `GET /admin/settings/logs` and `POST /admin/settings/logs`

## 10. Admin Email Settings Page

- [ ] 10.1 Create `handlers/email_settings.go` with CRUD handlers for EmailSettings
- [ ] 10.2 Create `web/templates/admin/email_settings.html` — SMTP configuration form with "Test Email" button
- [ ] 10.3 Register email settings routes under `/admin/settings/email`
- [ ] 10.4 Wire EmailSettings edge into AdminDashboard query in `handlers/handlers.go`

## 11. Admin AI Settings Page

- [ ] 11.1 Create `handlers/ai_settings.go` with CRUD handlers for AISettings
- [ ] 11.2 Create `web/templates/admin/ai_settings.html` — AI provider config form with "Test Connection" button
- [ ] 11.3 Register AI settings routes under `/admin/settings/ai`
- [ ] 11.4 Wire AISettings edge into AdminDashboard query in `handlers/handlers.go`

## 12. OpenTelemetry Integration

- [x] 12.1 Add `go.opentelemetry.io/otel` and OTLP exporter dependencies to `go.mod`
- [x] 12.2 Implement OTEL log exporter init in `logging/otel.go` (reads config from LogSettings)
- [x] 12.3 Map internal log levels to OTEL severity numbers
- [x] 12.4 Handle exporter errors gracefully (log locally, don't crash)

## 13. Instrumentation & Verification

- [ ] 13.1 Verify all log entries flow through central system by exercising settings endpoints (email, AI)
- [ ] 13.2 Verify verbosity filtering works (change level to "error", confirm warn entries hidden)
- [ ] 13.3 Verify OTEL export works (configure endpoint, confirm logs arrive)
- [ ] 13.4 Run `task pre-push` (gofmt, tests, build) and fix any issues
