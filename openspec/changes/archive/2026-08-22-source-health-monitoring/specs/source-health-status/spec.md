# Source Health Status Specification

## ADDED Requirements

### Requirement: Source health tracking
The server SHALL record health information for every configured datasource after each render attempt, keyed by type and id, storing last success time, last error, last duration, consecutive failure count, and totals.
#### Scenario: Successful render records success
- WHEN the feed loop or preview endpoint successfully renders a datasource of type `weather` with id `2`
- THEN the health registry records a success for key `weather:2` with the render duration and current timestamp
- THEN the consecutive failure count for that key is reset to zero

#### Scenario: Failed render records failure
- WHEN a datasource `GetPNG` returns an error
- THEN the health registry records the error message and duration for that key
- THEN the consecutive failure count is incremented and the total failure count is incremented

#### Scenario: Snapshot is read-safe
- WHEN any handler requests a health snapshot while renders are in progress
- THEN the snapshot contains consistent per-source values without blocking writers for longer than a read lock

### Requirement: Health status classification
A source's status SHALL be derived from its consecutive failure count: green at zero, yellow at 1-2, red at 3 or more or when it failed without prior success.
#### Scenario: Healthy source is green
- WHEN a source has zero consecutive failures
- THEN its status is green

#### Scenario: Flaky source is yellow
- WHEN a source has failed 1 or 2 times consecutively after a prior success
- THEN its status is yellow

#### Scenario: Broken source is red
- WHEN a source has 3 or more consecutive failures, or has failed without any prior success
- THEN its status is red

### Requirement: Dashboard health display
The admin dashboard SHALL show a status indicator per configured datasource and a health summary with green, yellow, and red counts.
#### Scenario: Dashboard shows per-source status
- WHEN the dashboard loads with sources configured
- THEN each row in the datasources table shows a status dot colored by the source's current classification

#### Scenario: Summary counts are shown
- WHEN the dashboard loads
- THEN the health summary shows how many sources are green, yellow, and red

### Requirement: Matrix editor warnings
The matrix layout editor SHALL mark failing sources in binding options, guarded so the feature degrades gracefully when the matrix feature is absent.
#### Scenario: Failing source is marked
- WHEN a source has a red status and the matrix editor renders binding options
- THEN that source's option is annotated with a warning mark

#### Scenario: Guarded dependency
- WHEN the matrix editor is not available (feature absent)
- THEN the dashboard health features still work without errors

### Requirement: Health API
GET /api/health SHALL return a read-only JSON snapshot of the health registry without authentication, and write methods SHALL be rejected.
#### Scenario: Health endpoint returns snapshot
- WHEN an unauthenticated client requests `/api/health`
- THEN the response is JSON containing per-source health entries with status classification

#### Scenario: Health endpoint is read-only
- WHEN any client requests `/api/health` with a write method such as POST
- THEN the request is rejected and no health state changes
