## Context

LEDit has public display endpoints and operational feed/webhook writes. Authentication must be added at the mutation boundary without breaking wall-display polling.

## Goals / Non-Goals

### Goals

- Keep public GET display data available.
- Use user-scoped, hashed, revocable bearer tokens for writes.
- Preserve existing admin and browser protections.

### Non-Goals

- Changing TRMNL read payloads.
- Storing real secrets in source control.
- Replacing existing session authentication.

## Decisions

- Add a token model and reversible migration with owner and lifecycle metadata.
- Validate bearer tokens in middleware after establishing the application user; require owner agreement.
- Protect feed controls, webhook notification, admin/log writes, and all other mutation routes discovered in the route audit.
- Expose token lifecycle operations only to the owner and never return a stored secret.

## Risks / Trade-offs

Operational clients need session and token provisioning. Public reads remain intentionally unauthenticated and must not be confused with writes.

## Migration Plan

1. Add persistence and token lifecycle endpoints.
2. Add middleware and route enforcement.
3. Add tests and migrate feed/webhook clients.

## Open Questions

- Should service accounts be supported, or must every token belong to a human user?
