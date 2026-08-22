## 1. Dependencies

- [x] 1.1 Add `opentelemetry-api`, `opentelemetry-sdk`, `opentelemetry-exporter-otlp-proto-grpc`, `opentelemetry-exporter-otlp-proto-http`, and `opentelemetry-instrumentation-logging` to `device/pyproject.toml` (Python >=3.8 compatible versions)

## 2. Telemetry module

- [x] 2.1 Add `device/ledit_device/telemetry.py` with `Telemetry` class mirroring `logging/otel.go`: env-var-driven init (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, sampler vars), disabled no-op when no endpoint, provider getters, and `shutdown()` that flushes all providers
- [x] 2.2 Add `init_telemetry()` helper returning a disabled instance when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, and an enabled instance (service name default `ledit-device`) otherwise
- [x] 2.3 Support both `grpc` (default) and `http/protobuf` OTLP protocols in exporter selection

## 3. Log bridge

- [x] 3.1 Rework `device/ledit_device/config.py` `log()` to use a module-level stdlib `logging.Logger` so stderr output is unchanged
- [x] 3.2 Wire the OTel log handler (with trace-context correlation) into the logger when telemetry is enabled

## 4. Instrumentation

- [x] 4.1 Instrument `device/ledit_device/client.py`: span per received message, nested spans for `render_image`/`render_text`, error span on connection error
- [x] 4.2 Add metrics on the `ledit-device` meter: `device.frames_rendered_total` (image/text attribute), `device.connection_errors_total`, `device.reconnects_total`
- [x] 4.3 Wire `init_telemetry()` before the connect loop and `telemetry.shutdown()` on exit in `device/ledit_device/__main__.py`

## 5. Tests & docs

- [x] 5.1 Add `device/tests/test_telemetry.py`: disabled when no endpoint, enabled with endpoint, default/HTTP protocol selection, service-name default, shutdown idempotent, no-panic no-op rendering with telemetry disabled
- [x] 5.2 Update `device/README.md` with `OTEL_*` environment variable documentation
- [x] 5.3 Run the Python test suite (`python -m unittest discover` in `device/`) and `task pre-push`
