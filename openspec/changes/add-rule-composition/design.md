## Context

LEDit resolves event rules in a single evaluator goroutine (`StartEventRuleEngine` → `runEvaluator`, `handlers/eventrules.go:214`/`326`). Rules are `DisplayRule` rows (`ent/schema/displayrule.go`) with a target (`source_type`/`source_id`), one `condition` JSON blob, an optional `state_path`, `check_interval_seconds` (default 30, min 5), `cooldown_seconds`, and `then_actions`. Every 1 s tick the evaluator checks rules whose jittered `nextCheck` elapsed, resolves the target through `resolveTarget` + `datasource.StateProvider`, fetches `CurrentState(ctx)` once, scopes the map via `resolveStatePath`, parses the condition with `datasource.ParseCondition`, and evaluates with `datasource.Evaluate` (`datasource/eventrule.go:38`). A true result calls `pinAll(cacheKey, name)` and on a false→true edge runs `executeThenAction`; a false result unpins once `cooldownUntil` passes. Rules reload from the DB every 30 s, targets without `StateProvider` are skipped with a once-per-start log, and fetch failures evaluate as an empty map. Precedence is notifications > pinned rule > rotation, and manual `Next()` (`handlers/feed_control.go:91`) clears `PinnedKey` while the rule keeps `rs.pinned=true`, so the wall stays released until the condition drops and rises again.

The engine's extension points are thus: the `condition` JSON and its parse/evaluate functions, `ruleState` (the per-rule scheduler/timer bag), the tick body in `runEvaluator`/`EvaluateRulesOnce`, `validateEventRule` (`handlers/eventrules.go:753`), and the admin form that currently posts one `{path, operator, value}` object into a hidden `condition` field (`web/templates/admin/eventrule_form.html:30`).

Constraints: one process, SQLite/Ent, no new scheduler or goroutines; the existing interval floor, jitter, reload, precedence, pin/cooldown, and skip contracts must not change; existing rule rows must keep evaluating identically; and auto-evaluation must not add per-rule or per-condition fetch loops — it must reuse the single `CurrentState` fetch and the shared `datasource.DotPath` resolver.

Stakeholders: homelab operators authoring compound, persistent, and ordered triggers on the Admin → Event Rules page.

## Goals / Non-Goals

**Goals:**
- Compose a trigger from multiple state conditions with AND/OR grouping, bounded in depth and size.
- Require a condition to hold continuously for a configured duration before firing.
- Express ordered triggers (`A`, then `B` within a window) with per-step hold durations.
- Preserve every existing rule contract: precedence, cooldown both edges, manual skip, non-capable-target safety, fetch-failure semantics, interval floor/jitter, and the 30 s reload.
- Keep evaluation in the one goroutine over one fetched state map per rule per check, reusing `datasource.DotPath`.
- Give the admin form a usable builder with server-side validation and readable list summaries.

**Non-Goals:**
- NOT/XOR/arithmetic/time-of-day expression operators or a general expression language.
- Per-leaf sustained timers (the hold is rule-level; a later change can add a leaf `for` without breaking this format).
- Per-step `after_seconds` delays (holds cover the stated cases; deferred).
- Cross-rule state, rule chaining, or sequences longer than 4 steps.
- Device-side or protocol changes; webhooks/MQTT as trigger sources.
- Changing precedence, the pin/cooldown contract, or the evaluation cadence.

## Decisions

### D1 — Compose inside the existing `condition` column: recursive AND/OR groups over legacy leaves, bounded

The `condition` JSON becomes a node tree. A leaf keeps today's exact shape `{"path":"garage.door","operator":"eq","value":"open"}`; a group is `{"op":"and"|"or","conditions":[<node>,...]}`. Validation (`ParseConditionNode`) enforces: `op` is exactly `and` or `or`; groups have ≥2 children; `path`/`operator`/`value` are not mixed onto groups; leaves use the existing operator whitelist; maximum 4 nested group levels; maximum 32 leaves per rule. `ParseCondition`/`Evaluate` stay untouched for existing callers, and the new evaluator uses `ParseConditionNode`/`EvaluateNode`.

- *Why:* zero migration (legacy rows parse as leaves), one canonical JSON shared by server, form, and simulate endpoint, and the existing hidden-field plumbing already carries a JSON blob.
- *Alternative:* a `rule_conditions` child table — rejected: joins and per-condition rows for a small bounded tree, plus form plumbing for N children.
- *Alternative:* flat CNF/DNF arrays — rejected: loses operator grouping, worse for the builder and error messages.
- *Risk:* a hand-edited or malicious tree could be huge; the bounds are enforced at parse time and again at save, and deep trees short-circuit like any boolean tree.

