## ADDED Requirements

### Requirement: Log level verbosity setting
The system SHALL allow admins to configure the minimum log level displayed and persisted.

#### Scenario: Default verbosity is warn
- **WHEN** the system starts with no log settings configured
- **THEN** the minimum log level SHALL default to `warn`

#### Scenario: Admin changes verbosity
- **WHEN** an admin changes the verbosity level on the log settings page
- **THEN** the system SHALL persist the new level and apply it immediately

#### Scenario: Log settings page
- **WHEN** an admin navigates to `/admin/settings/logs`
- **THEN** the system SHALL render a page with a level selector and a retention period field

### Requirement: Log retention
The system SHALL automatically remove log entries older than a configurable retention period.

#### Scenario: Default retention
- **WHEN** the system starts with no log settings configured
- **THEN** the default retention period SHALL be 7 days

#### Scenario: Retention cleanup
- **WHEN** the cleanup goroutine runs
- **THEN** it SHALL delete log entries older than the configured retention period

#### Scenario: Configurable retention
- **WHEN** an admin sets a retention period on the log settings page
- **THEN** the system SHALL persist it and the cleanup goroutine SHALL use the new value
