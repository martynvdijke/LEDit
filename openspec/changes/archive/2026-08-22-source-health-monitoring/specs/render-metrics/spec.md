# Render Metrics Specification

## ADDED Requirements

### Requirement: Per-source render duration metrics
The health registry SHALL store the last render duration and an exponentially weighted moving average (alpha 0.3) of render duration in milliseconds for every source.
#### Scenario: Duration recorded on render
- WHEN any datasource render completes (success or failure)
- THEN the last duration is stored and the EWMA is updated for that source

#### Scenario: Metrics are readable
- WHEN the dashboard or analytics page loads
- THEN per-source average and last render durations are available for display

### Requirement: Matrix cache metrics
The matrix panel cache SHALL track hit and miss counters, guarded so metrics work without the matrix feature.
#### Scenario: Cache hit counted
- WHEN a matrix cell render is served from the TTL cache
- THEN the cache hit counter is incremented

#### Scenario: Cache miss counted
- WHEN a matrix cell render requires a fresh fetch
- THEN the cache miss counter is incremented

#### Scenario: Guarded dependency
- WHEN the matrix dashboard feature is absent
- THEN render metrics for regular sources still work without errors

### Requirement: Metrics display
The dashboard SHALL show a render-metrics summary including average duration and matrix cache hit ratio when available, and the analytics page SHALL list per-source durations.
#### Scenario: Dashboard summary
- WHEN the dashboard loads
- THEN a summary of render metrics is visible, including average render duration and matrix cache hit ratio when available

#### Scenario: Analytics breakdown
- WHEN the analytics page loads
- THEN per-source last duration and EWMA values are listed
