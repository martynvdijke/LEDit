## Context

LEDit is a single Go/Gin/SQLite/Ent server that renders datasources to LED walls over WebSocket (`serveFeed`, `handlers/websocket.go:1050`) and push transports (`handlers/output_runner.go`). Content selection today has three device modes — `global`, `playlist`, `scheduled` (`ent/schema/device_settings.go:33`, `composeDeviceSources` `handlers/websocket.go:382`) — and one system-wide ordering mode — `ordering_mode ∈ sequential|random|adaptive` on the `GeneralSettings` singleton (`ent/schema/generalsettings.go:35`, tuning columns `adaptive_floor`, `adaptive_half_life_days`, `adaptive_window_days`, `adaptive_epsilon`). Adaptive weights are computed by a pure kernel (`ComputeWeights`, `handlers/adaptive_weights.go:42`) from in-memory display and skip events (`handlers/analytics.go:8-41`), cached in `weightsCache` (`handlers/adaptive_weights.go:174`), recomputed on a 5-minute ticker plus a debounced trigger (`triggerSkipRecompute`, `handlers/adaptive_weights.go:259`) and exposed by `GET /api/analytics/weights` (`handlers/analytics_weights_api.go:9`).

Two system-level gaps remain:

1. **No whole-system mode.** Playlists, event rules (`DisplayRule`, `handlers/eventrules.go`), brightness (`ResolveBrightness`/`ResolveEffectiveBrightnessWithScene`, `handlers/brightness.go:146-209`), the default theme (`themeResolver.resolve`, `handlers/theme.go:53`) and the per-device overlay strip (`overlaySpecForDeviceWithGroup`) are each configured independently and per device. There is nothing that says "the wall is in Evening mode now"; switching means many manual edits with no atomicity, no audit trail and no one-click rollback.
2. **Adaptive curation is blind to the strongest signals.** `ComputeWeights` sees only `displays`/`skips`. It ignores that an operator *pinned* a source (`FeedController.Pin`, `handlers/feed_control.go:151`), *paused* on it (`FeedController.Pause`, `:75`) or dwelled on it for minutes; skips are attributed by name label (`RecordSkip("", 0, cur)` at `:106`), while displays are recorded by display name (`TrackDisplay(sw.Name, ...)` at `handlers/websocket.go:1459`) and the weights API stringifies `SourceKey.ID` with `string(rune(k.ID))` (`handlers/analytics_weights_api.go:27`) — so signal attribution is inconsistent and unexplainable.

Constraints: one process, SQLite, in-memory caches are acceptable but must be bounded and deterministic; `modernc`-style pure-Go stdlib math only; feed tier precedence `notification > incident > alarm > scene > pinned rule > rotation` is frozen (`handlers/websocket.go:1242-1304`); a wall must never blank; device playlists play in saved order and `random`/shuffle applies to global mode only (`README.md` "Device Playlists", "Random/Sequential Ordering"); scheduled playlists already own the time-of-day playlist problem (`handlers/schedule.go`, `add-scheduled-playlists`) and must not be duplicated.

Stakeholders: homelab operators running several walls that should all change character together (workday, evening, party, guest); households that benefit when the wall learns what they actually watch; monitoring users who require that no profile or weight change can hide a notification or incident.

## Goals / Non-Goals

**Goals:**
- Named profiles that atomically switch a director playlist, profile-tagged event rules, brightness, default theme and overlay, with validated apply, audit log and rollback to a seeded Default profile.
- Profile time-of-day scheduling that reuses the existing window matching/resolution primitives and the existing evaluator loop — no cron.
- A documented, complete precedence model: feed tiers unchanged, profile as an inherited configuration layer, device/group explicit settings always win, never-blank fallback at every step.
- Canonical engagement signal capture (display, dwell, skip, pin, pause) plus deterministic decayed weighting with hold/dwell boosts, explainability and opt-out.
- Adaptive selection applies to effective `global`-mode rotation only; playlist/scheduled saved order and `sequential`/`random` modes are untouched.

