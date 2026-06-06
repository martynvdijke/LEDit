## ADDED Requirements

### Requirement: Admin log viewer page
The system SHALL provide an admin-accessible log viewer page at `/admin/logs` that displays persisted log entries in a table.

#### Scenario: View logs page
- **WHEN** an admin navigates to `/admin/logs`
- **THEN** the system renders a page showing log entries with columns: timestamp, level, source, message

#### Scenario: Filter by log level
- **WHEN** an admin selects a log level filter (trace, debug, info, warn, error, fatal)
- **THEN** the system SHALL display only log entries at that level or above (based on configured verbosity)

#### Scenario: Search by message
- **WHEN** an admin types a search query into the search field
- **THEN** the system SHALL display only log entries whose message contains the query text

#### Scenario: Filter by source
- **WHEN** an admin selects a source filter (e.g., "email-settings", "ai-settings", "feed")
- **THEN** the system SHALL display only log entries from that source

#### Scenario: Empty state
- **WHEN** there are no log entries matching the current filters
- **THEN** the system SHALL display a "No log entries found" message

#### Scenario: Pagination
- **WHEN** there are more than 100 log entries matching the current filters
- **THEN** the system SHALL paginate the results, showing 100 per page

### Requirement: Log entry severity badges
The system SHALL display log entries with color-coded severity badges.

#### Scenario: Severity colors
- **WHEN** log entries are displayed in the table
- **THEN** entries SHALL show a colored badge: trace (gray), debug (blue), info (green), warn (yellow), error (red), fatal (dark red)

### Requirement: Log viewer nav item
The system SHALL include a "Logs" link in the admin sidebar navigation.

#### Scenario: Nav item present
- **WHEN** an admin views any admin page
- **THEN** the sidebar SHALL include a "Logs" link pointing to `/admin/logs`

#### Scenario: Active state
- **WHEN** an admin is on the logs page
- **THEN** the "Logs" nav item SHALL appear active/highlighted
