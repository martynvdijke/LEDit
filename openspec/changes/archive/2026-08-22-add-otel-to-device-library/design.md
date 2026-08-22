## Context

LEDit is a Go (Gin + Ent/SQLite) server that renders datasources into pixel images and streams them over WebSocket. The Go side already has a full OpenTelemetry pipeline (`logging/otel.go`): traces, metrics, and log export via OTLP (gRPC by default, HTTP `http/protobuf` optional), configured exclusively through standard `OTEL_*` environment variables, with graceful no-op degradation when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset. Logs and traces are correlated by bridging `slog` into the OTel logger provider.

The Python device library (`device/ledit_device`, package `ledit-device`) is a small WebSocket client that connects out to the server (`/ws/device/<token>`), decodes PNG frames, and renders them onto an `rpi-rgb-led-matrix` panel. It logs to stderr via a tiny `config.log(level, msg)` helper and has no telemetry of any kind. The device is where display hardware actually lives, so its connection and rendering health is exactly what operators most want to observe — but today it is invisible to the OTel backend the server already exports to.

The library must stay small (a stated project constraint) and remain runnable on a Raspberry Pi Zero. `pyproject.toml` currently pins `requires-python = ">=3.8"`; Python OTel SDK supports 3.8+.

## Goals / Non-Goals

**Goals:**
- A `telemetry` module in `ledit_device` that initialises traces, metrics, and log export from standard `OTEL_*` env vars, mirroring `logging/otel.go` semantics (service name, resource attributes, sampler, protocol).
- Graceful no-op when `OTEL_EXPORTER_OTLP_ENDPOINT` is not set — zero new failure modes, no required config.
- Log-to-trace correlation: device stderr logs flow to the OTLP backend with the active trace context.
- Spans around WebSocket lifecycle and frame rendering (connect, reconnect, render image, render text, invalid frame).
- Device metrics: frames rendered, connection errors, reconnects.
- Shutdown that flushes providers on exit.
- Unit tests covering enabled/disabled init and no-panic no-op behaviour.

**Non-Goals:**
- Instrumenting the Go server — it already has full OTel; this change only touches the Python library.
- Supporting every OTLP protocol variant — only `grpc` (default) and `http/protobuf`, matching the server's supported set.
- Replacing the existing stderr `config.log` output — OTel log export is additive; stderr logging stays as-is.
- Metrics beyond the small device-relevant set (frames, connection errors, reconnects).
- Running a full OTel collector in tests — tests verify init/shutdown/no-op behaviour without network I/O.

## Decisions

### 1. Mirror `logging/otel.go` env-var semantics

- **Decision**: The Python `telemetry` module reads the same standard env vars the Go side uses: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc` default, `http/protobuf` supported), `OTEL_SERVICE_NAME` (default `ledit-device`), `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG`. If `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, `init_telemetry()` returns a disabled no-op instance.
- **Rationale**: Operators already configure the server this way; one mental model across the fleet. The official Python OTel SDK exposes the same semantics (`resource.get_otel_asgi_default`-style env handling via `Resource.create(attributes, telemetry_sdk.OTEL_RESOURCE_ATTRIBUTES)` / `sdk.trace.sampling` from env).
- **Alternatives considered**: A bespoke YAML/TOML config was rejected — it would diverge from the server and add a config surface for zero benefit.

### 2. Dependencies: `opentelemetry-api`, `opentelemetry-sdk`, plus OTLP exporters

- **Decision**: Add to `device/pyproject.toml` dependencies:
  - `opentelemetry-api>=1.20`
  - `opentelemetry-sdk>=1.20`
  - `opentelemetry-exporter-otlp-proto-grpc>=1.20` (default protocol)
  - `opentelemetry-exporter-otlp-proto-http>=1.20` (used when `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`)
  - `opentelemetry-instrumentation-logging` (optional) — or implement a small custom log handler so log records carry trace context.
- **Rationale**: The `opentelemetry-exporter-otlp` meta-package pulls both gRPC and HTTP exporters; including both explicitly keeps the dependency list predictable. Custom handler vs. `opentelemetry-instrumentation-logging`: the instrumentation package's `LoggingInstrumentor` adds the currently-active span context to `logging` records and is the maintained path for correlation.
- **Alternatives considered**: Only shipping gRPC was rejected because a Pi Zero on a LAN may reach a collector via HTTP only, and the server already supports both. Shipping only the `opentelemetry-exporter-otlp` meta-package was noted as equivalent; explicit deps are clearer.

