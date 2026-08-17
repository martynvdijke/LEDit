## Why

LEDit provides public display statistics but also has feed-control and webhook operations. Those writes need the same explicit user-plus-token policy as the other API-backed projects.

## What Changes

- Keep `GET /api/trmnl/stats`, health, feed, and notification reads public.
- Require an authenticated user and bearer API token for feed pause/resume/next, webhook notifications, and all other writes.
- Add secure, one-time-visible token lifecycle management.

## Capabilities

### New Capabilities

- `api-authentication`

### Modified Capabilities

- `public-api-reads`

## Impact

LEDit route middleware, persistence, token management, migration, tests, and API documentation are affected. TRMNL polling remains unauthenticated.