### D2 — Composed evaluation is a pure function over one fetched state map

`runEvaluator` keeps fetching `CurrentState` once per rule per scheduled check, scopes it once with `resolveStatePath` (empty `state_path` = whole map, unchanged), and calls the pure `EvaluateNode(state, node)`. AND short-circuits on the first false leaf, OR on the first true leaf. Leaves resolve with `datasource.DotPath`; `exists` is true when the path resolves; any other operator on an unresolvable path is false; numeric/string/bool/contains semantics are exactly today's `datasource.Evaluate` behavior, which the leaf evaluator reuses rather than reimplements.

- *Why:* reuses the existing fetch cadence and the shared resolver (`datasource/dotpath.go`), so there is no new polling loop and no change to upstream load; pure function = trivially unit-testable.
- *Alternative:* fetch state per condition — rejected: multiplies upstream requests and contradicts the scope's no-new-polling requirement.

### D3 — Sustained conditions: rule-level `for_seconds`, observed continuously at the check cadence

Add `DisplayRule.for_seconds` (int, default 0, min 0). `ruleState` gains `conditionSince`: the first tick where the composed condition was true after a false/unknown tick. The rule fires only when the condition is true on the current tick and `now - conditionSince >= for_seconds`; a false tick (including a fetch-failure tick, which evaluates as an empty map per existing semantics) clears `conditionSince`. 0 reproduces today's immediate firing. Hysteresis is *not* a new field: the existing `cooldown_seconds` keeps its exact two-edge contract (minimum hold after firing, no re-fire after release).

- *Why:* the real use cases ("door open for 10 minutes", "CPU above 80 for 5 minutes") are about the whole trigger persisting; one timer per rule is cheap and testable.
- *Alternative:* per-leaf `for_seconds` in the node JSON — deferred: requires timer state per node and complicates the builder; the rule-level hold covers the requirement and the format stays extensible.
- *Risk:* a transient fetch failure resets the hold and effectively restarts the clock; documented, logged once per outage window (`logStateFetchOnce`), and operator-visible. Preserving the existing "failure = false" rule matters more than timer elegance.
- *Validation:* `for_seconds` must be 0, or between the clamped check interval (`max(5, check_interval_seconds)`) and 86400 s; a hold shorter than one check cannot be observed reliably, so it is rejected with a clear message.

### D4 — Sequences are an explicit trigger mode: `trigger_kind` + `sequence` JSON

Add `trigger_kind` (string, default `condition`) and `sequence` (text, default `""`). When `trigger_kind = sequence`, `sequence` holds `{"window_seconds":900,"steps":[{"condition":<node>,"for_seconds":600},...]}` with 2-4 ordered steps. Validation (`ParseSequence`): step count 2-4, `window_seconds` in 5..86400, each step's `for_seconds` 0..86400 (and 0 or ≥ the clamped check interval), each step's condition a valid composed node, and `for_seconds` on the rule itself must be 0. When `trigger_kind = condition` the `sequence` field must be empty and `for_seconds` applies. Empty/absent `trigger_kind` reads as `condition`, so legacy rows are untouched.

- *Why:* an explicit mode makes validation and the form unambiguous ("this rule is a condition or a sequence, not a bit of both") and keeps legacy inference trivial.
- *Alternative:* infer sequence mode from a non-empty `sequence` field — rejected: partial saves and UI round-trips become ambiguous, and mode conflicts lose a clear error message.
- *Alternative:* store steps as a top-level `{"op":"sequence"}` node inside `condition` — rejected: nests a stateful, timed construct inside the pure boolean tree, mixing two evaluation models in one parser.

### D5 — Sequence state machine: arm → advance → fire once → hold → release

Sequence state lives in `ruleState` (`seqStep`, `stepSince`, `armedAt`) and is advanced by the same pure per-tick helper as D3:

1. **Idle** (`seqStep = -1`): evaluate step 0. When its condition holds for `for_seconds`, mark it complete, set `armedAt = now`, advance to step 1, and start step 1's hold at `now`. A step that goes false simply clears `stepSince` and waits (the window bounds the wait); it does not kill the run.
2. **Active** (step `k ≥ 1`): if `now - armedAt > window_seconds`, reset to idle *without* firing. Otherwise evaluate step `k`; when it holds for its own `for_seconds`: if it is the last step, **fire** (pin + `executeThenAction` + `pinned=true` + `cooldownUntil = now + cooldown`) and enter **held**; otherwise advance to `k+1` and start its hold.
3. **Held** (after firing): keep the pin while the final step's condition still matches, refreshing the min-hold like the condition-true/pinned branch does today; when it stops matching and `now ≥ cooldownUntil`, unpin, reset to idle, and set `cooldownUntil = now + cooldown` for the no-re-fire edge.
4. A new run can only arm from idle, so re-fire requires the previous run to release. Windows and cooldown bound the repeat rate when step 0 stays true.

