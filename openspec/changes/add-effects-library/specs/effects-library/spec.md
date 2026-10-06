# Spec: effects-library

## ADDED Requirements

### Requirement: Effect registry and common interface

The system SHALL provide a procedural effect engine in `render/effects` with a common `Effect` interface evaluated per tick at the target resolution and a named, stable registry. The registry SHALL include at least the effects `plasma`, `fire`, `sparkle`, `scan`, `noise`, and `gradient`, SHALL expose the registered names in a deterministic order for pickers and tests, and SHALL reject lookup of a name that is not registered. The registry SHALL also expose byte-identical adapters for the existing animation code paths (`render/screensaver`, `render/matrixrain`) so those looks can be reused without reimplementation.

#### Scenario: Built-in effects are registered

- **WHEN** the effect registry is queried
- **THEN** it SHALL contain at least `plasma`, `fire`, `sparkle`, `scan`, `noise`, and `gradient`, each implementing the common effect interface and reporting its own name

#### Scenario: Unknown effect rejected

- **WHEN** a preset or parameter set names an effect that is not registered
- **THEN** the engine SHALL return a validation error and SHALL NOT render or persist the unknown effect

#### Scenario: Legacy adapter is byte-identical

- **WHEN** a legacy adapter effect (for example `legacy-matrix-rain`) is rendered for a given elapsed time and dimensions
- **THEN** its output SHALL be byte-identical to the existing legacy renderer invoked with the same elapsed time and dimensions

### Requirement: Parameter model and validation

The system SHALL parameterize every effect with a palette of theme color roles plus `speed`, `intensity`, and `density` percentages in the range 0–100, a `direction` from the closed set `up|down|left|right|none`, and an integer `seed`. Each registered effect SHALL declare its default parameter values and per-field bounds, and the system SHALL validate parameter sets against them at the point of save with a field-level error, rejecting unknown effects, out-of-range values, and invalid directions. Parameter sets SHALL serialize to a versioned JSON document that round-trips without loss, and rendering SHALL clamp out-of-range input defensively rather than failing or drawing out of bounds.

#### Scenario: Defaults when unspecified

- **WHEN** an administrator saves a preset for an effect without touching its parameters
- **THEN** the preset SHALL persist the effect's declared defaults for speed, intensity, density, direction, and seed

#### Scenario: Out-of-range parameter rejected

- **WHEN** a parameter set supplies a speed, intensity, or density outside 0–100, or a direction outside the closed set
- **THEN** the save SHALL fail with a validation error identifying the offending field and the preset SHALL NOT be stored

#### Scenario: Parameter round-trip

- **WHEN** a parameter set is serialized and deserialized
- **THEN** the effect name, palette roles, numeric parameters, direction, and seed SHALL be preserved exactly

#### Scenario: Defensive clamping at render time

- **WHEN** an effect is asked to render with an out-of-range parameter value that bypassed save-time validation
- **THEN** the engine SHALL clamp the value into its declared bounds and SHALL render a bounded frame without error

### Requirement: Deterministic tick-quantized rendering

Effect rendering SHALL be a pure function of the effect, its parameters, its seed, and a time tick quantized to the engine's fixed animation interval of 66 ms (~15 fps, matching the existing animator cadence). The engine SHALL NOT read the wall clock, global random state, or any mutable shared state during `Draw`. Identical inputs SHALL produce byte-identical frames regardless of call order or render path (feed, preview, or push), and an animated effect SHALL produce a different frame on a different tick.

#### Scenario: Same tick reproduces the same frame

- **WHEN** the same effect, parameters, seed, and tick are rendered twice, on any render path
- **THEN** the two frames SHALL be byte-identical

#### Scenario: Animation advances with the tick

- **WHEN** an animated effect is rendered at successive 66 ms ticks
- **THEN** at least two of the rendered frames SHALL differ while each remains reproducible for its own tick

#### Scenario: Seed selects the pattern

- **WHEN** the same effect renders with two different seeds at the same dimensions and tick
- **THEN** the frames SHALL differ, and each seed SHALL remain stable across repeated renders

