# Spec: rule-composition

## ADDED Requirements

### Requirement: Composed condition trees

The system SHALL accept a `DisplayRule` condition as either a legacy single leaf (`{"path": "...", "operator": "...", "value": ...}`) or a recursive composition of leaves and groups of the form `{"op": "and"|"or", "conditions": [...]}`. A group SHALL contain at least two children, a composition SHALL be limited to at most 4 nested group levels and at most 32 leaf conditions, and every leaf SHALL use one of the existing operators (`eq`, `ne`, `gt`, `lt`, `ge`, `le`, `contains`, `exists`). Legacy single-condition JSON SHALL remain valid and SHALL be treated as a one-leaf composition.

#### Scenario: Legacy condition remains valid
- **WHEN** a rule stores the condition `{"path":"cpu","operator":"gt","value":80}`
- **THEN** parsing SHALL succeed unchanged and the rule SHALL evaluate exactly as before this change

#### Scenario: AND group accepted
- **WHEN** an administrator saves the condition `{"op":"and","conditions":[{"path":"temp","operator":"gt","value":25},{"path":"humidity","operator":"gt","value":60}]}`
- **THEN** the system SHALL persist it and the rule SHALL match only when both leaves match

#### Scenario: OR group accepted
- **WHEN** an administrator saves a condition whose root group has `"op":"or"`
- **THEN** the system SHALL persist it and the rule SHALL match when any child matches

#### Scenario: Nested groups accepted
- **WHEN** an administrator saves a condition that nests an `or` group inside an `and` group within the depth limit
- **THEN** the system SHALL persist it and evaluate the nesting as written

#### Scenario: Excessive depth rejected
- **WHEN** an administrator submits a condition nesting five or more group levels
- **THEN** the system SHALL reject the save with a message naming the depth limit and SHALL NOT persist the rule

#### Scenario: Excessive leaf count rejected
- **WHEN** an administrator submits a composition with more than 32 leaf conditions
- **THEN** the system SHALL reject the save with a message naming the leaf limit and SHALL NOT persist the rule

#### Scenario: Degenerate group rejected
- **WHEN** an administrator submits a group with fewer than two children or a group missing the `op` field
- **THEN** the system SHALL reject the save with a validation message and SHALL NOT persist the rule

#### Scenario: Invalid leaf operator rejected
- **WHEN** any leaf in a composition uses an operator outside the supported set
- **THEN** the system SHALL reject the save with a message naming the invalid operator

### Requirement: Composed condition evaluation semantics

The system SHALL evaluate a composition as a boolean tree over one fetched state map: an `and` group SHALL match when every child matches, an `or` group SHALL match when any child matches, and evaluation SHALL short-circuit. Leaf conditions SHALL resolve their `path` with the shared dot-path helper (`datasource.DotPath`) and SHALL use the existing numeric, string, boolean, `contains`, and `exists` semantics; an unresolvable path SHALL satisfy no operator except `exists`. A failed `CurrentState` fetch SHALL evaluate the composition as false for that check and SHALL preserve the existing failure semantics, including cooldown-governed release.

#### Scenario: AND requires every child
- **WHEN** an `and` group has one matching and one non-matching leaf
- **THEN** the group SHALL NOT match

#### Scenario: OR requires one child
- **WHEN** an `or` group has one matching leaf and one non-matching leaf
- **THEN** the group SHALL match

#### Scenario: Nested evaluation
- **WHEN** the state map makes the inner group true and the outer `and` group's other child false
- **THEN** the root composition SHALL NOT match

#### Scenario: Unresolvable leaf is safe-false
- **WHEN** a leaf references a path absent from the state map with operator `eq`, `ne`, `gt`, `lt`, `ge`, `le`, or `contains`
- **THEN** that leaf SHALL NOT match

#### Scenario: Exists on a missing key
- **WHEN** a leaf uses operator `exists` against a path absent from the state map
- **THEN** the leaf SHALL NOT match, and it SHALL match when the path is present

