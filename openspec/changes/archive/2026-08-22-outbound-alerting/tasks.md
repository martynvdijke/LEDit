## 1. AlertSettings Entity

- [x] 1.1 Add `ent/schema/alertsettings.go`: single-row entity with gotify_enabled, gotify_url, gotify_token, email_enabled, recipient_email, failure_threshold, cooldown_minutes, stale_multiplier, notify_recovery + edge from `GeneralSettings`; run codegen
- [x] 1.2 Add defaults matching design (threshold 3, cooldown 15, stale multiplier 3, recovery on)

## 2. Sender Implementations

- [x] 2.1 Define `AlertSender` interface and `Alert{Title, Message, Priority}` in `handlers/alerting.go`
- [x] 2.2 Implement Gotify sender: POST `{server}/message` with `X-Gotify-Key` header, priority mapping (fail 5 / stale 5 / recover 2); unit test with `httptest`
- [x] 2.3 Implement email sender using stdlib `net/smtp` with `EmailSettings` (PlainAuth, implicit-TLS or StartTLS fallback); unit test with a stub SMTP server or skip-on-no-server guard
- [x] 2.4 Log delivery failures via slog with the `source` attribute convention

## 3. Alert Engine

- [x] 3.1 Implement `StartAlertEngine(ctx, registry, senders, cfg)`: 30s ticker, per-key state machine (failing/stale/recovered), cooldown map
- [x] 3.2 Consume the health registry from `source-health-monitoring` (read-only): consecutive failures per source + device `last_seen_at` staleness
- [x] 3.3 Wire recovery/stale/back-online transitions per design; short-circuit when no channel enabled
- [x] 3.4 Unit tests: transition triggers exactly one alert, cooldown suppresses repeats, transient failures below threshold don't alert, stale + recovery cycle, no-channel short-circuit
- [x] 3.5 Start the engine at server init in `main.go` / server setup

## 4. Admin UI

- [x] 4.1 Add alert settings admin form (template + save handler `POST /admin/settings/alerts`) with all fields
- [x] 4.2 Add "Send test alert" handler `POST /admin/settings/alerts/test` reporting per-channel results
- [x] 4.3 Add sidebar link/section for alert settings
- [x] 4.4 Playwright test: save settings, test alert button shows per-channel results

## 5. Integration & Verification

- [x] 5.1 Playwright test: alert settings page renders and persists
- [x] 5.2 Run `task pre-push` (gofmt, tests, build) and fix failures
- [x] 5.3 Confirm no changes to feed loop, datasources, or WebSocket protocol
