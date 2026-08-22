## ADDED Requirements

### Requirement: Display rule storage and management
The system SHALL provide a `DisplayRule` entity with `name`, `enabled` (default true), a target source reference (`source_type`/`source_id` in the established reference shape), a `condition` JSON blob (`{path, operator, value}`), `check_interval_seconds` (default 30, minimum 5), and `cooldown_seconds` (default 0). Admins SHALL be able to create, edit, list, and delete rules via session-authenticated admin routes.

#### Scenario: Create a playback rule
- **WHEN** an admin creates a rule "Jellyfin playing" targeting `genericapi:2` with condition `{"path":"player.isPlaying","operator":"eq","value":true}` and interval 15
- **THEN** the rule is persisted and becomes active without a server restart

#### Scenario: Interval floor enforced
- **WHEN** an admin submits `check_interval_seconds` below 5
- **THEN** the save is rejected with a validation error

### Requirement: State evaluation via the StateProvider capability
Datasources MAY implement `StateProvider.CurrentState(ctx) (map[string]any, error)`. The evaluator SHALL evaluate rules only against targets implementing this capability; rules targeting non-capable sources SHALL be skipped with a log entry emitted once per evaluator start, not per check. GenericAPI, HomeAssistant, and SystemStats SHALL implement it at minimum.

#### Scenario: Rule targeting a non-capable source
- **WHEN** an enabled rule targets a datasource without `CurrentState`
- **THEN** the rule never pins, one warning names it, and the feed rotation is unaffected

#### Scenario: Upstream state fetch fails
- **WHEN** `CurrentState` returns an error for a rule's target
- **THEN** that check is treated as condition-false, the error is logged, and previously pinned state follows normal cooldown/release semantics

### Requirement: Condition evaluation semantics
Conditions SHALL resolve `path` using the shared dot-path helper (`extractDotPath` semantics). Operators `eq`, `ne`, `gt`, `lt`, `ge`, `le`, `contains`, `exists` SHALL be supported: numeric comparison when both sides parse as numbers, otherwise string equality; `contains` applies to strings and arrays; an unresolvable path SHALL satisfy no operator except `exists`.

#### Scenario: Numeric threshold
- **WHEN** state reports `{"battery": 12}` and the condition is `{"path":"battery","operator":"lt","value":20}`
- **THEN** the condition matches

#### Scenario: Unresolvable path is safe-false
- **WHEN** state lacks the configured path and the operator is `eq`
- **THEN** the condition does not match

#### Scenario: Exists on missing key
- **WHEN** state lacks the configured path and the operator is `exists` with value `false`
- **THEN** the condition matches

### Requirement: Pin lifecycle in the feed controller
When a rule's condition matches, the evaluator SHALL pin its target source on all active feed controllers; `serveFeed` SHALL render and hold the pinned source at slot boundaries instead of advancing the rotation. When the condition clears, the pin SHALL release and rotation resumes from where it left off. A manual skip/next SHALL clear the current pin. Pinned slots SHALL flow through the normal render path (last-known-good cache, health recording, stale marking, frame counting) with no special-casing.

#### Scenario: Condition holds the wall
- **WHEN** the rule condition is true and the rotation reaches a slot boundary
- **THEN** every active feed renders the target source and continues holding it across successive boundaries until release

#### Scenario: Release resumes rotation
- **WHEN** the condition turns false after cooldown
- **THEN** feeds resume cycling their prior source lists from the next slot

#### Scenario: Manual skip overrides a pin
- **WHEN** an operator sends next/skip while a rule holds the wall
- **THEN** the pin clears immediately and rotation advances

#### Scenario: Pinned source render failure
- **WHEN** the pinned source fails to render during a held slot
- **THEN** the feed serves its last-known-good frame marked stale, or retries next tick if none exists — never a blank wall

### Requirement: Precedence ordering
Display precedence SHALL be: notification broadcasts > pinned event rule > scheduled rotation. Notifications SHALL interrupt a pinned slot and the pin SHALL persist across the interruption. Manual pause SHALL still pause the feed regardless of pin state. Future push-to-display TTL overrides SHALL sit above rule pins.

#### Scenario: Notification during a pin
- **WHEN** a priority notification arrives while a rule holds the wall
- **THEN** the notification displays first, then the wall returns to the pinned source while the condition remains true

#### Scenario: Pause beats pin
- **WHEN** an operator pauses a feed that a rule has pinned
- **THEN** the feed stays paused until resumed

### Requirement: Evaluator reliability
The evaluator SHALL run as a single goroutine scheduling checks per rule with ±10% jitter, reloading rules from the database at most every 30 seconds, and recovering from panics via the server run loop. Evaluator failures SHALL never block or blank any feed. Cooldown SHALL suppress re-pinning for `cooldown_seconds` after release and hold a pin for at least `cooldown_seconds` after firing.

#### Scenario: Flapping condition damped
- **WHEN** a condition alternates true/false faster than `cooldown_seconds`
- **THEN** the pin holds steady through the flicker instead of strobing

#### Scenario: Evaluator panic
- **WHEN** the evaluator goroutine panics
- **THEN** it is restarted, pins reset to unpinned, and rotation was never interrupted
