# matrix-grid-layout Specification

## Purpose
TBD - created by archiving change matrix-dashboard-rendering. Update Purpose after archive.
## Requirements
### Requirement: Matrix layout configuration
The system SHALL allow an administrator to create, edit, and delete matrix layouts. Each layout SHALL have a name, a row count, a column count, an optional panel gap in pixels, an optional background color, and an enabled flag.

#### Scenario: Create a matrix layout
- **WHEN** an administrator saves a new matrix layout named "Live" with 2 rows, 3 columns, and a 2-pixel gap
- **THEN** the layout is persisted and appears in the matrix layout list

#### Scenario: Delete a matrix layout
- **WHEN** an administrator deletes an existing matrix layout
- **THEN** the layout is removed from persistence and no longer appears in the source list

### Requirement: Panel bindings to datasources
The system SHALL let an administrator bind a datasource to each cell of a matrix layout. A binding SHALL reference a cell position (row, column) and a concrete datasource of any configured type (weather, crypto, stocks, F1, calendar, news, generic API, Google Calendar, or another matrix layout).

#### Scenario: Bind a datasource to a cell
- **WHEN** an administrator binds the configured weather datasource to cell row 1, column 1 of a layout
- **THEN** that cell renders the weather datasource at the cell's panel size

#### Scenario: Reject invalid binding
- **WHEN** an administrator saves a binding whose row or column is outside the layout's grid dimensions
- **THEN** the save is rejected with a validation error and no binding is persisted

### Requirement: Composite matrix rendering
The system SHALL render a matrix layout as a single image by compositing each bound datasource's `GetPNG` output at its cell's computed panel size, separated by the configured gap, on the configured background color.

#### Scenario: Render a full grid
- **WHEN** a matrix layout with all cells bound renders at 64×64 pixels
- **THEN** the output is one 64×64 PNG image containing every bound panel at its correct cell position

#### Scenario: Render layout with an unbound cell
- **WHEN** a matrix layout contains a cell with no binding
- **THEN** the cell renders as an empty themed cell using the layout background color and the rest of the matrix renders normally

### Requirement: Small-panel legibility
The system SHALL scale panel text to fit the cell size. Panel font size SHALL scale relative to panel height, and cells below a minimum usable size SHALL render the bound source's title only.

#### Scenario: Render at 32×32 device resolution
- **WHEN** a matrix layout renders on a 32×32 device
- **THEN** each panel's text is scaled to its cell height and no panel content overflows its cell bounds

#### Scenario: Render an undersized cell
- **WHEN** a bound cell is smaller than the minimum usable panel size
- **THEN** the cell renders the source title only instead of overflowing content

### Requirement: Panel render caching
The system SHALL cache each bound panel's rendered image for a TTL of 60 seconds so that repeated matrix refreshes within the TTL do not re-fetch or re-render the upstream datasource.

#### Scenario: Refresh within TTL
- **WHEN** a matrix layout refreshes twice within 60 seconds for the same bound datasource
- **THEN** the second refresh reuses the cached panel image and does not call the datasource's fetch logic again

#### Scenario: Cache expiry
- **WHEN** a matrix layout refreshes after the 60-second TTL has elapsed for a bound datasource
- **THEN** the panel is re-fetched and re-rendered and the cache is updated

### Requirement: Feed integration
The system SHALL expose enabled matrix layouts as selectable sources in the feed, so a layout streams as one source over WebSocket like any other datasource.

#### Scenario: Matrix appears in the feed source list
- **WHEN** an enabled matrix layout exists
- **THEN** it appears in the feed source list and the feed streams its composite image during its turn

#### Scenario: Disabled layout is not streamed
- **WHEN** a matrix layout is disabled
- **THEN** it does not appear in the feed source list

### Requirement: Unresolved binding tolerance
The system SHALL render a matrix even when a binding references a datasource that no longer exists, showing an empty themed cell for that binding instead of failing the entire render.

#### Scenario: Bound datasource deleted
- **WHEN** a matrix layout is rendered and one bound datasource has been deleted
- **THEN** the deleted binding's cell renders as an empty themed cell and the remaining panels render normally

