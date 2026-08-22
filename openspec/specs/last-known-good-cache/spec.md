# last-known-good-cache Specification

## Purpose
TBD - created by archiving change last-known-good-cache. Update Purpose after archive.
## Requirements
### Requirement: Last-known-good caching
The system SHALL cache the last successful render of each datasource, keyed by source identity and resolution.

#### Scenario: Successful render is cached
- **WHEN** a datasource's `GetPNG` call succeeds
- **THEN** the system SHALL store the rendered frame in the cache keyed by `<type>:<id>` and resolution

#### Scenario: Distinct resolutions cached separately
- **WHEN** the same source renders at different resolutions (e.g., 400×400 preview and 64×64 device)
- **THEN** the system SHALL maintain a separate cache entry for each resolution

### Requirement: Serve stale frame on failure
The system SHALL serve the cached frame when a datasource's render fails and a cached frame exists.

#### Scenario: Feed serves stale frame
- **WHEN** `GetPNG` fails in the feed loop and a cached frame exists for that source and resolution
- **THEN** the system SHALL send the cached frame to the client instead of skipping the source

#### Scenario: Preview serves stale frame
- **WHEN** `GetPNG` fails in the admin preview endpoint and a cached frame exists
- **THEN** the system SHALL return the cached frame with the `X-LEDit-Stale: 1` and `X-LEDit-Stale-Age` response headers

#### Scenario: No cache entry keeps today's behavior
- **WHEN** `GetPNG` fails and no cached frame exists for that source
- **THEN** the system SHALL behave as before the change (skip in feed, error in preview)

### Requirement: Stale frame marking in feed messages
The system SHALL mark stale frames in the WebSocket feed message so clients can distinguish live from cached content.

#### Scenario: Stale flag present
- **WHEN** the feed loop serves a cached (stale) frame
- **THEN** the message SHALL include `stale: true` and `stale_age` (seconds since the frame was rendered)

#### Scenario: Live frames unmarked
- **WHEN** the feed loop serves a freshly rendered frame
- **THEN** the message SHALL NOT include the stale flag

#### Scenario: Old clients unaffected
- **WHEN** a device client that predates the stale flag receives a message containing it
- **THEN** the client SHALL continue to display the frame normally (unknown fields ignored)

### Requirement: Bounded cache memory
The system SHALL bound the cache size so memory use cannot grow without limit.

#### Scenario: LRU eviction
- **WHEN** the cache reaches its maximum entry count and a new frame is cached
- **THEN** the system SHALL evict the least-recently-used entry

#### Scenario: Config change invalidates stale entry
- **WHEN** a source's configuration (e.g., URL or token) changes
- **THEN** the system SHALL treat the existing cache entry as absent until the source renders successfully with the new configuration

### Requirement: Cache statistics
The system SHALL track cache hit, miss, and stale-serve counts.

#### Scenario: Stats available
- **WHEN** the health dashboard or admin surfaces request cache statistics
- **THEN** the system SHALL provide hit, miss, and stale-serve counts for the current process

