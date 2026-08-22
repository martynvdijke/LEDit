# admin-live-preview Specification

## Purpose
TBD - created by archiving change matrix-dashboard-rendering. Update Purpose after archive.
## Requirements
### Requirement: On-demand datasource preview
The system SHALL render a live preview of any configured datasource (weather, crypto, stocks, F1, calendar, Google Calendar, news, generic API, RSS, Sonarr/Radarr, Home Assistant, Untappd) as a PNG image on demand from the admin UI, using the same datasource resolver and renderer as the feed so the preview matches what the display shows.

#### Scenario: Preview a datasource
- **WHEN** an administrator opens a datasource's live preview at a given width and height
- **THEN** the system renders the datasource with real fetched data and returns a PNG image of that size

#### Scenario: Preview uses the configured size
- **WHEN** a preview is requested at 64×64 pixels
- **THEN** the returned PNG is 64×64 pixels

#### Scenario: Upstream API failure during preview
- **WHEN** a datasource's upstream API fails during a preview
- **THEN** the preview returns the datasource's fallback/placeholder render instead of an error

### Requirement: On-demand matrix layout preview
The system SHALL render a live preview of any matrix layout as a single composite PNG on demand, showing each bound panel rendered with real data at its cell size.

#### Scenario: Preview a matrix layout
- **WHEN** an administrator requests a preview of a matrix layout with bound panels
- **THEN** the system returns one composite PNG containing every bound panel at its correct cell position

### Requirement: Preview does not affect the feed
The system SHALL isolate preview renders from the feed: generating a preview SHALL NOT change feed state, source ordering, pause/skip state, or display tracking.

#### Scenario: Preview while the feed is streaming
- **WHEN** an administrator requests previews while the feed is actively streaming
- **THEN** the feed continues streaming unchanged and preview renders do not appear in display analytics

### Requirement: Preview authentication
The system SHALL require an authenticated admin session for all preview endpoints and SHALL only render sources configured in the database, never URLs supplied via query parameters.

#### Scenario: Unauthenticated preview request
- **WHEN** an unauthenticated request hits a preview endpoint
- **THEN** the request is rejected with the standard admin authentication redirect/error

### Requirement: Live preview on datasource forms
The system SHALL show a live preview image on the create/edit form of each datasource type that updates as the form's URL/token/configuration changes, with debounced updates so keystrokes do not trigger a request per character.

#### Scenario: Form changes update the preview
- **WHEN** an administrator edits a datasource's URL in its form
- **THEN** the preview image updates to reflect the new configuration shortly after editing stops

### Requirement: Matrix editor with live composite preview
The system SHALL provide a matrix editor page with controls for rows, columns, gap, background color, and per-cell datasource bindings, plus a live composite preview image that updates as grid settings or bindings change.

#### Scenario: Editor reflects grid settings
- **WHEN** an administrator changes the row or column count in the matrix editor
- **THEN** the live preview updates to the new grid dimensions

#### Scenario: Editor reflects a cell binding
- **WHEN** an administrator binds a datasource to a cell in the matrix editor
- **THEN** the live preview updates to show that datasource rendered in that cell

#### Scenario: Invalid grid settings show a warning
- **WHEN** an administrator enters grid settings that exceed the minimum or maximum allowed dimensions
- **THEN** the editor shows a validation warning and the preview does not update until the values are valid

### Requirement: PNG template export
The system SHALL export a matrix layout as a PNG template image showing the grid structure: layout background, cell grid lines, per-cell coordinates, and the bound source's short name in each bound cell, with unbound cells marked empty.

#### Scenario: Export a template
- **WHEN** an administrator downloads the template for a matrix layout at a given size
- **THEN** the system returns a PNG showing the grid with per-cell coordinates and bound source names

#### Scenario: Template reflects unbound cells
- **WHEN** a matrix layout has unbound cells and its template is exported
- **THEN** the unbound cells are visibly marked as empty in the template

