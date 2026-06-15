## ADDED Requirements

### Requirement: Theme settings stored in database

The system SHALL persist custom theme settings (background color, accent color, text color, title, font size) in the database.

#### Scenario: Theme data model

- **WHEN** inspecting the theme storage mechanism
- **THEN** theme settings SHALL be stored as a JSON field on the `GeneralSettings` Ent schema

### Requirement: AdminThemeSave persists data

The `AdminThemeSave` handler SHALL write submitted theme form data to the database.

#### Scenario: Save theme

- **WHEN** a POST request is made to `/admin/theme` with valid color and font size values
- **THEN** the system SHALL save the theme settings to the database AND redirect to `/admin/theme` with a success flash message

#### Scenario: Load saved theme

- **WHEN** a GET request is made to `/admin/theme` and a saved theme exists
- **THEN** the theme editor form SHALL be pre-populated with the saved values

### Requirement: Theme applied to dashboard preview

The saved theme SHOULD be applied when rendering the admin dashboard or feed display.

#### Scenario: Theme reflected in rendering

- **WHEN** a custom theme is saved AND the feed displays content
- **THEN** the rendering engine SHALL use the saved theme values instead of hardcoded defaults
