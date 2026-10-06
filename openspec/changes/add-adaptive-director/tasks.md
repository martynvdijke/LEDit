## 1. Profile Data Model & Migration

- [ ] 1.1 Add `ent/schema/system_profile.go`: `name` (NotEmpty, Unique), `description`, `enabled` (default true), `is_fallback` (default false), `playlist_id` (optional nillable), `brightness` (text JSON, default `"{}"`), `theme_id` (optional nillable), `overlay` (text JSON, default `"{}"`), `schedule_windows` (text JSON, default `"[]"`), `created_at`/`updated_at`; run `go generate ./ent` and verify the additive migration on an existing database
- [ ] 1.2 Add `ent/schema/profile_settings.go` singleton mirroring `firmware_settings.go`: `active_profile_id` (optional nillable), `selection_mode` (`manual|scheduled`, default `manual`), `last_switch_from`, `last_switch_reason`, `last_switch_at`; run `go generate ./ent`
- [ ] 1.3 Add `ent/schema/profile_switch.go` audit log (`profile_id`, `from_profile_id` optional, `reason`, `actor`, `created_at`) with a bounded prune to the most recent N entries; run `go generate ./ent`
- [ ] 1.4 Add nullable `profile_id` to `ent/schema/displayrule.go`; run `go generate ./ent` and confirm untagged rules (`profile_id IS NULL`) are unaffected by migration
- [ ] 1.5 Seed the fallback profile and the `ProfileSettings` singleton idempotently on startup (alongside `seedThemes` in `handlers/theme.go` or a new `handlers/profiles.go` init path); a rerun must not create duplicates
- [ ] 1.6 Unit tests: unique name enforcement, fallback seeding idempotence, JSON member round-trip, schedule window storage/reload

## 2. Profile Engine — Resolution & Atomic Activation

- [ ] 2.1 Implement `handlers/profiles.go` value types: `SystemProfile` with parsed members (brightness/overlay JSON and `[]ScheduleWindow`) converted from Ent rows so resolution is testable without DB types
- [ ] 2.2 Implement profile validation: name non-empty/unique, referenced playlist/theme exist when set, brightness/overlay ranges match the device fields (`ValidateBrightnessWindows`, overlay position/height/speed/bg/fg constraints), `ValidateWindows` with the 32-window cap, override level 0-100; return per-member errors
- [ ] 2.3 Implement `ActivateProfile(id, reason, actor)`: validate → single Ent transaction (active pointer + profile theme `is_default` swap + `ProfileSwitch` row) → publish a `GlobalBus` event → return the switch summary; a validation failure must leave the prior pointer/theme untouched
- [ ] 2.4 Implement idempotent re-apply: activating the already-active profile only records the audit attempt; startup re-applies the persisted `active_profile_id` and falls back to the default profile when missing/disabled
- [ ] 2.5 Implement fallback behavior: deleting/disabling the active profile activates the fallback profile with reason `fallback`; deleting a theme referenced by the active profile keeps the built-in default; expose `DefaultProfile()` and `ActiveProfile()` helpers
- [ ] 2.6 Unit tests: valid activation commits pointer+theme in one transaction, invalid member changes nothing, duplicate activation is idempotent, startup re-apply, delete-active → fallback, switch audit rows written

## 3. Feed, Brightness, Theme, Overlay & Rule Integration

- [ ] 3.1 Extend `composeDeviceSources` (`handlers/websocket.go:382`) with the profile director playlist for effective `global` content mode only (device explicit > group > global): resolve the profile playlist through `buildSourceIndex`; disabled/missing/empty → warn once and continue to `loadSources`, preserving the existing fallback ladder
- [ ] 3.2 Wire profile switching into the 60 s `reschedule` boundary (`handlers/websocket.go:887`) and the bus event so global-mode connections re-compose with the new director playlist without a reconnect
- [ ] 3.3 Add the inherited profile brightness layer to the device/push brightness resolution (`handlers/websocket.go` bFn, `handlers/feed_render.go` `pushBrightness`): apply only when effective brightness is not explicitly enabled; profile override beats profile schedule; explicit device/group config always wins; alarm/scene tiers unchanged
- [ ] 3.4 Extend `effectiveBrightnessConfig` call sites/tests so profile brightness never changes precedence `override > alarm > scene > sensor > schedule > 100`
- [ ] 3.5 Theme integration: `ActivateProfile` swaps the default theme inside the activation transaction; `ResolveTheme` keeps per-source assignment → default → built-in precedence and `themeCacheSig` invalidates cached frames on switch
- [ ] 3.6 Overlay integration: add a profile-overlay default applied only when the effective device/group overlay is disabled; scene overlays still replace it (`applyOverlayWithScene`) and notification/incident frames are untouched
- [ ] 3.7 Event-rule filter: evaluator `loadRules` (`handlers/eventrules.go:347`) selects rules where `profile_id IS NULL OR profile_id = active`; reload on the existing 30 s ticker and on the profile-switch bus event; untagged rules always evaluate
- [ ] 3.8 Tests: global device uses director playlist; playlist/scheduled device untouched; group explicit beats profile; dangling director playlist falls back without blanking; explicit brightness/overlay wins; notification/incident/pinned-rule precedence unchanged (extend `group_precedence_test.go`/`feed_control_pin_test.go` style tests)

