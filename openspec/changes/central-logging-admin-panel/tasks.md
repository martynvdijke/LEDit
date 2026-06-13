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

## 7. Replace Remaining log.Printf Calls

- [x] 7.1 Replace `log.Printf`/`log.Fatalf` in `handlers/server.go` with `slog` calls
- [x] 7.2 Replace `log.Printf`/`log.Fatalf` in `main.go` with `slog` calls
- [x] 7.3 Replace `log.Printf` in `handlers/websocket.go` with `slog.Warn`/`slog.Error` calls (source="websocket")
- [x] 7.4 Replace `log.Println`/`log.Printf` in `logging/store.go` with `slog.Warn`/`slog.Error` calls (source="logging")
- [x] 7.5 Replace `log.Printf` in `logging/cleanup.go` with `slog.Info`/`slog.Error` calls (source="logging")
- [x] 7.6 Replace `log.Println` in `logging/slog.go` with `slog.Info` call (source="logging")

## 8. Admin Log Viewer Page

- [x] 8.1 Create `handlers/log_admin.go` with `AdminLogs` handler and `AdminLogsAPI` JSON endpoint
- [x] 8.2 Create `web/templates/admin/logs.html` — log viewer UI with table, filters, severity badges, pagination
- [x] 8.3 Register route `GET /admin/logs` and `GET /admin/api/logs` in `handlers/server.go`

## 9. Admin Log Settings Page

- [x] 9.1 Create `AdminLogSettings` (GET) and `AdminLogSettingsSave` (POST) in `handlers/log_admin.go`
- [x] 9.2 Create `web/templates/admin/log_settings.html` — verbosity selector, retention field, OTEL config
- [x] 9.3 Register routes `GET /admin/settings/logs` and `POST /admin/settings/logs`

## 10. Admin Email Settings Page

- [x] 10.1 Create `AdminEmailSettings` and `AdminEmailSettingsSave` handlers in `handlers/log_admin.go`
- [x] 10.2 Create `web/templates/admin/email_settings.html` — SMTP configuration form
- [x] 10.3 Register email settings routes under `/admin/settings/email`
- [ ] 10.4 Wire EmailSettings edge into AdminDashboard query in `handlers/handlers.go` (include `.WithEmailSettings()`)

## 11. Admin AI Settings Page

- [x] 11.1 Create `AdminAISettings` and `AdminAISettingsSave` handlers in `handlers/log_admin.go`
- [x] 11.2 Create `web/templates/admin/ai_settings.html` — AI provider config form with provider dropdown
- [x] 11.3 Register AI settings routes under `/admin/settings/ai`
- [ ] 11.4 Wire AISettings edge into AdminDashboard query in `handlers/handlers.go` (include `.WithAISettings()`)

## 12. OpenTelemetry Integration

- [x] 12.1 Add `go.opentelemetry.io/otel` and OTLP exporter dependencies to `go.mod`
- [x] 12.2 Implement OTEL log exporter init in `logging/otel.go` (reads config from LogSettings)
- [x] 12.3 Map internal log levels to OTEL severity numbers
- [x] 12.4 Handle exporter errors gracefully (log locally, don't crash)

## 13. Datasource API Call Logging (Token/URL datasources)

- [x] 13.1 Add slog instrumentation to `datasource/sonarr.go` — log API call start, success (series count), failure, fallback
- [x] 13.2 Add slog instrumentation to `datasource/radarr.go` — same pattern
- [x] 13.3 Add slog instrumentation to `datasource/f1.go` — same pattern
- [x] 13.4 Add slog instrumentation to `datasource/weather.go` — same pattern, include location
- [x] 13.5 Add slog instrumentation to `datasource/homeassistant.go` — same pattern, include entity count on success
- [x] 13.6 Add slog instrumentation to `datasource/untappd.go` — same pattern
- [x] 13.7 Add slog instrumentation to `datasource/crypto.go` — same pattern
- [x] 13.8 Add slog instrumentation to `datasource/stock.go` — same pattern

## 14. Datasource File/Feed/Text Logging

- [x] 14.1 Add slog instrumentation to `datasource/rssfeed.go` — log fetch start, entry count, failure
- [x] 14.2 Add slog instrumentation to `datasource/calendar.go` — log fetch start, event count, failure
- [x] 14.3 Add slog instrumentation to `datasource/image.go` — log file read, path, failure
- [x] 14.4 Add slog instrumentation to `datasource/video.go` — log file read, path, failure
- [x] 14.5 Add slog instrumentation to `datasource/textslide.go` — log render start/success
- [x] 14.6 Add slog instrumentation to `datasource/systemstats.go` — log stats collection

## 15. AI Provider Test Connection Logging

- [x] 15.1 Add "Test Connection" button and handler to AI settings page that attempts to connect to the AI provider
- [x] 15.2 Log test connection start at info level with source "ai-settings" (provider, model, endpoint attrs)
- [x] 15.3 Log test connection success at info level with source "ai-settings" (latency attr)
- [x] 15.4 Log test connection failure at error level with source "ai-settings" (error, latency attrs)

## 16. Instrumentation & Verification

- [x] 16.1 Verify all datasource log entries appear in central log view by exercising each datasource
- [x] 16.2 Verify WebSocket errors appear in logs (connect and disconnect)
- [x] 16.3 Verify logging infrastructure self-logs appear (store flush, cleanup, init)
- [x] 16.4 Verify verbosity filtering works (change level to "error", confirm warn entries hidden)
- [x] 16.5 Run `task pre-push` (gofmt, tests, build) and fix any issues