- *Why:* "A then B within a window" is inherently a timed, stateful trigger; keeping the state in the evaluator's per-rule bag preserves the single-goroutine model and testability. Holding while the final condition matches makes the pin's release follow state, exactly like condition rules, with cooldown as the minimum hold.
- *Alternative:* hold for a fixed TTL field — rejected: another field and semantics to explain; cooldown + final-condition level already give a predictable release.
- *Alternative:* abort the run the moment any step goes false — rejected: one-tick sensor blips would kill long waits; the window deadline already prevents stale runs.
- *Alternative:* edge-only firing with an instant release — rejected: a transient final condition would produce a blink, and `cooldown_seconds = 0` would make it useless.
- *Note:* for patterns that mean "still true after N", operators should use a sustained condition (`for_seconds`); the UI copy says so, because a sequence only samples the final condition at advance time and then holds on its level.

### D6 — Evaluation-engine integration: shared pure helper, trigger signature on reload, no new loops

Extract the per-rule tick body into a pure helper (for example `advanceTrigger(rs *ruleState, now time.Time, state map[string]any) (fire, release bool)`) used by both `runEvaluator` and `EvaluateRulesOnce`, so tests and production share one transition path. `ruleState` gains a `triggerSig` string (hash of `condition`, `for_seconds`, `trigger_kind`, `sequence`); the existing 30 s `loadRules` closure compares the signature on reload and, when it changed, resets `conditionSince`/sequence/`stepSince`/`armedAt` but keeps `pinned` and `cooldownUntil` (an edit mid-hold must not strobe the wall). The 1 s ticker, per-rule jittered `nextCheck`, interval floor, target resolution, once-per-start non-capable log, fetch-failure handling, and panic-restart loop are untouched.

- *Why:* one transition function is the only way to keep `runEvaluator` and the exported test seam honest; signature-based reset avoids surprising pin drops on unrelated edits.
- *Alternative:* reset all timer state on every reload — rejected: the reload fires every 30 s regardless of edits, so sustained holds and sequences would never complete when `for_seconds`/`window_seconds` exceed 30 s.

### D7 — No new precedence or pin contract

`pinAll`/`unpinAll` remain the only pin path; precedence `notifications > pinned rule > rotation` is decided in the feed controller and is untouched. Manual `Next()` clears `PinnedKey` and `rs.pinned` stays true, so a skipped rule does not re-pin until its trigger falls and rises again (condition mode) or a fresh sequence run completes (sequence mode). Cooldown semantics are byte-for-byte today's: hold-refresh while true, suppress unpin inside the window, and suppress re-fire after release.

- *Why:* the scope requires these preserved, and restating them as the new capability's contract keeps future changes honest.
- *Alternative:* treat a sequence completion as a new precedence tier — rejected: no requirement, and it would complicate the feed controller for nothing.

### D8 — Admin UI: one JSON contract, a recursive builder, and explicit units

