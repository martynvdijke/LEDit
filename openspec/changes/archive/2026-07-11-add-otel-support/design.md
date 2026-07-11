## Context

LEDit is a Go 1.26 application using the **Gin** router and **SQLite** (`mattn/go-sqlite3`). It currently has a partial OTel setup in `logging/otel.go`: an OTel logs SDK with both `otlploggrpc` and `otlploghttp` exporters and a logger provider. There are no trace exporters, no metric exporters, no HTTP request instrumentation, no DB query tracing, and no service name set. The logs SDK is at v0.20.0 (`otel/log`, `sdk/log`). The stable OTel SDK is at v1.44.0 (`otel`, `sdk`, `trace`, `metric`).

## Goals / Non-Goals

**Goals:**
- Pluggable OTLP exporters (gRPC and HTTP/protobuf) for traces, metrics, and logs
- Gin request tracing via `otelgin` middleware — automatic spans per request
- OTel-native HTTP metrics (request count, duration) exported via OTLP
- DB query tracing — wrap SQLite queries with OTel spans
- Complete the logs pipeline with verified slog bridge for log-to-trace correlation
- Set service name default to `ledit` via `OTEL_SERVICE_NAME`
- Standard OTel env var support: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_SAMPLER`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_SERVICE_NAME`
- Graceful degradation: no OTel config → noop, partial failure → warn + fallback
- Unit tests for telemetry init and integration test for exporter configuration
- All existing tests pass, CI stays green

**Non-Goals:**
- Not replacing any existing logging infrastructure — OTel logs are additive
- Not instrumenting every individual handler (Gin middleware covers the request lifecycle; DB tracing covers queries)
- Not adding OTel auto-instrumentation agents or sidecars
- Not changing the Dockerfile — OTel config is env-var driven

## Decisions

**Decision 1: OTLP gRPC as primary exporter, HTTP/protobuf as secondary**

Both `otlptracegrpc`/`otlpmetricgrpc`/`otlploggrpc` and `otlptracehttp`/`otlpmetrichttp`/`otlploghttp` will be supported. The protocol is selected via `OTEL_EXPORTER_OTLP_PROTOCOL` (default: `grpc`).

Rationale: gRPC is the default OTel protocol and the most efficient for high-throughput. HTTP/protobuf is useful when gRPC is blocked. Supporting both adds minimal binary cost. The logs pipeline already has both gRPC and HTTP exporters.

Alternative considered: Only HTTP for logs (already present), gRPC for traces/metrics. Rejected: consistency across all three pillars is cleaner.

**Decision 2: otelgin middleware for request tracing**

Use `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin` middleware.

Rationale: otelgin automatically creates spans with HTTP semantic convention attributes, handles trace context propagation via `traceparent` headers, and sets span status on errors.

Trade-off: Adds a dependency on `contrib/instrumentation`. The contrib module is maintained by the OTel project and has compatibility guarantees.

**Decision 3: OTel metrics via OTLP export, no Prometheus bridge needed**

LEDit has no existing Prometheus metrics, so OTel metric instruments will be exported directly via OTLP. No Prometheus exporter is needed.

Rationale: Without existing Prometheus scrapers to preserve, OTLP is the cleaner path — single exporter, single endpoint, no format bridging.

Alternative considered: Add OTel Prometheus exporter for future flexibility. Rejected: YAGNI.

**Decision 4: DB query tracing via helper function wrapper**

Create a DB query tracing helper with a `TraceDBQuery(ctx, operation, dbFunc)` function that wraps a SQLite query in an OTel span.

Rationale: A wrapper function allows per-query opt-in without touching every call site at once. Key queries will be wrapped first.

**Decision 5: Config via standard OTel env vars only**

The app relies on the Go OTel SDK's automatic env var detection. Do NOT duplicate OTEL_* vars in app config.

Rationale: The OTel SDK already reads all standard env vars. Duplicating this is unnecessary and risks drift from the spec.

**Decision 6: Extend logging/otel.go to a full telemetry init**

- `logging/otel.go`: extended to initialize tracer, meter, and logger providers in a single `initTelemetry()` function
- `main.go`: add `otelgin.Middleware()` to the Gin router, update `initTelemetry()` call and shutdown

Rationale: The existing otel.go is the natural home for all OTel initialization. Consolidating all three pillars in one place keeps concerns clean.

**Decision 7: Complete logs pipeline with slog bridge**

Wire the OTel slog bridge so slog log records flow through the OTel logs SDK with automatic trace context injection. The log exporters (`otlploggrpc`, `otlploghttp`) are already present.

Rationale: Bridging slog to OTel logs provides log-to-trace correlation without changing every log call site.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| OTLP exporter connection blocks startup | Move exporter connection to background goroutine with timeout; server starts with noop fallback |
| `otelgin` middleware version compatibility | Pin to same minor version as OTel SDK (v1.44.x / contrib v0.69.x) using go.mod |
| DB query tracing adds overhead to every query | No overhead when no exporter is registered; sampling reduces overhead in production |
| Logs SDK is still v0.20.0 (unstable) | Pin version explicitly; API may change in future |
| No service name currently set | Add `OTEL_SERVICE_NAME` default "ledit" in resource detection |

## Open Questions

- Should we add a health check endpoint for the OTel exporter? — Deferred
- Should `logging/otel.go` be renamed to `logging/telemetry.go` to reflect its broader scope? — Consider during implementation