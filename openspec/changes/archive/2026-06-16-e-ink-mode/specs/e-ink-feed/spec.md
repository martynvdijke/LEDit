## ADDED Requirements

### Requirement: Matrix overlay hidden in e-ink mode
The live feed SHALL hide the LED matrix overlay grid when e-ink mode is active.

#### Scenario: Matrix canvas is hidden
- **WHEN** `.eink-mode` is active on the live feed page
- **THEN** the `#matrix-overlay` canvas SHALL have `display: none`

### Requirement: Clock overlay frozen
The live feed SHALL stop the live clock overlay updates in e-ink mode.

#### Scenario: Clock update interval cleared
- **WHEN** `.eink-mode` is active on the live feed page
- **THEN** the `setInterval` for the clock overlay SHALL be cleared; the clock SHALL display the time at activation and not update every second

### Requirement: Marquee becomes static
The marquee scrolling animation SHALL be disabled in e-ink mode.

#### Scenario: Marquee animation stops
- **WHEN** `.eink-mode` is active on the live feed page
- **THEN** the `#marquee-text` element SHALL have `animation: none`; its content SHALL be displayed as static, non-scrolling text with `overflow: hidden`

### Requirement: Image fade transitions removed
The live feed SHALL remove fade-in/out transitions on image display in e-ink mode.

#### Scenario: Images display without transition
- **WHEN** `.eink-mode` is active and a new image arrives via WebSocket
- **THEN** the `#media-display` SHALL update immediately without any `opacity` transition or `.fade-out` class manipulation

### Requirement: Manual refresh button shown
The live feed SHALL show a "Refresh Display" button in e-ink mode for manual feed advancement.

#### Scenario: Refresh button is visible
- **WHEN** `.eink-mode` is active on the live feed page
- **THEN** a "Refresh Display" button SHALL be visible in the feed controls area

#### Scenario: Refresh triggers next feed item
- **WHEN** user clicks "Refresh Display" in e-ink mode
- **THEN** the system SHALL send a `next` action via WebSocket and display the next feed item

### Requirement: Configurable auto-refresh interval
The system SHALL allow configuring an auto-refresh interval for the feed page in e-ink mode.

#### Scenario: Default refresh interval
- **WHEN** e-ink mode becomes active on the feed page with no custom interval set
- **THEN** the feed SHALL auto-refresh every 30 seconds by sending a `next` WebSocket action

#### Scenario: Refresh interval configurable via cookie
- **WHEN** a `ledit_eink_refresh` cookie is present with a numeric value in seconds
- **THEN** the auto-refresh interval SHALL use that value instead of the 30-second default

### Requirement: WebSocket status display simplified
The feed page SHALL simplify the connection status display in e-ink mode.

#### Scenario: Status text without color changes
- **WHEN** `.eink-mode` is active
- **THEN** `#status-text` SHALL show connection state as plain text without color-coded CSS classes (green/red/blue)