**Non-Goals:**
- A Home Assistant `select` entity for profiles — the scope is "later"; this change only reserves the activation seam (a single `ActivateProfile` entry point) so an HA discovery entity can be added additively.
- Per-device or per-group profile scopes — v1 profiles are system-wide singletons; per-device profiles would multiply the inheritance matrix for little benefit now.
- A "quiet"/incident-suppressing profile member — profiles must never hide notifications or incidents; overlay is default-only, and incident state is never cleared.
- Replacing `ComputeWeights` with bandits/ML, per-person personalization, or cross-device weight keying.
- Duplicating the schedule machinery: no `robfig/cron`, no new ticker, no new goroutine; the existing `runEvaluator` tick is the only clock.
- A new database table for engagement events — the existing bounded in-memory analytics pattern is extended.

## Decisions

### D1 — Two capabilities (`system-profiles`, `engagement-curation`), not one `adaptive-director`

Profiles are deterministic configuration transactions; engagement curation is statistical signal processing. They have different failure domains (a failed profile apply must be atomic and reversible; a stale signal must merely decay), different test strategies (pure window/apply tests vs distribution/decay tests), and independent opt-outs (an operator may want profiles without adaptive rotation, or adaptive rotation without profiles). One capability would force one requirement set to describe both, and a bug in the weighting math would be archived under the same spec as profile atomicity. The umbrella term "director" is a design concept documented here, not a spec container.

- *Alternative:* single `adaptive-director` capability — rejected: couples unrelated lifecycles and makes archive-time requirement sets less precise.
- *Alternative:* three capabilities incl. `profile-scheduling` — rejected: scheduling is a property of profile selection, not an independently deployable feature; splitting it would scatter the precedence model.

### D2 — Profiles are an inherited configuration layer, not a device-config rewrite

A profile resolves *only where configuration is inherited*. Effective device content mode (device explicit > group > default `global`) decides whether the profile director playlist applies; device/group brightness and overlay settings win when explicitly enabled; per-source theme assignments always win over the profile's default theme. The profile never mutates `DeviceSettings` rows.

- *Why:* the README contract ("Playlists play in saved order; Random/shuffle applies to global mode only") and the existing `composeDeviceSources` precedence stay intact, rollback is a single pointer change, and there is no destructive snapshot of every device's hand-tuned settings to maintain.
- *Alternative:* profiles snapshot and rewrite full device configuration — rejected: invasive, lossy on rollback, and would silently change playlists for devices the operator never associated with the profile.
- *Alternative:* profiles as a new feed tier that pins a source — rejected: would break the frozen `notification > incident > alarm > scene > pinned rule > rotation` precedence by inserting a content tier above the pin.

### D3 — `SystemProfile` entity + `ProfileSettings` singleton; profile-tagged `DisplayRule`

New `ent/schema/system_profile.go`: `name` (non-empty, unique), `description`, `enabled`, `is_fallback` (the seeded Default), `playlist_id` (nullable), `brightness` (JSON: enabled/schedules/override, same shape as the device brightness columns), `theme_id` (nullable), `overlay` (JSON: enabled/position/height/text/speed/bg/fg, same shape as the device overlay columns), `schedule_windows` (JSON, reused `ScheduleWindow`), `created_at`/`updated_at`. New `ent/schema/profile_settings.go` singleton: `active_profile_id` (nullable), `selection_mode` (`manual|scheduled`, default `manual`), `last_switch_at`, `last_switch_reason`, `last_switch_from`. A small `ent/schema/profile_switch.go` audit log records each `from → to`, reason, actor and timestamp (pruned to the most recent N entries). `ent/schema/displayrule.go` gains nullable `profile_id`: untagged rules always evaluate, tagged rules evaluate only while their profile is active.

- *Why:* mirrors the existing singleton pattern (`FirmwareSettings`, `WebhookSettings`) and JSON-columns-for-bundles pattern (`Scene.Actions`, `Playlist.ScheduleWindows`); one active-pointer update switches the rule set atomically without touching any rule row.
- *Alternative:* mutate `DisplayRule.enabled` flags on switch — rejected: destructive to operator intent, non-atomic across rows, and hard to roll back.
- *Alternative:* a join table of rule sets per profile — rejected: a nullable tag is one column and no join query on the evaluator hot path.
- *Alternative:* store profile config by reference to named "brightness profiles"/"overlay profiles" — deferred: inlining the existing JSON shapes keeps v1 simple; reuse can be added without a migration break because the field is JSON text.