#### Scenario: Fetch failure evaluates false
- **WHEN** `CurrentState` returns an error for the rule's target on a check
- **THEN** the whole composition SHALL evaluate as false for that check and the failure SHALL be logged once per outage window

### Requirement: Sustained conditions

The system SHALL support a `for_seconds` field on a rule (default 0, range 0-86400) that requires the composed condition to hold true on every evaluation for at least that duration before the rule fires. A check that evaluates false — including a check whose state fetch failed — SHALL reset the hold. A value of 0 SHALL preserve immediate firing. `for_seconds` SHALL be validated as either 0 or at least the effective check interval (`max(5, check_interval_seconds)`), so a hold shorter than the observation cadence cannot be configured.

#### Scenario: Fires only after the hold elapses
- **WHEN** a rule with `for_seconds` 60 and check interval 5 s evaluates true on every check
- **THEN** the rule SHALL NOT pin before 60 s of continuous true checks and SHALL pin once the hold is satisfied

#### Scenario: A false check resets the hold
- **WHEN** a sustained condition turns false after 50 s and true again
- **THEN** the hold SHALL restart and the rule SHALL fire only after another full `for_seconds` of continuous true checks

#### Scenario: Failed check resets the hold
- **WHEN** a state fetch fails mid-hold
- **THEN** the hold SHALL reset and a previously pinned rule SHALL be released subject to the existing cooldown

#### Scenario: Zero hold preserves immediate firing
- **WHEN** `for_seconds` is 0 and the condition evaluates true
- **THEN** the rule SHALL fire on that first true check exactly as before this change

#### Scenario: Hold shorter than the cadence rejected
- **WHEN** an administrator saves `for_seconds` 3 with a check interval of 30 s
- **THEN** the system SHALL reject the save with a message explaining the hold must be at least the check interval

#### Scenario: Cooldown hysteresis unchanged
- **WHEN** a sustained rule has fired and its condition turns false while `cooldown_seconds` is still active
- **THEN** the pin SHALL hold until the cooldown elapses, and no re-fire SHALL occur inside the cooldown after release

### Requirement: Sequence triggers

The system SHALL support sequence-triggered rules via `trigger_kind` set to `sequence` and a `sequence` JSON object `{"window_seconds": ..., "steps": [{"condition": ..., "for_seconds": ...}, ...]}` with 2 to 4 ordered steps. A run SHALL arm only when the first step's condition holds for its own duration; each subsequent step SHALL be evaluated after the previous step completes and SHALL complete when its condition holds for its own duration; the rule SHALL fire exactly once when the final step completes within `window_seconds` measured from the first step's completion. If the window elapses first, the run SHALL reset to idle without firing. A step whose condition stops matching SHALL reset only that step's hold and SHALL NOT abort the run before the window. Sequence mode SHALL be mutually exclusive with the rule-level `for_seconds` field, and `window_seconds` SHALL be validated to be at least the sum of the hold durations of the steps after the first plus the effective check interval.

#### Scenario: A then B fires once
- **WHEN** step A holds, step B completes within the window, and the window has not elapsed
- **THEN** the rule SHALL pin its target and execute its then-action exactly once for that run

#### Scenario: B too late resets without firing
- **WHEN** step A holds but the final step does not complete before `window_seconds` elapses
- **THEN** the run SHALL reset to idle, the rule SHALL NOT pin, and no then-action SHALL execute

#### Scenario: B without A does not fire
- **WHEN** a later step's condition matches while the run is idle and the first step has never held
- **THEN** the rule SHALL NOT fire

#### Scenario: Step hold duration respected
- **WHEN** the final step's `for_seconds` is 600 and its condition holds
- **THEN** the rule SHALL fire only after 600 s of continuous match for that step

#### Scenario: Step blip restarts only that step's hold
- **WHEN** the final step's condition drops and recovers while the window is still open
- **THEN** the step's hold SHALL restart from the recovery and the run SHALL fire if the final step completes before the window elapses

#### Scenario: Step count bounded
- **WHEN** an administrator submits a sequence with one step or with five steps
- **THEN** the system SHALL reject the save with a message naming the 2-4 step limit

