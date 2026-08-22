## ADDED Requirements

### Requirement: Datasource type registry

The system SHALL maintain a registry mapping endpoint names (e.g., "sonarr", "radarr", "weather") to their Ent client operations for create, read, update, and delete.

#### Scenario: All datasource types registered

- **WHEN** the application starts
- **THEN** all 8 token/URL datasource types (sonarr, radarr, f1, weather, homeassistant, untappd, crypto, stock) SHALL be registered in the datasource registry

### Requirement: Generic CRUD handlers

The system SHALL provide generic handler functions that accept a datasource type from the registry instead of per-type switch statements.

#### Scenario: Generic create handler

- **WHEN** a POST request is made to `/admin/datasources/sonarr/new`
- **THEN** the generic create handler SHALL create a Sonarr record using the registry AND add the edge to GeneralSettings AND redirect to `/admin/`

#### Scenario: Generic edit handler

- **WHEN** a GET request is made to `/admin/datasources/radarr/1/edit`
- **THEN** the generic edit handler SHALL look up the Radarr record by ID using the registry AND render the `datasource_form.html` template with the record data

#### Scenario: Generic update handler

- **WHEN** a POST request is made to `/admin/datasources/weather/1/edit`
- **THEN** the generic update handler SHALL update the Weather record by ID using the registry AND redirect to `/admin/`

#### Scenario: Generic delete handler

- **WHEN** a POST request is made to `/admin/datasources/crypto/1/delete`
- **THEN** the generic delete handler SHALL delete the Crypto record by ID using the registry AND redirect to `/admin/`

### Requirement: Per-type handler functions become thin wrappers

Each per-type handler function SHALL be a single-line call to the generic handler, eliminating the current switch-case repetition.

#### Scenario: One-liner wrappers

- **WHEN** inspecting `AdminSonarrCreate`, `AdminSonarrEdit`, `AdminSonarrUpdate`, `AdminSonarrDelete`
- **THEN** each function SHALL be a single call like `s.genericCreate(c, "sonarr")` with no switch or conditional logic
