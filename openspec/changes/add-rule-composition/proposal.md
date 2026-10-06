## Why

Event rules (`handlers/eventrules.go`) watch exactly one dot-path on one state-capable source with a single operator (`datasource.Condition` in `datasource/eventrule.go:12`) and pin the wall while that one comparison is true. That shape cannot express the triggers operators actually ask for — "hot AND humid", "garage door open for 10 minutes", "motion, then the front door opens within 2 minutes". Authors fake compounds with separate rules that fight over the same pin, and there is no way to require persistence or an ordered sequence at all. The engine already fetches one state map per rule per check and resolves paths with `datasource.DotPath` (`datasource/dotpath.go:12`), so composition can be added without a new scheduler, new goroutines, or per-rule polling loops.

## What Changes

- **Composed conditions (AND/OR)**: extend the `DisplayRule.condition` JSON (`ent/schema/displayrule.go:20`) from a single leaf `{path, operator, value}` to a recursive tree of leaves and `{op: "and"|"or", conditions: [...]}` groups, bounded to at most 4 nested group levels and 32 leaves. Legacy single-condition JSON stays byte-compatible and evaluates identically; every leaf still resolves through `datasource.DotPath`.
- **Sustained conditions ("for N seconds/minutes")**: new `for_seconds` rule field (default 0). The composed condition must hold on every evaluation for that duration before the rule fires/pins; a false or failed tick resets the hold; 0 preserves today's immediate behavior. Hysteresis remains exactly the existing `cooldown_seconds` both-edges contract — no new hold field.
- **Sequence triggers**: new `trigger_kind` (`condition` default | `sequence`) and `sequence` JSON field (`{window_seconds, steps:[{condition, for_seconds}]}`, 2-4 ordered steps). A run arms when step one holds for its duration, advances as each next step holds for its own duration, and fires once when the final step completes within the window; if the window elapses first the run resets without firing. After firing, the pin holds while the final step's condition keeps matching and releases under the existing cooldown.
- **Evaluator integration**: `ruleState` (`handlers/eventrules.go:273`) gains condition-hold and sequence timers; the 30-second reload resets timers only when a rule's trigger config actually changed and keeps pins; `EvaluateRulesOnce` and `runEvaluator` share one pure transition helper. One `CurrentState` fetch per rule per check, the existing 1-second tick with ≥5 s interval and ±10% jitter, and the single evaluator goroutine are unchanged.
- **Preserved contracts**: precedence `notifications > pinned rule > rotation`, manual skip releasing a pin until a fresh fire, cooldown suppression on both edges, state-incapable or unresolvable targets skipped with a once-per-start log, fetch failure evaluating as empty state, the 30-second config reload, and then-actions firing on a rising edge only (or once per completed sequence run).
- **Admin UI & validation**: `web/templates/admin/eventrule_form.html` gains a trigger-mode selector, a recursive AND/OR condition builder, a hold-duration field with seconds/minutes units, and an ordered-step editor for sequences; the server validates trees and sequences with clear messages and re-renders the submitted JSON on error. `web/templates/admin/eventrules.html` renders readable group/sequence summaries, and `APIEventRuleSimulate` reports per-node/per-step matches without pinning or executing actions.

## Capabilities

### New Capabilities
- `rule-composition`: Rule trigger composition — composed AND/OR condition trees, sustained `for_seconds` holds, ordered sequence triggers with windows, evaluation in the existing single-loop engine, preserved pin/cooldown/precedence contracts, server and client validation, and the admin condition builder.

### Modified Capabilities
- None — `event-driven-switching` and `universal-triggers` exist only as unarchived changes under `openspec/changes/`, not as capabilities in `openspec/specs/`, so every preserved contract is specified as an ADDED requirement inside `rule-composition` rather than as a delta. No capability that currently exists in `openspec/specs/` changes behavior.

## Impact

- **New server code**: composed-condition and sequence parsing/evaluation in `datasource/eventrule.go`, plus a pure per-tick trigger-transition helper in `handlers/eventrules.go`.
- **Modified server code**: `ent/schema/displayrule.go` with additive `for_seconds`, `trigger_kind`, and `sequence` columns (+ `go generate ./ent`); `handlers/eventrules.go` (`runEvaluator`, `EvaluateRulesOnce`, `ruleState`, `validateEventRule`, CRUD form plumbing, `APIEventRuleSimulate`); `web/templates/admin/eventrule_form.html`; `web/templates/admin/eventrules.html`.
- **API surface**: admin event-rule create/edit forms gain `trigger_kind`, `for_seconds`, and `sequence` fields; `POST /api/eventrules/:id/simulate` gains per-node/per-step results. No device protocol or WebSocket changes.
- **Tests**: `datasource/eventrule` unit tests; `handlers/eventrules*_test.go` evaluator, validation, reload, sustained, and sequence regressions; feed integration coverage for precedence and manual skip with composed rules.
- **Dependencies**: none new.
- **Risk**: a transient upstream fetch failure resets a sustained hold (because fetch failure must keep evaluating as false), and operators may read a sequence as "remains true" instead of "A then B" — mitigated by the unchanged cooldown contract, UI copy recommending `for_seconds` for "still true" patterns, bounded tree/step sizes, and validation that rejects sequences whose window cannot fit their holds.
