## ADDED Requirements

### Requirement: Analog clock ambience source
The system SHALL provide a built-in analog clock source that requires no configuration.

#### Scenario: Clock appears in feed
- **WHEN** the feed source list is built
- **THEN** the analog clock SHALL be present without any datasource configuration

#### Scenario: Clock renders current time
- **WHEN** the analog clock renders at a given width and height
- **THEN** the frame SHALL show a 12-hour dial with hour, minute, and second hands positioned for the current time
- **AND** the hands SHALL advance when the source is re-rendered in a later feed cycle

#### Scenario: Clock in matrix cells
- **WHEN** a matrix cell binding uses the analog clock type
- **THEN** the cell SHALL render the analog clock at the cell's resolution

### Requirement: Matrix rain ambience source
The system SHALL provide a built-in matrix rain source that requires no configuration.

#### Scenario: Rain appears in feed
- **WHEN** the feed source list is built
- **THEN** matrix rain SHALL be present without any datasource configuration

#### Scenario: Rain animates across re-renders
- **WHEN** matrix rain renders repeatedly across feed cycles
- **THEN** the glyph columns SHALL fall smoothly with a bright head and fading trail
- **AND** the animation SHALL be deterministic (the same point in time produces the same frame)

#### Scenario: Rain in matrix cells
- **WHEN** a matrix cell binding uses the matrix rain type
- **THEN** the cell SHALL render matrix rain at the cell's resolution

### Requirement: Countdown timer entity
The system SHALL provide countdown timer datasources with name, target time, optional label, and enabled state.

#### Scenario: Admin creates a countdown
- **WHEN** an authenticated admin creates a countdown with a name, target time, and label
- **THEN** the countdown SHALL appear in the datasource list and feed source selection

#### Scenario: Countdown renders remaining time
- **WHEN** a countdown's target is in the future and it renders
- **THEN** the frame SHALL show the label and remaining time in the format `Xd HH:MM:SS` (or `HH:MM:SS` under 24 hours, `MM:SS` under 1 hour)

#### Scenario: Countdown reached
- **WHEN** a countdown's target time has passed and it renders
- **THEN** the frame SHALL display a "DONE" state

#### Scenario: Admin edits or disables a countdown
- **WHEN** an authenticated admin edits the target time/label or disables a countdown
- **THEN** the change SHALL take effect on the next render and a disabled countdown SHALL be excluded from the feed and matrix bindings

### Requirement: Deterministic time-based rendering
All ambience renders SHALL be pure functions of the current time so the feed loop's re-renders produce animation without extra infrastructure.

#### Scenario: Renderers accept injected time
- **WHEN** an ambience renderer is invoked
- **THEN** it SHALL compute its frame from a provided time value, making the output deterministic for tests

#### Scenario: Rendering stays cheap
- **WHEN** ambience sources render at typical resolutions (32×32 to 400×400)
- **THEN** each render SHALL complete without unbounded allocations or per-frame growth (benchmark-guarded)
