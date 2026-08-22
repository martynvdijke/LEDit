# device-management Specification

## ADDED Requirements

### Requirement: Device token generation
Every device SHALL have a unique, randomly generated token that identifies it to the device stream.

#### Scenario: Token created
- **WHEN** an administrator creates a new device
- **THEN** the system SHALL generate and store a unique random token for that device

#### Scenario: Existing devices backfilled
- **WHEN** a device created before tokens existed is loaded
- **THEN** the system SHALL assign it a token if it has none

### Requirement: Per-device refresh interval configuration
The administrator SHALL be able to configure the refresh interval (seconds per source) for each device.

#### Scenario: Default value
- **WHEN** a device is created without an explicit refresh interval
- **THEN** the interval SHALL default to 60 seconds

#### Scenario: Interval edited
- **WHEN** an administrator edits a device and sets a refresh interval
- **THEN** the new value SHALL be stored and used by that device's stream

### Requirement: Connection URL and token display
The device list SHALL show each device's token and a usable connection URL so the device can be pointed at the server.

#### Scenario: Connection URL shown
- **WHEN** an administrator views the devices list
- **THEN** each device SHALL display its token (maskable/copyable) and a `ws://…/ws/device/<token>` connection URL

### Requirement: Connection status
The device list SHALL show a best-effort online/offline status derived from the device's last-seen time.

#### Scenario: Recently seen
- **WHEN** a device has connected recently
- **THEN** the device SHALL be shown as online

#### Scenario: Never seen or stale
- **WHEN** a device has never connected or its last-seen time is stale
- **THEN** the device SHALL be shown as offline

### Requirement: Legacy connection fields remain optional
The existing device connection fields (ip, port, username, password) SHALL remain editable but be optional and unused for WebSocket connections.

#### Scenario: Legacy fields present
- **WHEN** an administrator creates a device without filling ip/port/username/password
- **THEN** the device SHALL still be creatable and connectable via its token