### 3. Log bridge via Python `logging`, not direct stderr interception

- **Decision**: Route `config.log(level, msg)` through a module-level `logging.Logger`. When telemetry is enabled, install a `LoggingHandler` (from `opentelemetry-instrumentation-logging`) that emits OTel log records with the active span context; stderr output remains via the existing handler so nothing visible changes.
- **Rationale**: `config.log` is the single choke point all device logs pass through; converting it to use the stdlib `logging` module gives OTel correlation without touching every call site (`client.py`, `display.py`, `__main__.py`).
- **Alternatives considered**: Wrapping `config.log` to call the OTel `LoggerProvider` directly was rejected — it would bypass stdlib `logging`, complicate testability, and duplicate correlation logic the instrumentation package already provides.

### 4. Instrumentation scope: WebSocket lifecycle + rendering spans

- **Decision**: Add spans in `client.py`:
  - `on_message` → span per received message (name `device.message.received`), covering decode + render.
  - `render_image` / `render_text` → nested spans (`device.render.image`, `device.render.text`).
  - Connection errors / reconnect → span `device.connection.error` (error status) and `device.connection.reconnect`.
- **Rationale**: Frames arrive only every `refresh_interval` (default 60s), so per-message spans are cheap and precisely answer "is this device rendering and why not".
- **Alternatives considered**: Tracing every WebSocket byte or ping was rejected as noise; the server only needs lifecycle + render visibility.

### 5. Metrics: small device-relevant set

- **Decision**: On the device meter (`ledit-device`), register:
  - `device.frames_rendered_total` (counter, attributes: image/text)
  - `device.connection_errors_total` (counter)
  - `device.reconnects_total` (counter)
- **Rationale**: These three answer the operational questions for unattended hardware: is it rendering, is it stable.
- **Alternatives considered**: CPU/memory gauges via `psutil` were rejected (extra dep on a constrained Pi, not core to display health).

### 6. Shutdown flushes on exit

- **Decision**: `Telemetry.shutdown()` shuts down trace/metric/log providers (flushing batch spans). `__main__.py` calls `init_telemetry()` before the connect loop and `telemetry.shutdown()` in a `finally`/`atexit` so spans flush on Ctrl-C and on reconnect-loop exit.
- **Rationale**: Batch span processors lose data if the process dies without a flush; the device process is long-running, so an explicit shutdown path is required.
- **Alternatives considered**: Relying solely on interpreter `atexit` was rejected — explicit wiring in `main()` makes the flush deterministic and testable.

## Risks / Trade-offs

- **Pi Zero resource use** (SDK threads for batch export) → Mitigation: OTel SDK's batch processors are lightweight; frames arrive ~1/min; telemetry is entirely disabled unless an endpoint is configured, so devices without a collector pay nothing.
- **Dependency weight on a constrained device** → Mitigation: only the four OTel packages plus their transitive deps (grpcio, protobuf); acceptable for a 64-bit Pi Zero 2; documented in README. Native gRPC wheels exist for ARM.
- **Python >=3.8 compatibility** → Mitigation: pin `>=1.20` OTel versions, which support 3.8; verify in CI/test run.
- **New failure modes from instrumentation** → Mitigation: all instrumentation wraps in try/except so a telemetry error never breaks rendering; no-op when disabled.
- **OTLP endpoint unreachable** → Mitigation: batch exporters buffer and retry; failures log a warning, rendering is unaffected.

## Migration Plan

1. Add OTel deps to `device/pyproject.toml`.
2. Add `device/ledit_device/telemetry.py` (init/shutdown/provider/helper, mirroring `logging/otel.go`).
3. Rework `config.log` onto stdlib `logging` + OTel handler wiring.
4. Instrument `client.py` (spans, metrics) and wire init/shutdown in `__main__.py`.
5. Add `device/tests/test_telemetry.py`; update README with `OTEL_*` env docs.
6. Run the Python tests and `task pre-push`.

Rollback: additive change confined to the Python library; `git revert` suffices. Devices without `OTEL_EXPORTER_OTLP_ENDPOINT` are unaffected (no-op).

## Open Questions

- Should the device also emit a periodic heartbeat metric/span (e.g., every `refresh_interval`) so operators can detect a wedged device that is still connected but not rendering? (Deferred — frames-rendered counter covers most cases.)
- Is `opentelemetry-instrumentation-logging` acceptable as a dependency, or should the log handler be hand-rolled to keep the dep list minimal?
