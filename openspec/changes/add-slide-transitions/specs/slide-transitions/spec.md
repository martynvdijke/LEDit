## ADDED Requirements

### Requirement: Transition settings
GeneralSettings SHALL gain `transition_style` (`none` | `fade` | `wipe` | `dissolve`, default `none`) and `transition_ms` (default 500, validated 100–2000), exposed in the existing settings admin form. The default SHALL preserve today's behavior exactly.

#### Scenario: Default is inert
- **WHEN** the server runs with unset transition settings
- **THEN** slot boundaries behave identically to before this change (single message per slot)

#### Scenario: Invalid settings rejected
- **WHEN** an admin saves an unknown style or a duration outside 100–2000 ms
- **THEN** the save is rejected with a validation error

### Requirement: Transition styles composite deterministically
The system SHALL provide fade (per-pixel lerp), wipe (column sweep), and dissolve (deterministic pseudo-random pixel reveal) as pure progress-parameterized blend functions over decoded frames. Identical inputs and progress SHALL always produce identical output frames.

#### Scenario: Fade endpoints are exact
- **WHEN** fade is evaluated at progress 0 and progress 1 for two frames
- **THEN** outputs equal the previous frame and next frame respectively

#### Scenario: Dissolve is reproducible
- **WHEN** dissolve is evaluated twice at the same progress for the same frame pair at the same resolution
- **THEN** both outputs are pixel-identical

### Requirement: Slot-boundary emission
When transitions are enabled, `serveFeed` SHALL emit intermediate composite frames between consecutive slots — step count derived from `transition_ms` (clamped 6–16) with paced sends — before holding the incoming slot. Ramp messages SHALL use the unchanged `{format, image, source, next}` shape with `source` set to the incoming source. The hold deadline SHALL start after the ramp completes.

#### Scenario: Ramp between two sources
- **WHEN** style is `fade`, duration 500 ms, and the feed advances from weather to calendar
- **THEN** the device receives ~12 composite messages labeled "Calendar" before the normal calendar slot message, then holds

#### Scenario: Message shape unchanged
- **WHEN** any ramp frame is sent
- **THEN** it parses with the existing device client code without modification

### Requirement: Transition skip conditions
Transitions SHALL be skipped when: style is `none`; either boundary frame is missing (first slot, failed render, stale fallback); a notification interrupts; a skip command arrives mid-ramp (remaining steps abort immediately); or pause engages mid-ramp (current step finishes, then pause is honored). Notifications SHALL take precedence over starting a ramp.

#### Scenario: Skip truncates the ramp
- **WHEN** the operator sends next while a dissolve is mid-reveal
- **THEN** remaining reveal frames are dropped and the incoming slot's full frame is shown immediately

#### Scenario: Failed render skips the ramp
- **WHEN** the incoming source fails to render at a boundary
- **THEN** no transition frames are emitted and the existing failure/fallback behavior applies untouched

### Requirement: Compatibility with cache, health, and other feeds
Transition compositing SHALL NOT read from or write to the last-known-good cache (it composes already-cached frames), SHALL NOT alter health recording or display analytics, and SHALL apply uniformly to the preview feed, device feeds, and device previews. The TRMNL/e-ink HTTP polling path SHALL be unaffected.

#### Scenario: LKG semantics untouched
- **WHEN** transitions run between two healthy sources
- **THEN** cache entries, staleness flags, and health snapshots are identical to a run with transitions disabled

#### Scenario: E-ink path unaffected
- **WHEN** a TRMNL device polls its HTTP endpoint while transitions are enabled
- **THEN** it receives single static images exactly as before