`eventrule_form.html` gains a trigger-mode selector (`condition` | `sequence`). Condition mode renders a recursive builder: group blocks with an `and`/`or` select, add/remove condition rows and nested groups (client-side depth cap mirrors the server's 4), and leaf rows with path/operator/value. A "hold for" number + unit select (seconds/minutes) writes `for_seconds` in seconds. Sequence mode renders a window field (seconds/minutes), 2-4 ordered step cards each with its own condition builder and hold, and add/remove-step controls. On submit, JS serializes the builder into the hidden `condition`, `for_seconds`, `sequence`, and `trigger_kind` fields exactly matching the server JSON. Server validation failures re-render the form with the submitted JSON and a clear message; the builder is reconstructed from that JSON on load, so an error never loses the tree. `eventrules.html` renders a readable summary (for example `cpu gt 80 and (temp lt 20 or wind gt 30)` and `door eq open → door eq open within 15m`), with the legacy single-condition summary preserved.

- *Why:* the JSON is the single source of truth across storage, server validation, simulate, and the builder; units live only in the UI, keeping storage canonical in seconds.
- *Alternative:* server-rendered nested form fields (`conditions[0][path]…`) — rejected: unbounded indexing, tedious parsing, and error replay is harder than carrying one JSON blob.
- *Alternative:* a third-party query-builder dependency — rejected: the project is dependency-light and the required tree is small.

### D9 — Extend `APIEventRuleSimulate` with per-node/per-step results, still read-only

The simulate endpoint keeps returning `matched`/`would`/`state`/`then` and adds the trigger shape (`trigger_kind`, `for_seconds`, `sequence`) plus a flattened `nodes` list of `{path, operator, matched}` (condition mode), or `steps` of `{index, matched, for_seconds}` (sequence mode). It evaluates against a single live snapshot and never pins, unpins, or executes actions — exactly today's contract. Because it has no history, it reports instantaneous matches only and does not claim a sustained or sequence fire verdict.

- *Why:* the simulator is the operator's main debugging tool; leaving it blind to groups or sequences would strand the new feature.
- *Alternative:* replay the last N minutes of state — rejected: no state history exists, and adding a ring buffer is a separate change.

### D10 — Additive schema, no data migration

Three additive `DisplayRule` columns: `for_seconds` (int, default 0, min 0), `trigger_kind` (string, default `condition`), `sequence` (text, default `""`). Ent auto-migration adds them with defaults, so every existing row reads as a legacy immediate condition with no sequence, and the default `trigger_kind` string is exactly the legacy behavior. Rollback is safe: an older binary ignores the new columns and continues evaluating `condition` as before. No data backfill, no denormalization.

- *Why:* additive columns with defaults are the project's established migration pattern and require no backfill.
- *Alternative:* version the rule row or move storage to a `trigger` JSON blob — rejected: a destructive migration for no functional gain.

## Risks / Trade-offs

- [Transient fetch failure resets a sustained hold] → Failure must keep evaluating as false per the existing contract; it is logged once per outage window (`logStateFetchOnce`), the pin release still follows cooldown, and the hold restarts on recovery.
- [Operators read a sequence as "remains true"] → The UI recommends `for_seconds` for "still true after N" patterns and shows a worker example; design D5 documents the exact semantics.
- [Sequence repeats while step 0 stays true] → Runs re-arm only from idle; window + cooldown bound the re-fire rate, and `cooldown_seconds` is the explicit knob.
- [Sequence window/holds cannot be satisfied] → Save-time validation rejects `window_seconds < Σ(step holds after the first) + check interval`, so a sequence that can never fire is never stored.
- [Builder breaks legacy rule editing] → Legacy single-condition rows are exactly D1 leaves; the builder round-trip is an explicit test ("load legacy → submit → JSON semantically identical").
- [Deep or wide trees cost CPU] → Parse-time bounds (4 levels, 32 leaves) plus short-circuit evaluation over an in-memory map; a rule larger than the bound is rejected at save and skipped safely if hand-edited into the DB.
- [Reload resets in-flight timers] → Only when `triggerSig` changed; unrelated edits (name, then-action, enabled) keep timers, and pins are never dropped by reload.
- [Hand-edited invalid trigger JSON] → The tick logs once per rule and skips, the rule stays enabled, and the evaluator never panics — same pattern as non-capable targets today.
- [Cooldown default 0 with a transient final sequence step] → Release is immediate once the final condition drops; the UI hints that sequence rules usually want a non-zero cooldown, and D5 documents it.

## Migration Plan

1. Add `for_seconds`, `trigger_kind`, and `sequence` to `ent/schema/displayrule.go` and run `go generate ./ent`; auto-migration is additive with defaults.
2. Add `ConditionNode`/`SequenceTrigger` parsing and evaluation to `datasource/eventrule.go` with bounds; keep `ParseCondition`/`Evaluate` unchanged.
3. Add the trigger-signature reset and the pure `advanceTrigger` helper to `handlers/eventrules.go`; wire condition and sequence modes into `runEvaluator`/`EvaluateRulesOnce`.
4. Extend `validateEventRule` and the create/update handlers with the new fields and messages; extend error replay to carry the builder JSON.
5. Rebuild the form template's condition section (mode selector, recursive builder, units, step editor) and extend the list summary and `APIEventRuleSimulate`.
6. Update the README "Event-Driven Switching" section, run `go build ./... && go test ./...`, then `task pre-push`.
7. Rollback: revert the server binary; existing rows keep evaluating their `condition` leaf, and the additive columns are simply ignored.

## Open Questions

- Should the hold duration be allowed per leaf as well as per rule? Assumed rule-level for v1; the leaf JSON can gain an optional `for` later without breaking stored data.
- Should sequences support a per-step `after_seconds` in addition to `for_seconds`? Assumed not for v1 — holds plus the window cover the stated cases.
- Should the sequence hold-after-fire be configurable (until-final-condition vs a fixed TTL)? Assumed until-final-condition plus cooldown; a TTL could be added as a sequence option later.
- Should the UI expose minutes-only for long holds? Assumed a seconds/minutes unit select storing seconds, so 90-minute holds remain expressible.
- Should simulate replay recent ticks for sustained/sequence verdicts? Assumed no — it stays a stateless snapshot, and the per-node/per-step results are the debugging aid.
