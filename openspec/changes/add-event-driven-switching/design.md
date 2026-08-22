## Context

Switching is purely time-driven: cron schedules gate when sources run and `serveFeed` rotates on a per-slot timeout, polling `FeedController.ShouldSkip()` every 50 ms. Notifications are the only interrupt (broadcast at slot boundaries), and they are all-or-nothing. There is no "stay on this while something is true" tier. Datasources implement a single method (`Datasource.GetPNG`); the pixel-maker-animator design establishes the precedent of optional capability interfaces type-checked by `serveFeed`.

## Goals / Non-Goals

**Goals:**
- A rule can hold the wall on a target source while a live condition is true, releasing automatically when it clears.
- Precedence is explicit: notifications > pinned rule > rotation; manual pause still pauses; manual skip releases a pin.
- Evaluator failure or a misconfigured rule can never stall or blank the feed.

**Non-Goals:**
- Arbitrary expression languages, multi-condition AND/OR trees (single `{path, operator, value}` per rule v1).
- Per-device rules (rules are global; per-device × event rules comes after device playlists mature).
- Webhook/MQTT push-triggered pins (that is push-to-display territory; this change is poll-based).

## Decisions

### D1 — Single evaluator goroutine with per-rule next-check times
One goroutine owns all enabled rules: each tick it scans rules whose `nextCheck` has elapsed, fetches state via the target's `StateProvider`, evaluates, and transitions pin state. Rules reload from the DB every 30 s so edits apply without restart. Check scheduling adds ±10% jitter.
- *Why:* One goroutine = no coordination bugs; jitter prevents thundering herds against the same upstream API; periodic reload keeps ops simple (no cache-invalidation API).
- *Alternative:* Goroutine per rule — rejected: lifecycle churn on edit for zero benefit at expected rule counts (<50).

### D2 — Condition model: one dot-path comparison per rule
`condition` JSON: `{"path": "player.isPlaying", "operator": "eq", "value": true}`. Path resolution reuses `extractDotPath` semantics (promoted from `datasource/genericapi.go` to an exported `datasource.DotPath`). Operators: `eq`, `ne`, `gt`, `lt`, `ge`, `le`, `contains`, `exists`. Numeric comparison when both sides parse as numbers, else string equality; `contains` works on strings and arrays; unresolvable paths make every operator false except `exists`.
- *Why:* Covers the real cases (playback state, sensor booleans, thresholds) without inventing an expression engine. Pixel-art bindings already chose band-rules over expressions for the same reason.

### D3 — StateProvider is an optional capability interface
```go
type StateProvider interface {
    CurrentState(ctx context.Context) (map[string]any, error)
}
```
Rules targeting sources that don't implement it are skipped with a once-per-start log line, not errors. Initial implementations: GenericAPI (reuses its fetch + JSON machinery), HomeAssistant, SystemStats. Jellyfin arrives with `more-datasources` and immediately becomes rule-targetable.
- *Why:* Mirrors the Animator capability pattern; graceful degradation means rules can be authored before their datasource ships.

### D4 — Pins live on FeedController; evaluator reaches controllers through an active-feed registry
`FeedController` gains pin state (`PinnedKey string`, set/cleared under the existing mutex). `serveFeed` registers its controller in a package-level registry on start and deregisters on exit; the evaluator pins/unpins **all registered controllers**, so physical devices and previews behave identically. At each slot boundary `serveFeed` checks `feed.PinnedKey`: when set, it renders that source instead of advancing, holding until unpinned. Manual `Next()` clears the current pin (operator override wins until the condition re-fires after cooldown).
- *Why:* Reuses the existing per-controller structure and the 50 ms poll loop; no new message types; device protocol untouched.
- *Alternative:* Evaluator writes frames directly — rejected: bypasses LKG/health/notifications plumbing and duplicates the render path.

### D5 — Pin rendering goes through the normal slot path
A pinned slot renders exactly like a normal slot: LKG cache, health recording, stale marking, notification checks, frame counting. Only source selection differs.
- *Why:* Zero special cases for caching/health; a pinned failing source degrades to its last-known-good frame like any other.

### D6 — Cooldown suppresses flapping in both directions
After a pin releases (condition false) it cannot re-fire for `cooldown_seconds`; after it fires, it holds minimum `cooldown_seconds` even if the condition flickers false briefly (min-hold). Default 0 = release immediately.
- *Why:* Garage-door sensors and playback states bounce; hysteresis on both edges is what makes this feel deliberate.

### D7 — Routes/UI follow admin CRUD patterns
`admin.GET/POST /admin/eventrules/...` CRUD page: name, enabled, target source picker (bindingOptions data), condition builder (path text, operator select, value input), interval + cooldown numbers. Dashboard shows a "held by rule: <name>" badge when any controller is pinned.
- *Why:* Consistent with every other admin surface; no new auth concepts.

## Risks / Trade-offs

- [Evaluator fetch storms against upstream APIs] → Per-rule intervals with jitter; implementations reuse each datasource's own caching/TTL (GenericAPI already caches); interval floor of 5 s validated at save.
- [Pin and notification fight at a slot boundary] → Fixed precedence: notifications are checked first and simply delay the pinned slot; the pin persists across the interruption.
- [Rule targets a source that fails to render] → Pinned slot falls back to LKG/stale like any slot; if no LKG exists, slot logs and retries next tick rather than blanking.
- [Stale pins after evaluator crash] → Evaluator death is logged and auto-restarted by the server's run loop; pins clear on process restart since they are in-memory only.

## Migration Plan

Ent codegen adds the `displayrule` table + GeneralSettings edge; additive migration. Feature is inert with zero rules; existing feeds are unaffected. Rollback safe.

## Open Questions

- Should a pinned rule be visible on the device itself (small corner indicator)? Assumed no for v1.
- Global kill-switch setting for all rules (deferred — disabling individual rules covers it).
