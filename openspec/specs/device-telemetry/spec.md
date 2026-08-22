# device-telemetry Specification

## Purpose
TBD - created by archiving change add-otel-to-device-library. Update Purpose after archive.
## Requirements
### Requirement: OTel pipeline initialisation from environment variables
The device library SHALL initialise an OpenTelemetry pipeline (traces, metrics, and log export) from standard `OTEL_*` environment variables, mirroring the LEDit server's conventions. When `OTEL_EXPORTER_OTLP_ENDPOINT` is not set, the library SHALL return a disabled no-op telemetry instance and the device SHALL run exactly as before.

#### Scenario: Endpoint configured
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is set to a reachable OTLP endpoint
- **THEN** the telemetry pipeline SHALL be enabled and export traces, metrics, and logs to that endpoint

#### Scenario: No endpoint configured
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is not set
- **THEN** telemetry SHALL be disabled and the device SHALL continue to render frames without any OTel-related failure

#### Scenario: Default protocol
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is set but `OTEL_EXPORTER_OTLP_PROTOCOL` is unset
- **THEN** the library SHALL use the `grpc` protocol

#### Scenario: HTTP protocol
- **WHEN** `OTEL_EXPORTER_OTLP_PROTOCOL` is set to `http/protobuf`
- **THEN** the library SHALL export over HTTP

#### Scenario: Service name
- **WHEN** `OTEL_SERVICE_NAME` is set to a value such as `my-led-matrix`
- **THEN** exported telemetry SHALL be attributed to that service name; otherwise it SHALL default to `ledit-device`

### Requirement: Device log export with trace correlation
The device's log output (currently written via `config.log`) SHALL be forwarded to the OTel log pipeline when telemetry is enabled, carrying the active trace context. Existing stderr output SHALL continue to appear unchanged.

#### Scenario: Log emitted with telemetry enabled
- **WHEN** the device logs a message while telemetry is enabled and a span is active
- **THEN** the OTel log record SHALL be exported with the active trace/span context attached

#### Scenario: Log emitted with telemetry disabled
- **WHEN** the device logs a message while telemetry is disabled
- **THEN** the log SHALL be written to stderr only, with no OTel activity

### Requirement: Spans for WebSocket lifecycle and rendering
The device library SHALL record OpenTelemetry spans for connection lifecycle and frame rendering when telemetry is enabled.

#### Scenario: Message received
- **WHEN** the device receives a WebSocket message
- **THEN** a span SHALL be recorded covering message decode and render

#### Scenario: Image rendered
- **WHEN** an image frame is rendered to the display
- **THEN** a nested span SHALL be recorded for the image render

#### Scenario: Text rendered
- **WHEN** a text frame is rendered to the display
- **THEN** a nested span SHALL be recorded for the text render

#### Scenario: Connection error
- **WHEN** the WebSocket connection reports an error
- **THEN** a span SHALL be recorded with error status describing the failure

#### Scenario: Telemetry disabled
- **WHEN** telemetry is disabled and a message is received
- **THEN** no span SHALL be created and rendering SHALL proceed normally

### Requirement: Device metrics
When telemetry is enabled, the device library SHALL record metrics for frame rendering and connection stability.

#### Scenario: Frame rendered
- **WHEN** a frame is rendered to the display
- **THEN** the `device.frames_rendered_total` counter SHALL increment with an attribute distinguishing image from text frames

#### Scenario: Connection error
- **WHEN** the WebSocket connection reports an error
- **THEN** the `device.connection_errors_total` counter SHALL increment

#### Scenario: Reconnect
- **WHEN** the client reconnects after a dropped connection
- **THEN** the `device.reconnects_total` counter SHALL increment

### Requirement: Graceful shutdown
The device library SHALL flush and shut down all telemetry providers on process exit so buffered spans and metrics are exported.

#### Scenario: Clean exit
- **WHEN** the device process exits (including via Ctrl-C)
- **THEN** all telemetry providers SHALL be shut down and buffered telemetry flushed

#### Scenario: Shutdown when disabled
- **WHEN** telemetry is disabled and shutdown is called
- **THEN** shutdown SHALL complete without error or side effects

### Requirement: Telemetry failure does not break rendering
Instrumentation SHALL never cause the device to fail rendering, even if the telemetry backend is unreachable or an instrumentation call raises.

#### Scenario: Unreachable OTLP endpoint
- **WHEN** the configured OTLP endpoint is unreachable
- **THEN** the device SHALL continue rendering frames and SHALL only log a warning about telemetry export

