## 1. Composed condition model

- [ ] 1.1 Add `ConditionNode` (leaf fields `path`/`operator`/`value`, group fields `op`/`conditions`) plus `ParseConditionNode` and `EvaluateNode` to `datasource/eventrule.go`; keep `ParseCondition`/`Evaluate` unchanged for existing callers and delegate leaf evaluation to the existing `Evaluate`
- [ ] 1.2 Enforce composition bounds at parse time with human-readable errors: `op` only `and`/`or`, groups need at least 2 children, groups must not mix leaf fields, maximum 4 nested group levels, maximum 32 leaves, and the existing operator whitelist for leaves
- [ ] 1.3 Add table-driven unit tests in `datasource/` (extend `eventrule` tests): legacy-leaf parity, AND/OR truth tables, nesting, short-circuit, safe-false on unresolvable paths, `exists` on missing/present keys, depth/leaf/single-child/malformed rejects

## 2. Sustained conditions

- [ ] 2.1 Add `for_seconds` to `ent/schema/displayrule.go` (int, default 0, min 0), run `go generate ./ent`, and verify the additive migration against an existing database file
- [ ] 2.2 Add `conditionSince` to `ruleState` in `handlers/eventrules.go` and extract the per-rule tick transition into one pure helper (e.g. `advanceTrigger`) used by both `runEvaluator` and `EvaluateRulesOnce`; fire only when the composed condition has held for `for_seconds` and reset the hold on a false or failed check
- [ ] 2.3 Extend `validateEventRule` and the create/update handlers to parse and validate `for_seconds` (0, or `max(5, check_interval_seconds)`..86400) with clear messages, and include the value in error re-renders
- [ ] 2.4 Add unit tests: pin only after the full hold across ticks; a false tick resets the hold; a fetch failure resets the hold and release follows cooldown; `for_seconds` 0 keeps legacy immediate firing; short-hold and over-range saves are rejected without persisting

## 3. Sequence triggers

- [ ] 3.1 Add `trigger_kind` (string, default `condition`) and `sequence` (text, default `""`) to `ent/schema/displayrule.go`, run `go generate ./ent`, and confirm existing rows read as `condition` mode with an empty sequence
- [ ] 3.2 Add `SequenceStep`/`SequenceTrigger` types with `ParseSequence` and validation in `datasource/eventrule.go`: 2-4 steps, each a valid composed condition with `for_seconds` 0..86400 (and 0 or ≥ the effective check interval), `window_seconds` 5..86400, and `window_seconds >= sum(post-first step holds) + effective check interval`
- [ ] 3.3 Implement the sequence state machine in the shared tick helper with `ruleState` fields `seqStep`, `stepSince`, and `armedAt`: arm when step one holds, advance as each step holds its duration, fire exactly once when the final step completes inside the window, reset without firing on window expiry, hold the pin while the final condition still matches, and release under `cooldown_seconds`
- [ ] 3.4 Extend `validateEventRule` and the create/update handlers for `trigger_kind`/`sequence`, including mode mutual exclusivity (no rule-level `for_seconds` in sequence mode; no sequence in condition mode) and window-fit rejection, with clear error messages
- [ ] 3.5 Add unit tests: A→B fires once and runs the then-action; late B resets without firing; B matching without A does not fire; per-step hold respected; step blip restarts only that hold; pin holds/releases with the final condition; cooldown suppresses a fresh run; manual skip clears the pin until a fresh run; invalid step count/window/exclusivity rejected

## 4. Evaluator integration and preserved semantics

- [ ] 4.1 Add a `triggerSig` (hash of `condition`, `for_seconds`, `trigger_kind`, `sequence`) to `ruleState`; on the 30 s `loadRules` reload reset timers only when the signature changed, keep `pinned` and `cooldownUntil`, and keep pruning disabled rules
- [ ] 4.2 Verify and test that composition leaves an existing contract untouched: one `CurrentState` fetch per rule per check, non-capable/unresolvable targets skipped with the once-per-start log, fetch failure evaluates as empty state, `state_path` scoping applied once, interval floor 5 s with ±10 % jitter, and panic-restart behavior
- [ ] 4.3 Add feed integration coverage (in the style of the existing `main_test.go` pin tests): a notification during a composed or sequence pin returns to the pinned source; manual skip releases until a fresh fire; then-actions execute once per rising edge or run completion
- [ ] 4.4 Confirm the existing event-rule test suite passes unchanged and add an explicit legacy-rule parity test (single condition, defaults, identical pin/cooldown/then behavior)

## 5. Admin UI

- [ ] 5.1 Rebuild the condition section of `web/templates/admin/eventrule_form.html`: trigger-mode selector, recursive AND/OR builder with add/remove for leaves and nested groups (client-side depth cap), hold-duration number plus seconds/minutes unit select, and a sequence editor with window field and 2-4 ordered step cards; serialize the builder into hidden `condition`, `trigger_kind`, `for_seconds`, and `sequence` fields
- [ ] 5.2 Plumb the new fields through `eventRuleFormVars` and `AdminEventRuleNew/Edit/Create/Update` (`handlers/eventrules.go`) and make error re-renders preserve the submitted builder JSON and trigger mode
- [ ] 5.3 Extend the `web/templates/admin/eventrules.html` summary script to render grouped and sequence triggers readably (e.g. `cpu gt 80 and (temp lt 20 or wind gt 30)`, `door eq open → door eq open within 15m`) while keeping the legacy single-condition summary
- [ ] 5.4 Add HTTP/handler tests via the existing server harness: create a grouped rule and a sequence rule through the form; invalid depth and out-of-range step count re-render 200 with the input preserved and no row persisted; minutes→seconds round-trip on edit; legacy rule edit round-trip is semantically identical; the list contains a readable summary

## 6. Simulation

- [ ] 6.1 Extend `APIEventRuleSimulate` (`handlers/eventrules.go`) to return the trigger shape plus per-leaf match results for compositions and per-step match results for sequences, staying read-only (no pin/unpin, no then-action) and not claiming history-dependent fire verdicts
- [ ] 6.2 Add tests: grouped simulation lists every leaf with its match result; sequence simulation lists steps and hold settings; a matching simulation changes no pin state and executes no then-action

## 7. Docs and verification

- [ ] 7.1 Update the README "Event-Driven Switching" section with composed AND/OR conditions, sustained `for_seconds`, sequence windows/steps, validation rules, and the unchanged precedence/pin/cooldown contracts
- [ ] 7.2 Run `go build ./... && go test ./...` and fix failures
- [ ] 7.3 Run `task pre-push` (gofmt, tests, build) and `openspec validate add-rule-composition --strict`; fix anything that fails