#### Scenario: Impossible window rejected
- **WHEN** an administrator submits `window_seconds` smaller than the sum of the post-first step holds plus the check interval
- **THEN** the system SHALL reject the save with a message explaining the window cannot fit the steps

#### Scenario: Trigger modes mutually exclusive
- **WHEN** a rule is submitted with `trigger_kind` `sequence` and a non-zero rule-level `for_seconds`, or with `trigger_kind` `condition` and a non-empty `sequence`
- **THEN** the system SHALL reject the save with a validation message

#### Scenario: Legacy rules default to condition mode
- **WHEN** a stored rule has no `trigger_kind` value
- **THEN** the system SHALL treat it as `condition` mode with an empty sequence and unchanged behavior

### Requirement: Sequence hold and release

After a sequence fires, the pin SHALL remain held while the final step's condition continues to match, and SHALL release when it stops matching subject to the existing `cooldown_seconds` minimum hold. The cooldown SHALL also suppress a fresh run from firing after release. A manual skip SHALL clear the pin immediately and the rule SHALL NOT re-pin until a fresh run completes.

#### Scenario: Pin holds while the final condition matches
- **WHEN** a sequence has fired and the final step's condition continues to evaluate true on later checks
- **THEN** the pin SHALL remain held

#### Scenario: Pin releases when the final condition drops
- **WHEN** the final step's condition turns false and the cooldown has elapsed
- **THEN** the pin SHALL release and the rule SHALL reset to idle

#### Scenario: Cooldown suppresses a fresh run
- **WHEN** a sequence has fired and released inside `cooldown_seconds`
- **THEN** a fresh run SHALL NOT fire until the cooldown elapses

#### Scenario: Manual skip releases until a fresh run
- **WHEN** an operator skips the wall while a sequence holds the pin
- **THEN** the pin SHALL clear immediately and the rule SHALL NOT re-pin until a fresh run completes

### Requirement: Existing rule semantics preserved

For every trigger mode the system SHALL preserve: precedence `notifications > pinned rule > rotation`; manual skip releasing a pin until the trigger re-fires; cooldown suppression on both edges (minimum hold after firing and no re-fire after release); once-per-start logging and safe skipping of targets that do not implement `StateProvider` or cannot be resolved; fetch failure evaluating as empty state; the 1 s evaluation tick with a 5 s minimum interval and ±10 % jitter; the 30 s rule reload; `state_path` scoping of the fetched state map; and then-actions executing only on a rising edge (condition mode) or once per completed sequence run. Previously created rules SHALL evaluate identically unless their trigger is edited.

#### Scenario: Legacy rule parity
- **WHEN** a rule created before this change has a single condition, `for_seconds` 0, and condition-mode defaults
- **THEN** its pin, cooldown, and then-action behavior SHALL be identical after this change

#### Scenario: Notification precedes a pinned rule
- **WHEN** a priority notification arrives while a composed or sequence rule holds the wall
- **THEN** the notification SHALL display first and the wall SHALL return to the pinned source afterward while the trigger still holds

#### Scenario: Non-capable target skipped safely
- **WHEN** a composed or sequence rule targets a source without `StateProvider` or an unresolvable reference
- **THEN** the rule SHALL never pin, SHALL log at most once per start, and SHALL remain enabled without affecting rotation

#### Scenario: Reload applies edits
- **WHEN** a rule's trigger, interval, or enabled flag is edited in the database
- **THEN** the next 30 s reload SHALL apply the edit, prune disabled rules, and reset only the edited rule's trigger timers without dropping its pin

#### Scenario: Then-action edge-triggered
- **WHEN** a condition rule stays true across many checks, or a sequence run is held after firing
- **THEN** the then-action SHALL execute exactly once (on the rising edge or run completion) and SHALL NOT repeat on subsequent true checks

### Requirement: Admin condition builder and validation

