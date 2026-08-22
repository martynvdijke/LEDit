# Change Proposal: outbound-alerting

## Why

LEDit can detect problems (the in-progress `source-health-monitoring` change adds a per-source health registry and device liveness), but it can only *show* them on the admin dashboard. The wall keeps falling back silently, devices go stale overnight, and nobody's phone buzzes. Meanwhile the app already stores full SMTP credentials (`EmailSettings`) that nothing ever uses. This change makes LEDit *tell you* when something is wrong — on your phone, via Gotify or email.

## What Changes

1. **Alert engine** — a background loop that watches the health registry (from `source-health-monitoring`): source enters "failing" state (consecutive failures ≥ threshold), source recovers, or a device goes stale/offline. State *transitions* trigger alerts — not every render failure. A cooldown per source (default 15 min) prevents alert storms.

2. **Gotify channel** — new `AlertSettings` entity with Gotify server URL + app token. Alerts POST to `{server}/message` with `X-Gotify-Key` auth (title, message, priority). Gotify is the primary channel: self-hosted, push to Android/iOS/web, zero email infra.

3. **Email channel** — the dormant `EmailSettings` (SMTP host/port/user/pass) finally gets used: alerts are sent as plain-text emails to a configured recipient. Both channels are independent toggles; at least one must be enabled for the engine to run.

4. **Admin configuration** — new admin settings section: per-channel enable, source-failure threshold (consecutive failures before alerting), device-stale threshold (how many refresh intervals without `last_seen_at`), cooldown minutes, and a "Send test alert" button that fires a message through each enabled channel.

5. **Delivery failures are logged, never fatal** — a failed Gotify POST or SMTP error is written to the log (`slog` + `LogEntry`) and retried on the next cooldown window. The alert loop can never take the server down.

## Capabilities

### New Capabilities
- `outbound-alerting`: delivery of source-health and device-liveness alerts to external channels (Gotify, email) with thresholds, cooldowns, and admin configuration.

### Modified Capabilities
- (none — existing capabilities keep their requirements; this is additive)

## Impact

- Depends on `source-health-monitoring` being implemented (the alert engine consumes its health registry and device liveness). If it lands first, this change is a pure consumer; the alert engine reads health state, never writes it.
- New `AlertSettings` ent entity (single row) with edges from `GeneralSettings`; new admin settings form section; new `handlers/alerting.go` engine + senders.
- New `POST /admin/settings/alerts` save handler, `POST /admin/settings/alerts/test` test handler.
- No changes to the feed loop, datasources, WebSocket protocol, or device client.
- New dependency: none for Gotify (plain HTTP POST); email uses Go's stdlib `net/smtp` — no new Go modules.
