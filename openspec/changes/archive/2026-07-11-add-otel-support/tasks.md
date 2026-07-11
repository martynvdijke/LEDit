## 1. Add OTel Dependencies

- [x] 1.1 Add `go.opentelemetry.io/otel/exporters/otlp/otlptrace`, `otlptracegrpc`, and `otlptracehttp` for OTLP trace exporter support
- [x] 1.2 Add `go.opentelemetry.io/otel/exporters/otlp/otlpmetric`, `otlpmetricgrpc`, and `otlpmetrichttp` for OTLP metric exporter support
- [x] 1.3 Add `go.opentelemetry.io/otel/sdk/metric` for the OTel meter provider
- [x] 1.4 Add `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin` for Gin request tracing
- [x] 1.5 Add OTel slog bridge dependency for log-to-trace correlation (otlploggrpc/http already present)
- [x] 1.6 Run `go mod tidy` to resolve all new dependencies

## 2. Extend logging/otel.go — Pluggable Exporter Setup

- [x] 2.1 Extend `initTelemetry()` to initialize tracer, meter, and logger providers
- [x] 2.2 Select exporter protocol based on `OTEL_EXPORTER_OTLP_PROTOCOL` (default: `grpc`), supporting both gRPC and HTTP/protobuf for all three pillars
- [x] 2.3 Configure `OTEL_TRACES_SAMPLER` and `OTEL_TRACES_SAMPLER_ARG` via OTel SDK sampler
- [x] 2.4 Configure `OTEL_RESOURCE_ATTRIBUTES` via OTel SDK resource detection, with `OTEL_SERVICE_NAME` defaulting to `ledit`
- [x] 2.5 Add graceful shutdown: `defer tp.Shutdown()` with timeout, flush pending spans/metrics/logs
- [x] 2.6 Add graceful degradation: if OTLP exporter connection fails, log warning and fall back to noop

## 3. Add OTel Metrics

- [x] 3.1 Create OTel meter and instruments for HTTP request count (`otel_http_requests_total`) and duration (`otel_http_request_duration_seconds`) with method/path/status labels
- [x] 3.2 Initialize OTel meter provider with OTLP exporter
- [x] 3.3 Expose metrics via OTLP export (no Prometheus endpoint needed)

## 4. Integrate Gin Request Tracing

- [x] 4.1 Add `otelgin.Middleware("ledit")` to the Gin router in `main.go` — before existing middleware but after Recovery
- [x] 4.2 Verify trace context propagation from incoming `traceparent` headers

## 5. Add DB Query Tracing

- [x] 5.1 Create a DB query tracing helper with `TraceDBQuery(ctx, operation, dbFunc)` function
- [x] 5.2 Wrap key DB queries with tracing spans
- [x] 5.3 Ensure spans link to parent request trace via context propagation

## 6. Complete Logs Pipeline

- [x] 6.1 Verify `otlploggrpc` and `otlploghttp` exporters are both wired in the logger provider
- [x] 6.2 Wire the OTel slog bridge so slog log records flow through the OTel logs SDK with trace context
- [x] 6.3 Verify log-to-trace correlation: logs emitted within a span include trace_id and span_id

## 7. Write Tests

- [x] 7.1 Write unit tests for `initTelemetry()`: OTLP endpoint config, noop fallback, sampling config, resource attributes
- [x] 7.2 Write unit test for DB query tracing helper
- [x] 7.3 Write integration test that starts the server with OTel env vars and verifies metrics are exported
- [x] 7.4 Write test that verifies graceful degradation (unreachable OTLP endpoint doesn't crash server)

## 8. Docker & Verification

- [x] 8.1 Update `docker-compose.yml`: add `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`
- [x] 8.2 Run `go vet ./...` — no new warnings
- [x] 8.3 Run `go test ./...` — all tests pass
- [x] 8.4 Run `go build -o /dev/null .` — binary compiles cleanly
- [x] 8.5 Commit all changes with a conventional commit message