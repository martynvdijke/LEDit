# Design: outbound-alerting

## Context

`source-health-monitoring` (in-progress) adds an in-memory health registry keyed `<type>:<id>` with last success/error times, consecutive failures, and total counts, plus device liveness from `last_seen_at`. `EmailSettings` (SMTP host/port/user/pass) is configured in admin but never used anywhere. The app has no outbound notification path at all.

## Goals / Non-Goals

**Goals**
- Alert on meaningful state *transitions* (source starts failing, source recovers, device goes stale), never on every render error.
- Ship Gotify (self-hosted push) as the primary channel and finally wire up the dormant SMTP settings as the second.
- Admin-configurable thresholds, cooldown, and per-channel toggles, with a test button.
- Delivery failures are logged and never crash the server.

**Non-Goals**
- No alert history table in v1 (alerts are ephemeral; the log is the record — revisit if needed).
- No integration with the push-to-display Telegram bot in this change (can be added later as another sender).
- No alert rules beyond source-health and device-stale (no arbitrary "price crossed X" alerting).

## Decisions

### 1. Alert engine: poll the registry, alert on transitions
A single goroutine started at server init (`StartAlertEngine`), ticking every 30s:
- For each source in the health registry: if `consecutiveFailures >= threshold` and the source's *last alert state* was not "failing" → send a "source failing" alert; if it recovers (success render) and was "failing" → send a "recovered" alert.
- For each enabled device: stale when `now - last_seen_at > staleThreshold` (default `3 × refresh_interval`, matching source-health-monitoring's alive definition) → alert once per stale episode, plus a "back online" alert on reconnect.
- State machine lives in memory (`map[alertKey]alertState`), so a restart re-arms alerts (no spamming old state).
- **Alternative considered**: event-driven hooks inside the feed loop. Rejected — it couples the hot render path to alerting and to the DB; a poller over the registry keeps the feed loop untouched and makes the engine independently testable.

### 2. Cooldown and dedup
Each alert key carries a `lastSentAt`; a key cannot alert again within `cooldownMinutes` (default 15). Because the poller only alerts on *transitions*, the cooldown is a second safety net against flapping (fail→recover→fail). The test button bypasses the cooldown.

### 3. Gotify sender
`POST {server}/message` with header `X-Gotify-Key: <appToken>`, JSON body `{title, message, priority, extras}`. Gotify priority mapping: source failing = 5 (high), device stale = 5, recovery = 2. Server URL may be `https://gotify.example.com` (path `/message` appended) — support servers mounted under a subpath by trimming trailing `/` and appending `/message`.
- **Alternative considered**: Ntfy. Rejected — user explicitly asked for Gotify; senders are small structs behind an interface, so a future `ntfy` sender is a ~30-line addition.

### 4. Email sender via stdlib `net/smtp`
Plain-text email from `EmailSettings.From` to `AlertSettings.RecipientEmail` using `smtp.PlainAuth` (TLS via `smtp.SendMail` with implicit-TLS fallback to `StartTLS` handled by checking the port / server capability). Content = same title/message as Gotify. `net/smtp` means zero new dependencies.
- **Alternative considered**: `gomail`/`go-mail` libraries. Rejected — stdlib covers plain SMTP; the dormant settings were never built around attachments or HTML.

### 5. Sender interface + registry
```go
type AlertSender interface {
    Name() string
    Send(ctx context.Context, a Alert) error  // Alert{Title, Message, Priority}
    Enabled() bool
}
```
`NewAlertEngine(registry, senders, cfg)` — the engine only depends on the interface and the health registry's read API, keeping this change decoupled from specific senders and easy to unit test with fake senders.

## Risks / Trade-offs

- [Gotify server down → repeated delivery errors] → logged via slog + LogEntry; retry happens naturally on next transition/cooldown window; engine never panics.
- [SMTP misconfigured (dormant settings may be garbage)] → test button gives immediate feedback; engine logs errors and continues; bad SMTP never blocks Gotify if both are enabled.
- [Alert storm when many sources fail at once (e.g., internet outage)] → transitions + per-key cooldown caps volume; also note: an internet outage may itself knock out Gotify/SMTP — alerts queue? No: v1 drops them (log only). Mitigation documented; revisit with a persistent outbox if users need it.
- [Health registry is in-memory (resets on restart)] → alert state resets too; on restart, a currently-failing source will re-alert once — acceptable and arguably desirable.

## Migration Plan

Additive ent migration: new `AlertSettings` single-row table + edge from `GeneralSettings`. No changes to existing tables. The engine is opt-in: if no channel is enabled, `StartAlertEngine` still runs but short-circuits (cheap poll, no sends). Rollback = remove the entity + stop the goroutine; `source-health-monitoring` is unaffected.

## Open Questions

- Should recovery alerts be optional (some users only want "something broke", not "it's fixed")? v1: on by default, one checkbox to silence recovery alerts.
- Alert content for stale devices: include device name + IP + last_seen — anything else useful? (Proposal assumes name + IP.)
