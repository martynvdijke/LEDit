## ADDED Requirements

### Requirement: Admin can configure Umami analytics settings
The system SHALL provide an admin settings page at `/admin/settings/umami` for configuring a self-hosted Umami analytics instance.

#### Scenario: View Umami settings form
- **WHEN** an authenticated admin visits `/admin/settings/umami`
- **THEN** the system SHALL render a form with fields for Endpoint URL, Website ID, and an Enable toggle
- **AND** the form SHALL be pre-populated with existing settings if they exist

#### Scenario: Save Umami settings
- **WHEN** an admin submits the Umami settings form with valid data
- **THEN** the system SHALL save the settings to the database
- **AND** redirect back to `/admin/settings/umami` with the updated values shown

#### Scenario: Enable tracking
- **WHEN** an admin sets the Enable checkbox to checked and saves
- **THEN** the system SHALL set `enable` to `true` in the database
- **AND** the Umami tracking script SHALL be injected into public page templates

#### Scenario: Disable tracking
- **WHEN** an admin sets the Enable checkbox to unchecked and saves
- **THEN** the system SHALL set `enable` to `false` in the database
- **AND** the Umami tracking script SHALL NOT be rendered in public page templates

#### Scenario: First-time save creates settings
- **WHEN** an admin saves Umami settings for the first time
- **THEN** the system SHALL create a new `UmamiSettings` record
- **AND** link it to `GeneralSettings` via the existing edge relationship

#### Scenario: Subsequent save updates settings
- **WHEN** an admin saves Umami settings when a record already exists
- **THEN** the system SHALL update the existing `UmamiSettings` record
- **AND** preserve the relationship to `GeneralSettings`

### Requirement: UmamiSettings stored in database
The system SHALL store Umami configuration in a dedicated `umami_settings` database table managed by Ent.

#### Scenario: Schema fields
- **WHEN** the `UmamiSettings` schema is created
- **THEN** it SHALL have fields: `id` (int, auto), `endpoint` (string), `website_id` (string), `enable` (bool, default false)

#### Scenario: Edge to GeneralSettings
- **WHEN** the schema migration runs
- **THEN** `GeneralSettings` SHALL have a `umami_settings` edge to `UmamiSettings`
