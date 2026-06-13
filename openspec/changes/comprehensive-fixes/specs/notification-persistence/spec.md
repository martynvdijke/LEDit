## ADDED Requirements

### Requirement: Notifications stored in database

The system SHALL persist notification history in a new `Notification` Ent schema backed by SQLite.

#### Scenario: Notification schema

- **WHEN** inspecting the database schema
- **THEN** a `notifications` table SHALL exist with columns: id (int, PK), title (string), message (string), created_at (timestamp)

#### Scenario: Notification saved on creation

- **WHEN** `AddNotification` is called
- **THEN** the system SHALL insert a new record into the `notifications` table AND keep the in-memory queue for feed display

#### Scenario: Notifications loaded on startup

- **WHEN** the application starts
- **THEN** the system SHALL load the most recent 50 notifications from the database into the in-memory queue

#### Scenario: Notification history endpoint uses DB

- **WHEN** a GET request is made to `/admin/notifications`
- **THEN** the page SHALL display notifications from the database, not just in-memory

### Requirement: In-memory queue retained for feed performance

The in-memory notification queue SHALL be retained for the live WebSocket feed to avoid database reads on every display cycle.

#### Scenario: Feed uses in-memory queue

- **WHEN** the WebSocket feed processes a priority message
- **THEN** the system SHALL read from the in-memory queue, not from the database
