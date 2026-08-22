# device-streaming Specification

## ADDED Requirements

### Requirement: Per-device WebSocket endpoint
The system SHALL expose a WebSocket endpoint at `GET /ws/device/<token>` that streams the same content feed as the browser preview, resolved for a specific device.

#### Scenario: Device connects
- **WHEN** a client opens `GET /ws/device/<token>` with a valid token for an enabled device
- **THEN** the connection SHALL be established and begin streaming frames

#### Scenario: Unknown token
- **WHEN** a client opens `GET /ws/device/<token>` with a token that matches no device
- **THEN** the connection SHALL be rejected/closed

#### Scenario: Disabled device
- **WHEN** a client opens `GET /ws/device/<token>` for a device whose `enabled` flag is false
- **THEN** the connection SHALL be rejected/closed

### Requirement: Per-device resolution rendering
The device stream SHALL render each datasource at the device's configured `width` × `height` rather than a hardcoded size.

#### Scenario: Non-square matrix
- **WHEN** a device is configured with a width and height other than 64×64 (for example 128×32)
- **THEN** frames sent to that device SHALL be rendered at 128×32

### Requirement: Per-device cycle interval
Each device stream SHALL advance between sources on its own `refresh_interval` (seconds), independent of the global timeout and of other devices.

#### Scenario: Default interval
- **WHEN** a device's `refresh_interval` is unset
- **THEN** the stream SHALL advance sources every 60 seconds

#### Scenario: Custom interval
- **WHEN** a device's `refresh_interval` is set to a value such as 30
- **THEN** that device's stream SHALL advance sources every 30 seconds without affecting other devices

### Requirement: Independent per-device feed control
Each device connection SHALL have its own pause/skip/next state, so controlling one device does not affect others.

#### Scenario: Pause one device
- **WHEN** a pause command is received on one device connection
- **THEN** only that connection pauses; other device connections continue cycling

### Requirement: Resolution-aware datasource rendering
The datasource interface SHALL accept explicit width and height so every datasource renders at the requested resolution.

#### Scenario: Datasource signature
- **WHEN** any datasource is rendered
- **THEN** it SHALL receive the target width and height and produce an image at that size

### Requirement: Shared source collection
The browser preview feed and the device feed SHALL both collect datasources using the same shared logic, so they never diverge in which sources are shown.

#### Scenario: Consistent sources
- **WHEN** the preview feed and a device feed are both active
- **THEN** both SHALL draw from the same configured datasources in the same order (subject to each feed's own random/shuffle and cycle settings)

### Requirement: Notification broadcast to all feeds
Priority/notification messages SHALL be delivered to every connected feed (the preview and all device streams) exactly once per connection.

#### Scenario: Notification reaches every device
- **WHEN** a priority/notification message is created while multiple device streams and the preview are connected
- **THEN** every connected stream SHALL receive the notification exactly once

#### Scenario: Late-connecting device
- **WHEN** a device connects after a notification has already been broadcast
- **THEN** that device SHALL NOT replay old notifications (it starts from the latest sequence position)
