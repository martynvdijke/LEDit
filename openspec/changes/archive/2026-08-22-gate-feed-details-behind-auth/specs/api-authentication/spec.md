## MODIFIED Requirements

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
