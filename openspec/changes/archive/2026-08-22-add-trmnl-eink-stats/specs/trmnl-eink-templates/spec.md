## ADDED Requirements

### Requirement: Plugin assets directory
The repository SHALL contain a `trmnl/` directory with the TRMNL plugin assets.

#### Scenario: Required files present
- **WHEN** the repository is checked out
- **THEN** `trmnl/settings.yml` SHALL exist
- **THEN** `trmnl/full.liquid` SHALL exist
- **THEN** `trmnl/half_horizontal.liquid` SHALL exist
- **THEN** `trmnl/half_vertical.liquid` SHALL exist
- **THEN** `trmnl/quadrant.liquid` SHALL exist

### Requirement: Plugin settings
`settings.yml` SHALL configure the plugin as a polling strategy plugin that polls the LEDit stats endpoint, and SHALL include an instance URL custom field so users can point the plugin at their own LEDit host.

#### Scenario: Polling configuration
- **WHEN** a user installs the plugin via `settings.yml`
- **THEN** the plugin SHALL use `strategy: polling`
- **THEN** the polling URL SHALL resolve to the instance URL custom field value plus `/api/trmnl/stats`
- **THEN** the plugin SHALL declare a refresh interval appropriate for an always-on stats display (1440 minutes, matching the reference implementation)
- **THEN** the plugin SHALL declare a `url` custom field (field type `url`, placeholder `https://ledit.example.com`) for the LEDit instance address
- **THEN** the plugin SHALL declare an author bio custom field describing the plugin

### Requirement: Templates render stats
Each Liquid template SHALL render LEDit stats from the polling response using TRMNL `IDX_0` field access and standard TRMNL layout classes.

#### Scenario: Full-screen layout
- **WHEN** the full template is rendered
- **THEN** it SHALL display system stats (CPU cores, memory, load average) prominently
- **THEN** it SHALL display analytics (total displays, uptime, per-source counts)
- **THEN** it SHALL include the `title_bar` footer with the LEDit title

#### Scenario: Half-screen layouts
- **WHEN** a half template (`half_horizontal` or `half_vertical`) is rendered
- **THEN** it SHALL display the core system stats and key analytics in a compact layout suited to half-screen orientation

#### Scenario: Quadrant layout
- **WHEN** the quadrant template is rendered
- **THEN** it SHALL display a compact summary of system stats and analytics in a quadrant-sized layout

#### Scenario: Missing data handling
- **WHEN** a stats field is absent or `"--"` (e.g., load average unavailable)
- **THEN** the template SHALL render gracefully without errors and show the placeholder value