### D4 — Atomic validated activation with a seeded, non-deletable Default profile

Activation performs: (1) load and validate the profile (`enabled`, referenced playlist/theme exist, JSON parses, ≤32 windows); (2) resolve every member to a concrete target and reject 400 with per-member errors if a *referenced* member is structurally invalid; (3) commit the active pointer, the derived global mutations (default theme swap via `is_default`) and the `ProfileSwitch` audit row in one Ent transaction; (4) publish an event so open feeds re-compose at their next boundary and log `from → to`, reason (`manual|scheduled|api|fallback`), actor and the active rule set. Re-applying the same active profile is idempotent; startup re-applies `active_profile_id` (missing profile → Default). Deleting the active profile, or disabling it mid-flight, activates Default. Default is seeded with `is_fallback=true`, cannot be deleted, and is always activatable.

- *Why:* one transaction boundary gives all-or-nothing semantics for the only mutable global state (pointer, theme default, log); members resolved at runtime keep working even if a playlist/theme is edited later.
- *Alternative:* best-effort partial apply with per-member rollback — rejected: partial profiles are the "half-applied scene" failure mode `SceneManager.Evaluate` already refuses (`handlers/scenes.go:650`), and rollback of already-published overlays/themes is racy.
- *Risk:* a profile that references a playlist which later loses all items; runtime resolution handles this with the fallback ladder rather than failing the activation.

### D5 — Never-blank fallback ladder for every member

Director playlist resolution order at feed composition time: profile playlist (if the effective content mode is `global`) → device's effective global list (`loadSources`) → idle screensaver fallback (`idleFallback`, `handlers/websocket.go:688`) → keep the current source list. A dangling/disabled/empty profile playlist is skipped with one warn log per connection and the ladder continues. Brightness/overlay members that are malformed are ignored (existing defaults), theme missing falls back to the built-in default (`themes.DefaultTheme`). No member can produce an empty source list.

- *Why:* the fallback ladder already exists and is tested for scheduled playlists (`handlers/websocket.go:652-673`); profiles must reuse it rather than invent a second policy.
- *Alternative:* fail closed (skip rotation) — violates "never blank a wall".

### D6 — Scheduling reuses `ScheduleWindow`/`WindowMatches`; selection mode is explicit; evaluation rides the existing evaluator tick

Profiles reuse `ScheduleWindow`, `ParseScheduleWindows`, `ValidateWindows`, `WindowMatches` and the ranking shape of `ResolveScheduledPlaylist` (`handlers/schedule.go`) in a new pure `ResolveScheduledProfile(now, profiles) *SystemProfile` (highest matching window priority wins, ties by profile order/id, no match → fallback profile). `selection_mode=manual` means operator activation persists and the scheduler is inert; `scheduled` means the evaluator computes the winner each tick and activates on change (cheap: ≤N profiles × ≤32 windows, same cost class as device schedule resolution). Evaluation happens inside `runEvaluator` (`handlers/eventrules.go:326`, 1 s tick, 30 s config reload) with no new goroutine or ticker, and uses the injectable `ScheduleNow` clock for tests.

- *Why:* the add-scheduled-playlists change deliberately reused pure functions plus the evaluator; a director profile scheduler that introduced cron would be a second, divergent time engine (timezone, sunrise/sunset, holidays) to keep correct.
- *Alternative:* `robfig/cron` or a new minute ticker — rejected: duplicate machinery, worse testability, and it would not honor `WindowMatches`' holiday/sunrise handling.
- *Precedence inside scheduling:* manual activation switches `selection_mode` to `manual` (so it sticks, matching "manual always wins" from ambient scenes); an admin "Resume schedule" action flips back to `scheduled`. Documented in the spec scenarios.

### D7 — Profile theme swaps the *default* only; per-source assignments still win

Activating a profile with `theme_id` sets that theme's `is_default` (demoting the current one) inside the same transaction; the profile stores its own `default_theme_id` implicitly (null = built-in default). `themeResolver.resolve` is unchanged: per-source `ThemeAssignment` → default theme → built-in. Default-profile activation restores the baseline theme recorded in the Default profile row.