### Requirement: Resolution-aware rendering and bounds safety

Effects SHALL render at any caller-supplied positive width and height, including common LED sizes (64×32, 128×64, 256×128), small matrix cells, non-square sizes, and the logical canvas width that includes panel bezel gaps. Drawing SHALL stay within the destination bounds for every size, including 1×1, and the engine SHALL NOT allocate or draw outside the target frame.

#### Scenario: Common resolutions render

- **WHEN** an effect renders at 64×32, 128×64, and 256×128
- **THEN** each frame SHALL exactly match the requested dimensions and SHALL fill its destination without clipping errors

#### Scenario: Tiny and non-square sizes are safe

- **WHEN** an effect renders at 1×1 and at a non-square cell such as 20×12
- **THEN** the render SHALL complete without panic and every written pixel SHALL be inside the destination bounds

#### Scenario: Logical canvas width

- **WHEN** a multi-panel device renders an effect on the logical canvas (physical width plus bezel gaps)
- **THEN** the effect SHALL render at the logical dimensions and the existing gap-slicing step SHALL produce the physical frame unchanged

### Requirement: Theme palette consumption

The engine SHALL color every effect from the effective theme palette resolved by the existing theme pipeline (per-source override, else global default, else built-in default). Effects SHALL derive all frame colors from the supplied palette roles and SHALL NOT contain literal fixed brand colors. A different effective palette SHALL produce a different frame, and preview rendering SHALL honor unsaved theme tokens exactly like the theme editor. Rendered frames SHALL be cached under a key that includes the theme and effect parameters, so a palette or parameter change never serves a stale cached frame.

#### Scenario: Palette change changes the frame

- **WHEN** the same effect renders with the built-in default theme and then with a theme whose accent and text colors differ
- **THEN** the two frames SHALL differ in the palette-derived pixels

#### Scenario: Source override wins

- **WHEN** an effect source has a per-source theme override and the global default differs
- **THEN** the rendered frame SHALL use the override palette

#### Scenario: Preview honors unsaved theme tokens

- **WHEN** an administrator previews an effect while editing theme colors without saving
- **THEN** the preview SHALL render with the unsaved tokens and SHALL match the feed path for those tokens

#### Scenario: No stale cache across palette changes

- **WHEN** the effective theme for an effect changes between two renders
- **THEN** the second render SHALL NOT be served from a cache entry created for the first palette

### Requirement: Effects as selectable sources

An effect preset SHALL be usable as an ordinary selectable source identified as `effect:<preset id>` across the source list, binding options, playlists, and schedules, exactly like existing built-in sources. The effect source SHALL implement the datasource render contract, accept a caller-supplied theme, report itself as animated and ambient to the existing animator and panel-cache paths, re-render at the engine tick cadence during its slot, perform no network or data fetch, and be reported as always healthy in the health registry. A reference to a missing or deleted preset SHALL degrade through the existing unresolved-source tolerance so the feed continues to cycle.

#### Scenario: Effect appears in pickers

- **WHEN** an administrator opens a playlist, matrix-cell, composition, or schedule picker
- **THEN** saved effect presets SHALL appear as a selectable group alongside the existing sources

#### Scenario: Playlist reference round-trips

- **WHEN** a playlist item `{source_type:"effect", source_id:<preset id>}` is saved and reloaded
- **THEN** the reference SHALL round-trip identically and the feed SHALL render that effect during its slot

#### Scenario: Animation during the slot

- **WHEN** an effect source is active and the animator path is available
- **THEN** the device SHALL receive updated frames at the engine tick cadence throughout the slot

#### Scenario: Zero network and always healthy

- **WHEN** the server has no external connectivity and an effect source renders
- **THEN** it SHALL render frames without error and its health status SHALL remain healthy without contributing to degraded counts

#### Scenario: Deleted preset tolerated

- **WHEN** a playlist or binding references an effect preset that has been deleted
- **THEN** the feed SHALL continue cycling by the existing unresolved-source tolerance and SHALL NOT fail the connection

