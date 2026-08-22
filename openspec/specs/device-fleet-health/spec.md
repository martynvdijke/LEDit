# device-fleet-health Specification

## Purpose
TBD - created by archiving change source-health-monitoring. Update Purpose after archive.
## Requirements
### Requirement: Device liveness classification
Each device SHALL be classified as alive, stale, or never-connected based on its last seen timestamp versus 3× its refresh interval, refreshed automatically every 15 seconds on the devices page.
#### Scenario: Device is alive
- WHEN a device's last seen timestamp is within 3× its refresh interval of now
- THEN the device is shown with an alive badge

#### Scenario: Device is stale
- WHEN a device's last seen timestamp is older than 3× its refresh interval
- THEN the device is shown with a stale badge

#### Scenario: Device never connected
- WHEN a device has no last seen timestamp
- THEN the device is shown with a never-connected badge

#### Scenario: Page refreshes automatically
- WHEN the devices page is open
- THEN the liveness badges and status are refreshed automatically every 15 seconds

### Requirement: Frames served counter
The server SHALL track a persisted frames-served counter per device, incremented per streamed frame and retained across restarts.
#### Scenario: Frame increments counter
- WHEN the feed loop streams a frame to a device over its WebSocket connection
- THEN the device's frames-served counter is incremented

#### Scenario: Counter survives restart
- WHEN the server restarts and a device reconnects
- THEN the frames-served counter retains its previous value

### Requirement: Device error tracking
Device streaming or render errors SHALL be recorded in the health registry under device:<id>, with recovery recorded on later success.
#### Scenario: Device render error recorded
- WHEN a device connection encounters a render or streaming error
- THEN the health registry records the error for `device:<id>`

#### Scenario: Device recovers
- WHEN a later frame for the same device renders successfully
- THEN the device health entry records a success and resets consecutive failures