- *Why:* `ResolveTheme` is deliberately uncached per render (`handlers/theme.go:28-31`); a profile override branch would add a read on every frame for every source. Swapping the default is O(1), atomic with the pointer, and already the model used by `AdminThemeSetDefault`.
- *Alternative:* add a profile layer inside `resolve` — rejected: per-render DB read plus cache-signature churn (`themeCacheSig` already invalidates on theme change, so the swap correctly invalidates cached frames).

### D8 — Profiles never suppress notifications or incidents; "overlay/incident state" means overlay default + audited switching

The profile overlay member is an inherited default applied only when the effective device/group overlay is disabled; `applyOverlay` and `applyOverlayWithScene` are unchanged, so scene overlays, notification frames and incident frames still win. Profiles cannot clear `CurrentIncidentScene`, cannot resolve incidents, and have no member that disables the notification or incident tiers. Switching profiles while an incident is active is allowed and logged; the incident keeps rendering.

- *Why:* incidents are safety signals from monitoring; letting a "Party" profile hide them would make the system less trustworthy than before the feature. The scope's "overlay/incident state" is implemented as the overlay baseline plus the guarantee that profile switching never alters incident state.
- *Alternative:* include an `incident_sensitive`/mute member — rejected for v1; can be added as an explicit, separately-specced capability if ever demanded.

### D9 — Canonical `SourceKey` attribution and a bounded in-memory engagement log

All signals use the canonical cache key `"<type>:<id>"` (`sourceWithName.cacheKey`, as used by LKG and `ResolveTheme`). `FeedController` gains `CurrentKey` set by `SetCurrent`; `Next()` attributes the skip to `CurrentKey`; `Pause()` records the current key; `Resume()` closes the dwell window. `TrackDisplay` is called with `sw.cacheKey` instead of `sw.Name`. Signals live in a bounded ring buffer in `handlers/engagement.go` (global cap, per-source cap, per-kind caps; oldest dropped), mirroring the existing 1000-event analytics pattern (`handlers/analytics.go:38`). Unknown keys fall to the existing unattributed counter without corrupting per-source counts; a repeated pin for the same key is deduplicated while pinned.

- *Why:* the current mismatch (name-keyed displays vs type-keyed skips, `string(rune(ID))` in the API) is the root cause of unexplained weights; one canonical key also makes the explainability endpoint, decay windowing and tests straightforward.
- *Alternative:* persist signals in a new Ent table — rejected: adds migration and per-event writes for data that is only meaningful in a rolling window; in-memory boundedness matches the existing analytics contract and restart behavior (relearning is acceptable and documented).
- *Alternative:* reuse `analytics_weights_api.go`'s string mapping — rejected: it mangles IDs (`string(rune(3))` is `"\x03"`).

### D10 — Weight math: decayed signal stats + capped hold/dwell boosts composited with the existing kernel

New pure functions in `handlers/adaptive_weights.go`:

```
AggregateEngagement(signals []EngagementSignal, candidates []SourceKey, now, cfg) map[SourceKey]SignalStats
    // decay each signal by exp(-ln2 * age / HalfLifeDays); per-kind bounded counts/seconds
ComputeEngagementWeights(stats, cfg) map[SourceKey]float64
    // weightedDisplays D, trusted skipRate = Ws/D (only if rawDisplays >= MinDisplaysForSkipTrust)
    // holdBoost  = min(HoldCap, HoldWeight * (pins + pauses) / (D + 1))
    // dwellBoost = min(DwellCap, DwellWeight * min(dwellSeconds, DwellCapSeconds) / TargetDwellSeconds)
    // score = D * (1 - beta*skipRate) * (1 + holdBoost + dwellBoost), clamped >= 0
    // then the existing epsilon smoothing, floor + renorm, cold-start equality, floor*N cap
```

`ComputeWeights(displays, skips, cfg)` stays as the kernel wrapper so the unarchived `adaptive-ordering` behavior and its tests keep passing; the rational pipeline is `AggregateEngagement → ComputeEngagementWeights`. Caps (`HoldCap`, `DwellCap`, `DwellCapSeconds`, `MinDwellSeconds`) and caps for dwell/pause are constants exposed on the admin settings page as read-only labels; the tuning knobs (`HalfLifeDays`, `WindowDays`, `Floor`, `Epsilon`, `HoldWeight`, `DwellWeight`, `TargetDwellSeconds`) are editable. All operations are O(signals + sources) over bounded inputs; there is no randomness in the aggregation, and weighted sampling keeps using the existing single `rand` draw.

