## Why

LEDit currently has a partial OTel setup: an OTel logs SDK (`otlploggrpc` and `otlploghttp`) wired through `logging/otel.go`, but no trace or metric export pipeline. In production there is zero visibility into request latency, error rates, or dependency performance — no distributed traces, no OTel-native metrics. The logs pipeline exists but lacks trace context correlation (no slog bridge verified).

Adding full OTel support — traces, metrics, and logs — with OTLP export unlocks distributed tracing, richer metrics, and structured log correlation with observability backends (Grafana Tempo/Loki, Jaeger, SigNoz, etc.) without vendor lock-in. All three pillars aligned on a single OTLP endpoint simplifies the collector story.

## What Changes

- **Add OTLP trace exporters** (gRPC and HTTP/protobuf) for production-grade trace ingestion
- **Add `otelgin` middleware** to automatically create spans for every HTTP request with method, path, and status code attributes
- **Add OTel metrics** — OTel SDK metric instruments for HTTP request count (`otel_http_requests_total`) and duration (`otel_http_request_duration_seconds`), exported via OTLP
- **Add DB query tracing** — instrument SQLite queries with OTel spans to capture DB latency in traces
- **Complete the logs pipeline** — ensure slog-to-OTel log bridge is wired for log-to-trace correlation (otlploggrpc/http already present)
- **Set service name** — default `OTEL_SERVICE_NAME` to `ledit`
- **Add configurable sampling and resource attributes** — support `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`, `OTEL_RESOURCE_ATTRIBUTES` env vars
- **Graceful degradation** — if OTel is not configured (no OTLP endpoint), fall back to no-op propagation without crashing
- **Tests** — unit tests for telemetry initialization and middleware, integration test verifying trace/metric/log export configuration

## Capabilities

### New Capabilities
- `otel-telemetry`: OpenTelemetry-based distributed tracing, metrics, and logs with configurable OTLP export, Gin request instrumentation, and DB query tracing

### Modified Capabilities
<!-- No existing capabilities are having their requirements changed -->

## Impact

- `go.mod`: add `go.opentelemetry.io/otel/exporters/otlp/otlptrace`, `otlptracegrpc`, `otlptracehttp`, `go.opentelemetry.io/otel/exporters/otlp/otlpmetric`, `otlpmetricgrpc`, `otlpmetrichttp`, `go.opentelemetry.io/otel/sdk/metric`, `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin`
- `logging/otel.go`: extend to initialize tracer and meter providers alongside the existing logger provider
- `main.go`: integrate `otelgin` middleware, update telemetry initialization and shutdown
- New file for DB query tracing helper
- New env vars: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_TRACES_SAMPLER`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`
- `docker-compose.yml`: add `OTEL_*` env vars, document collector endpoint
- CI: no pipeline changes needed — OTel is a pure code addition