The event rule form SHALL provide a trigger-mode selector, a recursive AND/OR condition builder with controls to add and remove leaves and nested groups, a sustained duration input that accepts seconds or minutes and stores seconds, and a sequence editor with a window field and 2-4 ordered step cards. On submit the browser SHALL serialize the builder into the same canonical `condition`, `trigger_kind`, `for_seconds`, and `sequence` fields the server stores. Invalid triggers SHALL be rejected with a clear message, SHALL NOT persist, and SHALL re-render the form preserving the submitted builder content. The rules list SHALL render a readable summary of grouped and sequence triggers. Editing a legacy rule SHALL present it in the builder and round-trip without semantic change.

#### Scenario: Build a grouped rule in the form
- **WHEN** an administrator assembles an `and` group of two leaf conditions in the builder and submits the form
- **THEN** the server SHALL persist the canonical condition JSON and the rule SHALL become active without a restart

#### Scenario: Invalid composition preserves input
- **WHEN** the builder submits a composition that exceeds the depth limit
- **THEN** the server SHALL re-render the form with a clear error and the submitted builder content intact

#### Scenario: Duration units convert to seconds
- **WHEN** an administrator enters a 10-minute hold and submits
- **THEN** the system SHALL store `for_seconds` 600 and the form SHALL show the value as 10 minutes on edit

#### Scenario: Legacy rule round-trips in the builder
- **WHEN** an administrator opens a legacy single-condition rule and saves it without changes
- **THEN** the stored condition JSON SHALL remain semantically identical and enforcement SHALL be unchanged

#### Scenario: Sequence editor bounded
- **WHEN** an administrator opens the sequence editor
- **THEN** the builder SHALL allow adding between 2 and 4 steps, SHALL show the window and per-step hold fields, and SHALL block submission with a visible message outside those bounds

#### Scenario: List summary is readable
- **WHEN** an administrator opens the event rules list containing a grouped and a sequence rule
- **THEN** each row SHALL show a human-readable summary such as `cpu gt 80 and (temp lt 20 or wind gt 30)` or `door eq open → door eq open within 15m`

### Requirement: Single-loop evaluation with shared resolution

Auto-evaluation SHALL reuse the existing evaluator: one goroutine on the 1 s tick, one `CurrentState` fetch per rule per scheduled check, one application of the existing state-path scoping, and the shared `datasource.DotPath` resolver for every leaf. Composed conditions, sustained holds, and sequences SHALL NOT introduce per-condition fetches, per-rule goroutines, or additional polling loops, and the 30 s database reload SHALL remain the only configuration refresh mechanism.

#### Scenario: One fetch per composed rule check
- **WHEN** a rule's composed condition contains multiple leaves against one target
- **THEN** the evaluator SHALL call `CurrentState` once for that rule's check, never once per leaf

#### Scenario: Resolution reuses the shared helper
- **WHEN** any leaf path is evaluated
- **THEN** the system SHALL resolve it through the existing dot-path helper used by single-condition rules

#### Scenario: Trigger timers advance only on evaluator ticks
- **WHEN** no evaluator tick has occurred
- **THEN** no sustained or sequence timer SHALL advance, and no background loop SHALL exist per rule

### Requirement: Simulation of composed triggers

The simulate endpoint SHALL evaluate the stored trigger against a single live state snapshot without pinning, unpinning, or executing any action, and SHALL return the trigger shape plus per-leaf match results for compositions and per-step match results for sequences. For sustained and sequence triggers it SHALL report instantaneous matches only and SHALL NOT claim a fire verdict that depends on history it has not observed.

#### Scenario: Grouped simulation reports leaves
- **WHEN** an administrator simulates a rule with an `and`/`or` composition
- **THEN** the response SHALL include each leaf's path, operator, and current match result

#### Scenario: Sequence simulation reports steps
- **WHEN** an administrator simulates a sequence rule
- **THEN** the response SHALL include each step's current match result and hold setting, and SHALL NOT assert that the sequence would fire

#### Scenario: Simulation is read-only
- **WHEN** a simulation runs against a matching condition
- **THEN** no pin state SHALL change and no then-action SHALL execute
