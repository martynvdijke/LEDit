## Context

LEDit is a Go-based LED matrix display controller using Gin (HTTP), Ent (ORM), SQLite, and Bootstrap. Currently, all logging uses Go's standard `log.Printf`/`log.Fatalf` with no levels, no persistence, no structure, and no observability export. The admin panel has a sidebar nav with pages for Dashboard, Settings, Schedules, Devices, Theme, Analytics, and Notifications — but no logs page.

The user wants:
1. A central "Logs" tab in the admin panel sidebar
2. Structured, leveled, persistent logging replacing raw `log.Printf`
3. All settings endpoints (email, AI, etc.) instrumented so their operations appear in the log view
4. Logs exported via OpenTelemetry (OTLP)
5. Verbosity control — default shows only warnings and above
6. New Email Settings and AI Settings pages onboarded with logging
7. **Extensive logging of all external dependency calls** — every datasource API call (Sonarr, Radarr, Weather, HomeAssistant, F1, etc.) and every AI provider request logged with start/success/failure visibility
8. **WebSocket event logging** — connection lifecycle and render errors visible in the central log
9. **Logging infrastructure self-logging** — store flush errors, cleanup operations logged through the same system

## Goals / Non-Goals

**Goals:**
- Replace all `log.Printf`/`log.Fatalf` with structured `slog` calls wired to a central sink
- Persist log entries in SQLite via a new `LogEntry` Ent schema
- Provide an admin `/admin/logs` page with filtering by level, source, and search
- Provide an admin `/admin/settings/logs` page for verbosity and retention config (default: warn)
- Export logs to OpenTelemetry via OTLP when configured
- Add `/admin/settings/email` and `/admin/settings/ai` pages with full logging instrumentation
- Add a nav item for Logs in the admin sidebar across all admin templates
- **Add `slog` logging to all 16 datasource `GetPNG()` methods** — log API call start, success, and failure per source
- **Add `slog` logging to WebSocket handler** — replace all `log.Printf` calls
- **Add `slog` logging to logging infrastructure** — replace `log.Printf`/`log.Println` in store.go, cleanup.go, slog.go
- **Add AI provider test connection logging** — log test connection requests, successes, failures with latency

**Non-Goals:**
- Replacing Go's `slog` with a third-party logger (stdlib is sufficient)
- Real-time WebSocket streaming of logs to the UI (static page with refresh is fine for V1)
- Log rotation or file-based logging (DB-based with retention is sufficient)
- Distributed tracing or metrics (OTEL logs only for now)
- Authentication for OTEL export (plain OTLP for now)
- Adding a separate HTTP client middleware layer for logging (inline `slog` calls in each datasource is simpler)
- Refactoring datasource error handling patterns (errors still return fallback renders; just add logging)

## Decisions

### 1. Use `log/slog` (stdlib) over third-party loggers
- **Why**: Go 1.21+ includes `slog` in the standard library — zero dependencies, leveled logging, structured output, and handler abstractions. No need for zap/logrus/zerolog.
- **Alternatives considered**: zap (faster but external dep), logrus (deprecated), zerolog (JSON-only)

### 2. Custom `slog.Handler` writes to both DB and OTEL
- **Why**: A single custom handler implementing `slog.Handler` can write each log entry to both the SQLite `log_entries` table (via a buffered channel + batch inserter) and to the OTEL exporter. This avoids dual-instrumentation.
- **Alternatives considered**: Two separate handlers chained (more complex, risks ordering issues)

### 3. New Ent schema `LogEntry` for persistence
- **Fields**: `id` (int, auto), `timestamp` (time.Time), `level` (string: trace/debug/info/warn/error/fatal), `source` (string, e.g. handler name), `message` (string), `metadata` (JSON string for structured attributes)
- **No edges needed** — logs are standalone, not linked to GeneralSettings
- **Indexed** on `(level, timestamp)` for efficient filtered queries
- **Retention**: Configurable max age (default 7 days) enforced by a background goroutine

