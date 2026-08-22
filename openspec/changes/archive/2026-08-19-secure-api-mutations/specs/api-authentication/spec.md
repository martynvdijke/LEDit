## ADDED Requirements

### Requirement: Public display reads

The service SHALL allow unauthenticated GET requests to `/api/trmnl/stats`, `/api/health`, current feed, and notification read endpoints.

#### Scenario: Public stats poll

- WHEN TRMNL requests `/api/trmnl/stats` without credentials
- THEN LEDit returns the statistics payload

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