- *Why:* pins/pauses/dwell are sparse, high-intent events; unbounded boosts would let one long pause dominate forever, so caps + decay keep the loop stable. Reusing the floor/exploration kernel preserves the anti-feedback guarantees already specified.
- *Alternative:* Thompson sampling / bandits — rejected: opaque, defeats explainability and determinism; the existing closed-form kernel is auditable.
- *Alternative:* treat pause as a negative signal — rejected: a deliberate pause on a source is a hold, not a skip; skipping after resume still applies the skip penalty.

### D11 — Adaptive selection is only for effective `global` mode; playlist/scheduled saved order is frozen

`serveFeed` currently computes an adaptive `nextName` label (`handlers/websocket.go:1292`) but the actual slot order is the slice iteration; `nextPushIndex` (`handlers/feed_render.go:68`) really selects and ignores the playlist guard. The change makes selection consistent: when the connection's effective content mode is `global` *and* `ordering_mode=adaptive`, the next slot index is drawn from `WeightedRandom` (with uniform-random fallback on an empty cache); when mode is `playlist`/`scheduled` (explicit or via group), or `ordering_mode` is `random`/`sequential`, selection is exactly today's behavior (`randomFlag=false` for playlists, `handlers/websocket.go:811-815`). `nextPushIndex` gains the same guard so push transports cannot reorder a playlist.

- *Why:* this is the literal README contract and the user-visible promise that a device playlist is authored, not learned; it also fixes the current inconsistency where adaptive affects the advertised "next" differently between WS and push transports.
- *Risk:* changing the serveFeed loop to select (not merely advertise) slightly alters rotation cadence for adaptive-mode global devices; covered by distribution tests and the unchanged random/sequential paths.

### D12 — Explainability is data, not prose: per-source breakdown on the existing endpoint

`GET /api/analytics/weights` keeps its existing keys (`weights`, `displays`, `skips`, `computedAt`, `config`, `collectingData`, `floorClampedSources`) and adds `sources[]` with `key`, `label`, `weight`, raw `displays/skips/pins/pauses/dwellSeconds`, `decayedDisplays`, `skipRate`, `skipPenalty`, `holdBoost`, `dwellBoost`, `clampedByFloor`, `coldStart`, and `reasons[]` from a closed vocabulary (`high_dwell`, `frequent_skip`, `pin_hold`, `pause_hold`, `exploration`, `floor`, `cold_start`, `excluded`). The admin analytics page renders the table and reason chips. Weights without signals return the equal-weight cold-start payload with `collectingData=true`.

- *Why:* operators will not trust a ranking they cannot explain; a closed reason vocabulary is testable and prevents unbounded strings in the payload.
- *Alternative:* a free-text explanation — rejected: untestable.

### D13 — Opt-out and decay policy

- Global: `ordering_mode=random|sequential` bypasses engagement entirely (no signal reads). `adaptive` remains the opt-in.
- Signal classes: `adaptive_signals` JSON (`displays|skips|pins|pauses|dwell`, each boolean, default all true). Disabling all yields the exact pre-change displays+skips kernel.
- Per-source: `adaptive_excluded_sources` JSON list of keys; excluded sources receive the exploration/floor base share and no learning (explainable via `excluded` reason).
- Decay: every signal class decays with `adaptive_half_life_days` (default 7) inside `adaptive_window_days` (default 14); signals older than the window are dropped at aggregation.
- All of the above are persisted on the `GeneralSettings` singleton and editable on the admin settings page; validation mirrors the existing ranges (half-life 1-30, window 1-90, floor 0-0.2, epsilon 0-0.5; boost weights 0-2; target dwell 5-600 s).

- *Why:* "adaptive is opt-in, engagement is opt-out" gives a conservative rollout: an operator can try profiles, adaptive, and engagement independently; nothing changes behavior until enabled.
- *Alternative:* a hard-coded engagement policy — rejected: signs and magnitudes vary per household; the parameters are cheap to expose and the floor/epsilon kernel makes mis-tuning non-catastrophic.