### 4. OTEL export via OTLP gRPC/HTTP
- **Why**: OpenTelemetry is the industry standard for observability. OTLP is widely supported (Jaeger, Grafana Tempo, SigNoz, etc.)
- **Library**: `go.opentelemetry.io/otel/log` + `go.opentelemetry.io/otel/exporters/otlp/otlploggrpc` (or otlploghttp)
- **Config**: OTEL endpoint + optional headers stored in settings, disabled by default

### 5. Verbosity stored in a new `LogSettings` table (or GeneralSettings field)
- **Why**: Keeping verbosity separate from GeneralSettings avoids polluting that schema. A dedicated `LogSettings` singleton table (1 row) is cleaner.
- **Default**: `warn` (shows warnings, errors, fatals by default)

### 6. Email and AI settings as new admin pages with logging
- **Why**: These don't exist yet. Building them with `slog` instrumentation from the start means all their operations (test email, AI API calls, config saves) appear in the central log view automatically.
- **Email schema**: `EmailSettings` — host, port, username, password (encrypted), from_address, use_tls
- **AI schema**: `AISettings` — provider (openai/ollama/etc), api_key (encrypted), model, endpoint

### 8. External dependency logging strategy
- **Why**: Currently all 16 datasource implementations silently call external APIs and fall back to placeholder images on failure — no logs, no visibility. The user wants to see every external call in the central log viewer.
- **Approach**: Add `slog.Info`/`slog.Error` calls inline in each datasource's `GetPNG()` method at three points:
  1. **API call start** (`slog.Info`, source=`"<ds-type>"`, attrs: url, name info) — logged before `apiGet`
  2. **API call success** (`slog.Info`, source=`"<ds-type>"`, attrs: response size/status) — after successful fetch
  3. **API call failure** (`slog.Error`, source=`"<ds-type>"`, attrs: error details) — when `apiGet` returns an error or fallback is used
- **Datasource identification**: Each datasource uses its own source tag (e.g., `"sonarr"`, `"weather"`, `"homeassistant"`, `"ai-settings"`) so logs are filterable in the UI
- **AI provider logging**: The AI settings "Test Connection" handler already logs at info/error. Additionally, any future AI provider API calls should log at info level with source `"ai-settings"`, including latency
- **WebSocket logging**: Replace `log.Printf` with `slog.Warn`/`slog.Error` calls, using source `"websocket"` so connection errors appear in the central log
- **Infrastructure self-logging**: Replace `log.Printf`/`log.Println` in `logging/store.go`, `logging/cleanup.go`, and `logging/slog.go` with `slog.Warn`/`slog.Error` calls

### 9. Sidebar nav updated via a shared template partial
- **Why**: Currently the sidebar is duplicated in every admin template. Extracting it into a shared `sidebar.html` partial and using `{{template "sidebar" .}}` avoids touching 11+ files for every nav change.
- **Alternative**: Edit all 11+ templates individually (more error-prone, but simpler initial refactor)

## Risks / Trade-offs

- **[Risk]** DB-backed logging adds write load to SQLite → **Mitigation**: Batch inserts, configurable retention with auto-cleanup, optional async writes with a buffered channel
- **[Risk]** OTEL export adds latency if the exporter is slow → **Mitigation**: Non-blocking send with a small buffer; log locally even if OTEL export fails
- **[Risk]** Extracting the sidebar into a partial touches all admin templates → **Mitigation**: Do this as the first task; it's mechanical and easy to verify
- **[Trade-off]** Using stdlib `slog` means no built-in async handler → We build our own, which is ~100 lines of Go
- **[Trade-off]** Email/AI settings with encrypted secrets require a crypto helper → Already exists in `ent/schema` patterns; minimal new code
- **[Risk]** Adding `slog` calls to 16 datasource files is verbose and repetitive → **Mitigation**: Use a consistent 3-call pattern (start/success/failure); consider extracting a helper if repetition is excessive
- **[Risk]** High-frequency datasource polling could flood the log table → **Mitigation**: Use `slog.Info` for normal operations (respects verbosity filter); noisy datasources can be filtered by source in the UI
- **[Risk]** Websocket handler errors logged via `slog` could create feedback loop → **Mitigation**: WebSocket errors use `slog.Warn`/`slog.Error`, which are safe — the handler has no DB write path that would re-enter the WebSocket
