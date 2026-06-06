## ADDED Requirements

### Requirement: OpenTelemetry log export
The system SHALL export structured log entries via OpenTelemetry OTLP when configured.

#### Scenario: OTEL disabled by default
- **WHEN** the system starts with no OTEL configuration
- **THEN** log entries SHALL NOT be exported via OTLP

#### Scenario: OTEL configured and enabled
- **WHEN** an admin configures an OTEL endpoint and enables export on the log settings page
- **THEN** each log entry SHALL be exported via OTLP to the configured endpoint

#### Scenario: OTEL export failure
- **WHEN** the OTEL exporter fails to send a log entry
- **THEN** the system SHALL log the failure locally and continue operating normally

#### Scenario: OTEL configuration fields
- **WHEN** an admin views the OTEL configuration section
- **THEN** the following fields SHALL be available: endpoint URL, protocol (gRPC/HTTP), enabled toggle

### Requirement: Log entry severity mapping to OTEL
The system SHALL map internal log levels to OpenTelemetry severity numbers.

#### Scenario: Severity mapping
- **WHEN** a log entry is exported via OTLP
- **THEN** trace → SEVERITY_NUMBER_TRACE (1), debug → DEBUG (5), info → INFO (9), warn → WARN (13), error → ERROR (17), fatal → FATAL (21)
