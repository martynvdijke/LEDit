## ADDED Requirements

### Requirement: Stats polling endpoint
The system SHALL expose a public HTTP GET endpoint at `/api/trmnl/stats` that returns JSON containing system stats and display analytics.

#### Scenario: Request stats
- **WHEN** a client sends `GET /api/trmnl/stats`
- **THEN** the response SHALL have HTTP status 200
- **THEN** the response body SHALL be JSON with top-level `system` and `analytics` objects

#### Scenario: No authentication required
- **WHEN** an unauthenticated client sends `GET /api/trmnl/stats`
- **THEN** the request SHALL succeed with HTTP status 200 (no login or token required)

### Requirement: System stats content
The `system` object in the response SHALL contain the same statistics collected by the System Stats datasource: CPU core count, Go version, OS/platform, memory allocation, and load average.

#### Scenario: System fields present
- **WHEN** the stats endpoint returns a response
- **THEN** the `system` object SHALL include `cpu_cores` (integer)
- **THEN** the `system` object SHALL include `go_version` (string)
- **THEN** the `system` object SHALL include `os` (string, `GOOS/GOARCH`)
- **THEN** the `system` object SHALL include `memory` (string, format `"alloc/total MB"`)
- **THEN** the `system` object SHALL include `load` (string, 1/5/15-minute load averages, or `"--"` when unavailable)

### Requirement: Analytics content
The `analytics` object in the response SHALL reflect the current display analytics: total displays, uptime, per-source display counts, and recent display events.

#### Scenario: Analytics fields present
- **WHEN** the stats endpoint returns a response
- **THEN** the `analytics` object SHALL include `total_displays` (integer)
- **THEN** the `analytics` object SHALL include `uptime` (string, Go duration format)
- **THEN** the `analytics` object SHALL include `by_source` (object mapping source name to display count)
- **THEN** the `analytics` object SHALL include `recent` (array of display events with `source`, `time`, and `duration` fields)

#### Scenario: Analytics reflects tracked displays
- **WHEN** display events have been tracked via the display tracking system
- **THEN** `total_displays` and `by_source` SHALL match the tracked events

### Requirement: Consistent data sources
The system stats SHALL be sourced from the same collection logic used by the System Stats datasource, so values never diverge between the TRMNL endpoint and the LED matrix feed.

#### Scenario: Shared collection logic
- **WHEN** the stats endpoint is called
- **THEN** the `system` values SHALL be produced by the same helper used by the System Stats datasource