## 4. Time-of-Day Profile Scheduling

- [ ] 4.1 Implement pure `ResolveScheduledProfile(now, profiles, fallback)` reusing `ScheduleWindow`/`WindowMatches`: highest matching window priority wins, ties by profile order/id, no match → fallback profile; use `ScheduleNow` as the injectable clock
- [ ] 4.2 Implement `selection_mode` handling: manual activation switches to `manual`; add a "resume schedule" action that switches back to `scheduled`; scheduled mode activates the winner only on change (no-op when the winner is already active)
- [ ] 4.3 Add the profile pass to `runEvaluator` (`handlers/eventrules.go:326`) on the existing 1 s tick with the 30 s config reload — no new goroutine, ticker, or cron
- [ ] 4.4 Implement `NextProfileSwitchTime(now, profiles)` for the admin/`resolve` badge (minute-scan pattern mirroring `NextSwitchTime`, not on the feed hot path)
- [ ] 4.5 Unit tests: simple/weekday/overnight windows, priority and tie-break determinism, no-match fallback, manual activation wins and switches mode, scheduled switch fires once at the boundary, injected-clock reproducibility

## 5. Profile Admin, API & Audit Surface

- [ ] 5.1 Add admin routes `/admin/profiles` (list), `/admin/profiles/new|:id/edit` (CRUD), activate, resume-schedule, delete with the existing session/admin auth and flash pattern; reject deleting the fallback profile and deleting the active profile without confirmation (delete → fallback)
- [ ] 5.2 Add `web/templates/admin/profiles.html` + form template with member pickers (playlist, theme, brightness editor, overlay editor, schedule window editor, tagged-rule multi-select) and show active profile, selection mode, last switch and next switch
- [ ] 5.3 Add API routes in `handlers/server.go`: `GET /api/profiles` (session/bearer, viewer-readable), `POST /api/profiles/:id/activate` (admin only, bearer/session), `GET /api/profiles/resolve?at=RFC3339` (session/bearer) returning resolved profile, matched window, mode, next switch
- [ ] 5.4 Record the actor identity (session user or API token label) on activation and render switch history from `ProfileSwitch` plus the singleton's last-switch fields
- [ ] 5.5 Tests: viewer GET 200, viewer activate 403, anonymous 401, activate persists, resolve endpoint time-travels with `at`, fallback/rollback history visible
- [ ] 5.6 Keep a single exported `ActivateProfile` entry point used by admin, API, scheduler and fallback so a future HA `select` entity can call it without refactoring (document the seam in a comment; no HA discovery change in this change)

## 6. Engagement Signal Capture & Bounded Store

- [ ] 6.1 Add `CurrentKey` to `FeedController` (`handlers/feed_control.go`) set by `SetCurrent`; keep `CurrentName` behavior unchanged for API/`Status`
- [ ] 6.2 Attribute skips by canonical key in `FeedController.Next()` (replace `RecordSkip("", 0, cur)` with the current key), keeping the unattributed counter for the no-current-source case
- [ ] 6.3 Record pin signals in `FeedController.Pin` (deduplicated while the same key stays pinned) and pause/resume signals with closed dwell duration in `Pause`/`Resume`
- [ ] 6.4 Add `handlers/engagement.go`: `EngagementSignal{Key SourceKey, Kind, At, DurationSeconds}`, a concurrency-safe bounded ring buffer with global/per-source/per-kind caps and oldest-first eviction, plus a snapshot accessor for pure aggregation
- [ ] 6.5 Replace name-based display recording with canonical keys: `TrackDisplay(sw.cacheKey, ...)` in `serveFeed` (`handlers/websocket.go:1459`) and the push runner (`handlers/output_runner.go`), keeping `TrackDisplay`'s signature or adding `RecordDisplay` with the canonical key
- [ ] 6.6 Unit tests: display/skip/pin/pause attribution all share one canonical key, repeated pin evaluations record one signal, pause+resume yields one bounded dwell, caps evict oldest, unattributed events never create per-source counts

## 7. Engagement Weight Computation — Pure Module