### D14 — Activation seam reserved for a future HA `select` entity

All activation paths (admin form, API, scheduler, fallback) call one server method that takes `(profileID, reason, actor)`. HA discovery (`handlers/ha_discovery.go`) is not touched in this change; a later change can publish a `select` entity whose command topic calls the same method, with no spec or schema change.

- *Why:* the scope explicitly defers the HA select; centralizing activation now avoids a future refactor of four call sites.

## Risks / Trade-offs

- [Profile schedules thrash on overlapping windows] → deterministic priority + order tie-break, activation only on winner change, pure resolver with injected clock, and a `/api/profiles/resolve` debug endpoint showing the matched window and next switch.
- [Profile apply fails halfway] → validate-then-single-transaction; theme default swap is the only global row mutated besides the pointer; failure leaves the prior profile active and returns per-member errors; Default is always available.
- [A referenced playlist/theme is deleted later] → runtime fallback ladder + warn log; deleting a profile that is active reverts to Default; deleting a theme used by the active profile keeps the built-in default.
- [Profile-tagged rules silently disable critical rules] → only explicitly tagged rules switch; untagged rules always evaluate; the profile form lists tagged rules and the switch log records the active rule set.
- [Engagement feedback loop] → existing epsilon + floor; capped hold/dwell boosts; per-source exclusion; short half-life default; skip trust threshold unchanged.
- [Pause mistaken for engagement] → pause contributes a bounded hold only when a source is on-screen; resume dwell is capped; a following skip still penalizes the same key.
- [Memory growth] → ring buffers with global/per-kind/per-source caps; recompute is O(cap + sources); no DB writes per event.
- [Signal attribution drift] → canonical `type:id` keys everywhere, a test asserting name-based and key-based attribution agree, and the API ID stringification bug fixed (currently `string(rune(k.ID))`).
- [Adaptive selection change alters global rotation cadence] → playlist/scheduled/random/sequential paths untouched; distribution tests; empty-cache fallback to uniform random keeps the wall non-empty.
- [Profile overlay confuses scene/notification layering] → overlay remains default-only and never applies to incident/notification frames; D8 guarantees profile switching cannot suppress them.

## Migration Plan

1. Add `SystemProfile` and `ProfileSettings` schemas and the nullable `DisplayRule.profile_id`; run `go generate ./ent`; the migration is additive (new tables/column, defaults `manual` selection, null active profile).
2. Seed the Default profile (`is_fallback=true`, empty members) on startup; existing behavior is unchanged until an operator activates a non-default profile — the active pointer lets the code paths no-op.
3. Add `handlers/profiles.go` (resolve/apply/rollback/schedule) + `handlers/profiles_admin.go` (routes/API) and wire the feed/brightness/theme/overlay/rule-resolution seams; publish a bus event on switch and re-compose at the next 60 s scheduled boundary (`fc.reschedule`, `handlers/websocket.go:887`).
4. Add engagement signals + aggregation + settings; ship `adaptive_signals` all-on but only active when `ordering_mode=adaptive`, which is already opt-in, so a stock upgrade changes nothing.
5. Extend the weights API/admin breakdown; verify the old response keys are preserved.
6. Rollback: switch `selection_mode` to `manual` and activate Default (or clear `active_profile_id`), set `ordering_mode=random`; the new tables/column are inert if unused and can be left in place.

## Open Questions

- Should a profile membership be per-device once profiles are trusted (a "bedroom" profile scope), or stay system-wide? Assumed system-wide for v1; the `ProfileSettings` pointer is the natural place to add a scope filter later.
- Should profile members include scenes (enable/disable a scene set) as well as event rules? Assumed no: scenes are ambient reactions, rules are content intent; revisit if operators ask.
- Should the director playlist apply to devices in `scheduled` mode as their fallback playlist? Assumed no: scheduled devices keep their configured fallback chain; the profile applies only to effective `global` mode.
- Should dwell be measured per device or globally? Assumed globally aggregated in v1, matching the existing global weights cache; per-device keying is the reserved extension point.
- What is the activation latency contract for the scheduler? Assumed ≤ one evaluator tick plus the connection's 60 s reschedule boundary (worst case ~61 s), documented in the spec; a future refinement can publish a reload signal to all connections immediately.
