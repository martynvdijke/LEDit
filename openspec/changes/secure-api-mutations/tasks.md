## 1. Audit and storage

- [ ] 1.1 Inventory feed, webhook, admin, and log mutation routes.
- [ ] 1.2 Add hashed-token persistence, indexes, and reversible migration.

## 2. Middleware and lifecycle

- [ ] 2.1 Implement bearer validation, expiry, revocation, and owner matching.
- [ ] 2.2 Protect every mutation while preserving public GET routes.
- [ ] 2.3 Implement create/list/revoke/rotate token operations with one-time secrets.

## 3. Tests and rollout

- [ ] 3.1 Test public reads and anonymous/token-only/malformed/expired/revoked writes.
- [ ] 3.2 Test ownership and secret non-disclosure.
- [ ] 3.3 Document client migration and run LEDit tests.
