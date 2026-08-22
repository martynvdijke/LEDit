# api-authentication Specification

## Purpose
TBD - created by archiving change secure-api-mutations. Update Purpose after archive.
## Requirements
### Requirement: Public display reads
The service SHALL allow unauthenticated GET requests only to `/api/trmnl/stats` and `/api/health`; `GET /api/feed/current` and `GET /api/notifications` SHALL require authentication via a valid session cookie or bearer API token and SHALL return `401 Unauthorized` for anonymous callers.

#### Scenario: Public stats poll
- **WHEN** TRMNL requests `/api/trmnl/stats` without credentials
- **THEN** LEDit returns the statistics payload

#### Scenario: Public health check
- **WHEN** an anonymous client requests `/api/health`
- **THEN** LEDit returns the health payload

#### Scenario: Anonymous feed current rejected
- **WHEN** an anonymous client requests `GET /api/feed/current` without a session cookie or bearer token
- **THEN** the response is `401 Unauthorized` and feed state is not disclosed

#### Scenario: Authenticated feed current succeeds via session
- **WHEN** a client requests `GET /api/feed/current` with a valid `session` cookie
- **THEN** LEDit returns the current feed status (paused, current, next)

#### Scenario: Authenticated feed current succeeds via bearer token
- **WHEN** a client requests `GET /api/feed/current` with `Authorization: Bearer <valid-token>`
- **THEN** LEDit returns the current feed status

#### Scenario: Anonymous notifications rejected
- **WHEN** an anonymous client requests `GET /api/notifications` without a session cookie or bearer token
- **THEN** the response is `401 Unauthorized` and no notification history is disclosed

#### Scenario: Authenticated notifications succeeds
- **WHEN** a client requests `GET /api/notifications` with a valid session cookie or bearer token
- **THEN** LEDit returns the notification history

### Requirement: Write authorization

Feed pause, resume, next, webhook notify, log-management writes, and every create/update/delete route MUST require an authenticated user and that user's valid bearer API token.

#### Scenario: Anonymous feed control

- WHEN an anonymous client posts a feed-control request
- THEN the request is rejected and feed state is unchanged

### Requirement: Safe token lifecycle

An authenticated user SHALL create, list metadata for, revoke, and rotate owned tokens. The secret MUST be shown only on creation and stored only as a hash.

#### Scenario: Token revocation

- WHEN a user revokes a token and reuses it
- THEN the write is rejected without leaking token metadata

