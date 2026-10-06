# Spec: text-choreography

## ADDED Requirements

### Requirement: Text reveal property

A timeline MAY declare `reveal` keyframes that control how many leading characters of a text base are visible. The value SHALL be an integer character count, clamped to `[0, text length]` at render time, and SHALL be applied by the server renderer as part of timeline execution. A reveal of 0 SHALL render no characters and a reveal at or beyond the text length SHALL render the full text.

#### Scenario: Reveal grows over time
- **WHEN** a `reveal` track runs from 0 to 10 over 1000 ms and the timeline is sampled at 500 ms with linear easing
- **THEN** five leading characters SHALL be visible

#### Scenario: Reveal zero hides text
- **WHEN** the sampled reveal value is 0
- **THEN** no text characters SHALL be drawn for the base

#### Scenario: Reveal clamps to the text length
- **WHEN** a keyframe declares a reveal greater than the base text length
- **THEN** the rendered text SHALL show all characters and SHALL NOT error

#### Scenario: Reveal rounds at character boundaries
- **WHEN** an interpolated reveal value falls between two character counts
- **THEN** the renderer SHALL use a consistent floor boundary so adjacent samples never produce partial-glyph flicker

### Requirement: Character-accurate reveal capability

Text bases SHALL be able to implement an optional `RevealRenderer` capability that renders exactly `reveal` characters. The built-in text slide source SHALL implement it. For any base that does not implement it, the renderer SHALL fall back to a deterministic pixel-column mask computed from the existing simple-font metrics, masking the hidden region to the background color. Neither path SHALL require changes to the `Datasource` interface.

#### Scenario: Text slide reveals exactly
- **WHEN** a timeline wraps a text slide and samples reveal 7
- **THEN** exactly the first seven characters SHALL be rendered and the remainder SHALL be absent

#### Scenario: Fallback mask for other bases
- **WHEN** a timeline applies reveal to a base that does not implement `RevealRenderer`
- **THEN** the renderer SHALL mask pixels beyond the reveal boundary with the background instead of failing

#### Scenario: No contract break
- **WHEN** an existing datasource that never implements `RevealRenderer` is compiled and streamed
- **THEN** it SHALL behave exactly as before

### Requirement: Text slide timeline attachment

Text slides SHALL be attachable as a timeline base through the existing source reference, with no change to the text slide storage format or its datasource contract. Unwrapped text slides SHALL render and transition exactly as before. A wrapped text slide's timeline SHALL start when its slot begins, and scene activation SHALL restart it.

#### Scenario: Static text slide unchanged
- **WHEN** a text slide is streamed without a wrapping timeline
- **THEN** it SHALL render identically to a build before this capability

#### Scenario: Wrapped text slide animates on slot start
- **WHEN** a timeline wrapping a text slide enters its feed slot
- **THEN** the reveal SHALL start from the first keyframe of that slot

#### Scenario: Scene activation restarts the reveal
- **WHEN** a scene referencing a text-slide timeline activates
- **THEN** the reveal SHALL restart from the beginning at activation

### Requirement: State-triggered choreography effects

Timelines SHALL support bounded hooks that map a value path exposed by the base's existing optional `StateProvider` capability to a built-in effect: `draw_in` (progressive reveal of a target region), `flash` (a short brightness/color envelope), or `tick` (a periodic pulse). Hook effects SHALL be evaluated server-side from the same transform primitives as property keyframes, SHALL have bounded durations, and SHALL NOT introduce new required methods on any datasource interface or change any existing fetch cadence.

#### Scenario: Score flash on change
- **WHEN** the base's state value at a hooked path changes between ticks
- **THEN** a `flash` envelope SHALL run for its configured duration and then return to the authored property values

#### Scenario: Chart draw-in
- **WHEN** a `draw_in` hook targets a region and its state path first becomes available
- **THEN** the region SHALL be revealed progressively over the hook duration and then hold

#### Scenario: Countdown tick
- **WHEN** a `tick` hook is configured against a per-second countdown value
- **THEN** a deterministic pulse SHALL occur once per value change, with identical output for identical ticks

#### Scenario: Missing state provider is inert
- **WHEN** a timeline declares hooks but its base does not implement `StateProvider`
- **THEN** the hooks SHALL be inert and the rest of the timeline SHALL render normally

#### Scenario: State failure does not break rendering
- **WHEN** the base's `CurrentState` returns an error at a tick
- **THEN** the renderer SHALL keep the last known values (or leave the hook inert) and SHALL NOT fail the frame

### Requirement: Choreography bounds and determinism

Hook count SHALL be capped at 16 per timeline and effect durations SHALL be bounded and validated with 400-style errors. Effects SHALL be deterministic for identical ticks and inputs, SHALL compose with the timeline's `opacity`/`brightness`/`reveal` properties without undefined ordering, and SHALL NOT read upstream data more often than the base source already does.

#### Scenario: Over-cap hooks rejected
- **WHEN** a timeline declares more hooks than the cap or an effect duration outside its bound
- **THEN** the system SHALL reject the document with a descriptive 400-style error

#### Scenario: Deterministic effect output
- **WHEN** the same tick is rendered twice for a timeline with an active hook
- **THEN** the two frames SHALL be byte-identical

#### Scenario: No extra upstream fetches
- **WHEN** a hook is active during a slot
- **THEN** the system SHALL NOT issue additional upstream datasource requests beyond what the base source already makes

### Requirement: Compatibility with transitions, scenes, and existing feeds

State-triggered effects and reveals SHALL honor feed skip and pause behavior, SHALL NOT change slide transitions or the device protocol, and SHALL be reset by scene activation. Timelines without hooks or text bases SHALL be unaffected by this capability.

#### Scenario: Skip mid-effect
- **WHEN** the feed skips while a reveal or effect is mid-progress
- **THEN** the slot SHALL end promptly and the next slot SHALL render normally

#### Scenario: Transitions are untouched
- **WHEN** slide transitions are enabled around a text-slide timeline
- **THEN** transition ramp frames SHALL use the existing blend functions and message shape

#### Scenario: No hooks means no change
- **WHEN** a timeline declares no hooks and no text base
- **THEN** this capability SHALL add no observable behavior to its rendering