### Requirement: Composition with existing panel, composite, and overlay surfaces

The engine SHALL compose with the existing render pipeline without forking it: effect sources SHALL be resolvable as matrix cells, composition regions, scene content, full-canvas backgrounds, and screensaver/animated slots through the existing source index; the overlay strip SHALL optionally render an effect behind its text; and the panel-canvas logical width, last-known-good caching, transitions, brightness, and physical slicing steps SHALL be unchanged. When no effect is configured for a surface, that surface's output SHALL be byte-identical to the behavior before this capability existed.

#### Scenario: Matrix cell and composition region

- **WHEN** an effect preset is bound to a matrix cell or composition region
- **THEN** the cell/region SHALL render the effect at its cell size and SHALL animate while visible

#### Scenario: Scene content

- **WHEN** a scene action sets `source_type` to `effect` with a valid preset id
- **THEN** the scene tier SHALL resolve through the existing source index and render that effect during the scene

#### Scenario: Overlay strip effect

- **WHEN** an overlay spec carries an effect and is enabled
- **THEN** the strip SHALL composite the effect frame behind the overlay text using the existing send-time overlay compositing path

#### Scenario: Overlay without effect is unchanged

- **WHEN** an overlay spec has no effect configured
- **THEN** the composited frame SHALL be byte-identical to the output before this capability existed

#### Scenario: Background layer

- **WHEN** an effect is used as a full-canvas source or as the base region of a composition
- **THEN** it SHALL render as the visible base layer at the canvas dimensions without changes to the compositor's geometry or theme handling

### Requirement: Performance budgets and buffer reuse

The engine SHALL bound per-frame cost with documented budgets at 64×32, 128×64, and 256×128, guarded by benchmarks that fail when a built-in effect exceeds its budget by more than the documented CI margin. A warm renderer SHALL reuse its scratch buffer so steady-state rendering adds no per-frame scratch allocation growth, SHALL NOT mutate a frame after returning it, and SHALL be safe for concurrent use by different renderers without shared mutable state.

#### Scenario: Benchmark budgets enforced

- **WHEN** the effect benchmark suite runs for 64×32, 128×64, and 256×128
- **THEN** each built-in effect SHALL stay within its documented per-frame budget (≤ 0.5 ms, ≤ 2 ms, and ≤ 6 ms respectively on the reference host) within the CI margin, measured with allocation reporting

#### Scenario: No steady-state allocation growth

- **WHEN** a renderer is warmed up and then renders many consecutive frames at a fixed size
- **THEN** the per-frame scratch allocations SHALL NOT grow with the frame count

#### Scenario: Concurrent renderers are independent

- **WHEN** two effect renderers render concurrently at different sizes or parameters
- **THEN** each SHALL produce its documented deterministic output with no data race and no cross-contamination of buffers

### Requirement: Backward compatibility and opt-in adoption

The effects engine SHALL be additive: existing screensaver variants, matrix rain, and the audio visualizer SHALL keep their current code paths and byte-identical output, and SHALL NOT be automatically re-routed through the engine. Frames for sources and devices that do not use an effect SHALL remain byte-identical. The WebSocket frame protocol and device-facing surfaces SHALL be unchanged, and the engine SHALL be implemented with the Go standard library only, adding no required runtime dependency.

#### Scenario: Legacy output unchanged

- **WHEN** the existing screensaver, matrix-rain, and visualizer renderers are exercised after this change
- **THEN** their golden frame hashes and behavior SHALL match the pre-change results

#### Scenario: Devices without effects are byte-identical

- **WHEN** a device or source has no effect configured
- **THEN** the frames it receives SHALL be byte-identical to frames produced before this capability existed

#### Scenario: Protocol unchanged

- **WHEN** a device connects and receives an effect frame
- **THEN** the WebSocket message shape (`format`, `image`, `source`, `next`) and the device protocol SHALL be unchanged

#### Scenario: No new dependency

- **WHEN** the module graph is inspected after this change
- **THEN** `go.mod` SHALL have no new direct requirement introduced by the effects engine
