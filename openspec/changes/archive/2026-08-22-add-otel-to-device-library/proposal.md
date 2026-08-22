## Why

The LEDit server already ships full OpenTelemetry support — traces, metrics, and log export via OTLP (`logging/otel.go`), configured through standard `OTEL_*` env vars — so operators can observe the server in a single backend. The Python device library (`device/ledit_device`) is the only un-instrumented surface: on-device behavior (reconnects, frame rendering, display errors) is invisible to the same telemetry backend, leaving a blind spot for the actual display hardware. Adding OTel to the Python library closes that gap with the same conventions the server uses.

## What Changes

- Add a `telemetry` module to `ledit_device` that initialises a full OTel pipeline (traces, metrics, logs) from standard `OTEL_*` environment variables, with graceful no-op degradation when no `OTEL_EXPORTER_OTLP_ENDPOINT` is set — mirroring `logging/otel.go` on the Go side.
- Route the device's stderr logging (`config.log`) through OTel so logs flow to the OTLP backend with trace context correlation.
- Instrument the WebSocket lifecycle and frame rendering with spans (connect, reconnect, render image, render text, invalid frame).
- Emit device metrics: frames rendered, connection errors, reconnects, uptime.
- Add `opentelemetry-*` dependencies to `device/pyproject.toml`.
- Add unit tests for telemetry init (endpoint present/absent), graceful shutdown, and no-panic no-op behaviour.

## Capabilities

### New Capabilities
- `device-telemetry`: OpenTelemetry instrumentation of the Python device library — env-var-driven OTLP init (traces, metrics, logs), graceful no-op when disabled, log-to-trace correlation, spans for WebSocket lifecycle and frame rendering, and device metrics.

### Modified Capabilities
- *(none)* — `openspec/specs/` contains no existing capability specs; the device library's current behaviour is implementation-only.

## Impact

- **New module**: `device/ledit_device/telemetry.py` (init/shutdown/helpers, mirroring `logging/otel.go`).
- **Modified**: `device/ledit_device/config.py` (log bridge), `device/ledit_device/client.py` (spans + metrics), `device/ledit_device/__main__.py` (init/shutdown wiring), `device/pyproject.toml` (deps), `device/README.md` (env docs).
- **Dependencies**: `opentelemetry-api`, `opentelemetry-sdk`, `opentelemetry-exporter-otlp-proto-grpc` (default protocol), `opentelemetry-exporter-otlp-proto-http` (for `http/protobuf` protocol). Device-side only; no Go/server impact.
- **Tests**: new `device/tests/test_telemetry.py`.
- **No server changes**: the Go side is untouched; this mirrors its existing conventions.