- [ ] 7.1 Extend `handlers/adaptive_weights.go`: `AdaptiveConfig` gains `HoldWeight`, `DwellWeight`, `TargetDwellSeconds`, `HoldCap`, `DwellCap`, `DwellCapSeconds`, `MinDwellSeconds`, signal-class toggles and excluded sources; update `DefaultAdaptiveConfig` with conservative defaults (all signal classes on, capped boosts)
- [ ] 7.2 Implement `AggregateEngagement(signals, candidates, now, cfg) map[SourceKey]SignalStats`: per-kind decay by `HalfLifeDays`, drop signals older than `WindowDays`, bounded per source, no randomness
- [ ] 7.3 Implement `ComputeEngagementWeights(stats, cfg) map[SourceKey]float64` with `score = decayedDisplays * (1 - beta*skipRate) * (1 + holdBoost + dwellBoost)`, capped boosts, cold-start equality, epsilon smoothing, floor + `floor*N` cap and renormalization
- [ ] 7.4 Keep `ComputeWeights(displays, skips, cfg)` as the compatible kernel wrapper so the existing `adaptive_weights_test.go` cases and the unarchived `adaptive-ordering` behavior continue to pass unchanged
- [ ] 7.5 Point `weightsCache.RecomputeFromAnalytics` at the signal store + new pipeline (no DB read), keeping the 5-minute ticker and 1 s debounced skip trigger
- [ ] 7.6 Unit tests: decay favors recent, pin/pause raises weight, dwell cap bounds the boost, low-display skip trust unchanged, floor/exploration/sum-to-1.0, determinism for identical inputs, excluded source gets base share only, all-signals-off reproduces displays+skips weights

## 8. Adaptive Rotation — Global Mode Only

- [ ] 8.1 Guard `nextPushIndex` (`handlers/feed_render.go:68`) so adaptive weighting applies only when the caller signals global mode; pass the effective global/playlist decision from `handlers/output_runner.go` so a playlist/scheduled device is never reordered
- [ ] 8.2 Restructure the `serveFeed` slot selection (`handlers/websocket.go:1288-1304`) so `ordering_mode=adaptive` in effective `global` mode actually selects the next source via `WeightedRandom`, while playlist/scheduled connections keep saved-order iteration and `random`/`sequential` behavior is unchanged
- [ ] 8.3 Ensure playlist/scheduled connections still set `randomFlag=false` and never consult the weights cache; adaptive with an empty cache falls back to uniform random and never yields an empty selection
- [ ] 8.4 Tests: sampled frequencies track injected weights for a global adaptive connection; playlist feed order is byte-stable under adaptive; scheduled feed order is stable; random/sequential ignore weights; empty cache fallback serves a valid frame
- [ ] 8.5 Verify push and WebSocket transports agree on the same global-mode-only rule (shared helper + tests)

## 9. Explainability, Settings & Opt-Out

- [ ] 9.1 Extend `GET /api/analytics/weights` (`handlers/analytics_weights_api.go`): fix the `string(rune(k.ID))` key mangling to canonical `type:id` (strconv), keep all existing keys, add `sources[]` with raw counts, decayed displays, skip rate/penalty, hold/dwell boosts, floor/cold-start flags and closed-vocabulary `reasons[]`
- [ ] 9.2 Add policy fields to `ent/schema/generalsettings.go`: `adaptive_signals` JSON (displays/skips/pins/pauses/dwell), `adaptive_excluded_sources` JSON, `adaptive_hold_weight`, `adaptive_dwell_weight`, `adaptive_target_dwell_seconds`; run `go generate ./ent`
- [ ] 9.3 Extend the settings GET/POST path (`handlers/handlers.go`) with admin-only validation of the new knobs and toggles (half-life 1-30, window 1-90, floor 0-0.2, epsilon 0-0.5, boost weights 0-2, target dwell 5-600); out-of-range → 400; viewers 403 on mutation
- [ ] 9.4 Update `web/templates/admin/analytics.html` (weight breakdown table, reason chips, collecting-data badge) and the settings page (signal toggles, exclusion editor, boost knobs with the read-only caps labeled); update the TypeScript and run `npm run build`
- [ ] 9.5 Tests: response keys preserved, reason vocabulary is a closed set, signal toggle removes the contribution, excluded source reported `excluded`, out-of-range rejected, viewer cannot mutate, cold-start labeled

## 10. Wiring, Docs & Verification

- [ ] 10.1 Wire profile routes/API, startup seeding, scheduler pass and the bus event in `handlers/server.go`; add sidebar/nav entries for profiles and show the active profile on the dashboard/devices view
- [ ] 10.2 Update `README.md`: profiles and selection modes, profile scheduling semantics and precedence (`notification > incident > alarm > scene > pinned rule > profile/rotation`), engagement signals, explainability, opt-out knobs, and the unchanged playlist/random contract
- [ ] 10.3 Add integration-style tests: profile switch while an incident is active leaves the incident rendering; scheduled profile switch activates at a window boundary with an injected clock; global adaptive weights distribution; playlist device order stable across a profile switch
- [ ] 10.4 Run `task pre-push` (gofmt, tests, build) and the frontend build; fix all failures
- [ ] 10.5 Run `openspec validate --strict add-adaptive-director` and confirm status shows proposal, design, specs and tasks done
