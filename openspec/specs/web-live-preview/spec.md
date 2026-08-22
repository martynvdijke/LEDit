# web-live-preview Specification

## Purpose
TBD - created by archiving change web-live-preview. Update Purpose after archive.
## Requirements
### Requirement: Device-accurate preview feed
The system SHALL provide an admin-authenticated WebSocket endpoint that streams what a specific device sees, at the device's resolution and refresh interval, without affecting device liveness.

#### Scenario: Preview connects by device id
- **WHEN** an authenticated admin connects to `/ws/device/:id/preview` for an existing enabled device
- **THEN** the system SHALL stream frames at the device's configured width, height, and refresh interval using an independent feed controller

#### Scenario: Preview never marks the device seen
- **WHEN** a preview connection is established or closed
- **THEN** the system SHALL NOT update the device's `last_seen_at`

#### Scenario: Preview without device token
- **WHEN** an authenticated admin connects to the preview endpoint
- **THEN** the system SHALL NOT require the device token; the admin session SHALL be sufficient

#### Scenario: Missing or disabled device
- **WHEN** the requested device id does not exist or the device is disabled
- **THEN** the system SHALL reject the connection with a clear error

#### Scenario: Preview controls are local
- **WHEN** an admin pauses, resumes, or skips in a device preview
- **THEN** only that preview connection SHALL be affected; the physical device and other connections SHALL be unaffected

### Requirement: Per-device live preview page
The system SHALL provide an admin page that displays the device-accurate live preview.

#### Scenario: Preview page renders live feed
- **WHEN** an authenticated admin visits `/admin/devices/:id/preview`
- **THEN** the page SHALL connect to the device-accurate preview endpoint and display frames in the live feed view with matrix overlay, source/next labels, and connection status

#### Scenario: Preview page shows device metadata
- **WHEN** the preview page is displayed
- **THEN** the page SHALL show the device name, resolution, and refresh interval alongside the feed

#### Scenario: Stale frame badge
- **WHEN** a received frame is marked stale (`stale: true` with a stale age)
- **THEN** the preview page SHALL display a stale indicator showing the frame's age

#### Scenario: No stale flag means no badge
- **WHEN** a received frame has no stale flag
- **THEN** the preview page SHALL NOT show a stale indicator

### Requirement: Device selector on the main feed page
The system SHALL let the main feed page switch between the shared preview and any configured device.

#### Scenario: Selector lists devices
- **WHEN** at least one device is configured and the main feed page loads
- **THEN** the page SHALL offer a selector with "Shared preview" plus one entry per configured device

#### Scenario: Switching devices reconnects
- **WHEN** a user selects a device in the selector
- **THEN** the page SHALL close the current WebSocket and reconnect to that device's preview endpoint, preserving connection status and reconnection behavior

#### Scenario: Selector hidden without devices
- **WHEN** no devices are configured
- **THEN** the main feed page SHALL show no selector and keep the current shared-preview behavior

### Requirement: Hardware feed path unchanged
The system SHALL keep the existing device feed endpoint (`/ws/device/:token`) behavior identical for hardware clients.

#### Scenario: Hardware devices unaffected
- **WHEN** a hardware device connects via its token
- **THEN** it SHALL receive the same feed behavior as before this change, including `last_seen_at` marking

