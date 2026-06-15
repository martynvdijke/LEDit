## Why

LEDit currently has no centralized logging or observability. Application errors and warnings are scattered across `log.Printf` calls with no persistence, no severity levels, and no way to view them in the UI. Operators have no visibility into what's happening with their display system — failures in datasource fetching, device communication, or scheduled tasks go unnoticed until something breaks visibly. Adding a central logging view with OpenTelemetry export gives operators real-time insight and a historical audit trail.

## What Changes

- **New admin "Logs" tab** in the sidebar navigation showing a real-time, filterable log view
- **Structured logging system** replacing raw `log.Printf` with leveled, structured log entries (trace/debug/info/warn/error/fatal)
- **Log persistence** — logs stored in SQLite via a new Ent schema/model so they survive restarts
- **Log verbosity control** — admin can set the minimum log level displayed (default: `warn`)
- **OpenTelemetry export** — logs are also exported via OTLP so they appear in external observability backends
- **Onboarding existing settings endpoints** — email, AI, and other setting endpoints get instrumented so their operations appear in the central log view
- **New email and AI settings sub-pages** in the admin panel, with logging built in from the start
- **Extensive external dependency logging** — all datasource fetches (Sonarr, Radarr, Weather, HomeAssistant, Untappd, Crypto, Stock, RSS, Calendar, F1, Images, Videos, Text Slides) are instrumented with `slog` calls at info/error level, capturing API call starts, successes, failures, and fallback renders
- **AI provider call logging** — all AI/LLM API calls (OpenAI, Anthropic, Ollama, etc.) are logged end-to-end: request initiation, response receipt, errors, and latency
- **WebSocket event logging** — connection lifecycle, source rendering errors, and feed control events are logged via `slog` instead of raw `log.Printf`
- **Logging infrastructure self-logging** — store flush errors, cleanup operations, and queue drops are logged via `slog` instead of raw `log.Printf`

## Capabilities

### New Capabilities
- `log-viewer`: Real-time, filterable log viewing page in the admin panel with severity badges, source filtering, and search
- `log-settings`: Verbosity level configuration (default: warn) and retention policy settings
- `otel-export`: OpenTelemetry OTLP export of all structured logs
- `email-settings`: SMTP/email configuration page in admin, fully instrumented with logging
- `ai-settings`: AI/LLM integration settings page in admin, fully instrumented with logging
- `external-dependency-logging`: Comprehensive `slog` instrumentation of all datasource API calls (Sonarr, Radarr, Weather, HA, etc.), AI provider calls, WebSocket events, and logging infrastructure internals

### Modified Capabilities
- *(none — no existing specs to modify)*

## Impact

- **New Go dependencies**: `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/exporters/otlp` for OTEL; `log/slog` (stdlib in Go 1.21+) for structured logging
- **New Ent schema**: `LogEntry` table for persisted logs with fields: timestamp, level, source, message, metadata
- **New handlers**: `AdminLogs` (view), `AdminLogSettings` (verbosity config), `AdminEmailSettings`, `AdminAISettings`
- **New templates**: `logs.html`, `log_settings.html`, `email_settings.html`, `ai_settings.html`
- **Sidebar changes**: All admin templates need the new "Logs" nav item added; "Settings" may be reorganized into sub-pages
- **Database migration**: New `log_entries` table, new fields on `GeneralSettings` or a new `LogSettings` table for verbosity level
- **Existing code touched**: All `log.Printf`/`log.Fatalf` calls in `websocket.go`, `logging/store.go`, `logging/cleanup.go`, `logging/slog.go` replaced with `slog` equivalents
- **Datasource instrumentation**: All 16 datasource files (`datasource/*.go`) get `slog` calls on API call start, success, failure, and fallback render
- **AI provider calls**: AI settings "Test Connection" and any provider API calls logged with request/response details and latency
- **WebSocket logging**: Connection upgrade, settings load, source render errors, and write errors logged via `slog`
