# public-feed-surface Specification

## Purpose
TBD - created by archiving change gate-feed-details-behind-auth. Update Purpose after archive.
## Requirements
### Requirement: Public landing shows minimal info
The system SHALL render `GET /` for unauthenticated requests as a minimal public landing that includes only basic public information (application name, version/build, service health/running status, and a login call-to-action) and SHALL NOT expose detailed feed state, source names, queue, notification history, device roster, or feed controls.

#### Scenario: Anonymous visits landing
- **WHEN** an unauthenticated client sends `GET /` without a valid session
- **THEN** the response is `200` and contains version/build, health/running status, and a login CTA, and does not contain current/next source names, device list details, or feed controls

#### Scenario: Anonymous view has no controls
- **WHEN** an unauthenticated client renders `GET /`
- **THEN** feed playback controls (pause/skip/fullscreen/e-ink toggle that implies feed state) and the device selector are not displayed

### Requirement: Authenticated landing shows full details
The system SHALL render `GET /` for authenticated requests (valid session cookie) with the full live feed, device selector, telemetry (current/next source, connection status, stale indicator), and playback controls.

#### Scenario: Authenticated visits landing
- **WHEN** a client sends `GET /` with a valid `session` cookie
- **THEN** the response is `200` and contains the live feed stage, device selector (when devices exist), telemetry, and feed controls

#### Scenario: Public landing sets cache policy
- **WHEN** `GET /` is served to an unauthenticated visitor
- **THEN** the response includes `Cache-Control: no-store` and `Vary: Cookie, Authorization` so intermediaries do not cache public as authenticated

