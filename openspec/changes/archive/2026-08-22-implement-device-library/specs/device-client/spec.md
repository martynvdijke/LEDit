# device-client Specification

## ADDED Requirements

### Requirement: WebSocket connection with device token
The device library SHALL connect to the server's device stream endpoint using a device token that identifies the device.

#### Scenario: Successful connection
- **WHEN** the device library is started with a server URL and a valid device token
- **THEN** it SHALL open a WebSocket connection to `ws://<server>/ws/device/<token>`

#### Scenario: Invalid or missing token
- **WHEN** the server rejects the connection (unknown token or disabled device)
- **THEN** the device library SHALL log the failure and retry with backoff rather than exit

### Requirement: Frame rendering to the LED matrix
The device library SHALL decode received PNG frames and render them onto the configured LED matrix panel.

#### Scenario: Frame received
- **WHEN** the server sends a message containing a `format` of `PNG` and a base64 `image`
- **THEN** the library SHALL decode the PNG, convert it to a pixel buffer at the device's matrix dimensions, and write it to the panel

#### Scenario: Non-image message
- **WHEN** the server sends a message without a PNG image (for example a text notification)
- **THEN** the library SHALL ignore or surface it without corrupting the current matrix content

### Requirement: Matrix configuration
The device library SHALL configure the matrix panel from command-line/environment parameters for the physical setup.

#### Scenario: Panel configured
- **WHEN** the device library starts with parameters such as rows, columns, chain length, brightness, and GPIO slowdown
- **THEN** it SHALL initialize the rpi-rgb-led-matrix panel with those values

#### Scenario: Default sizing
- **WHEN** no explicit rows/columns are supplied
- **THEN** the library SHALL default to 64×64

### Requirement: Automatic reconnection
The device library SHALL automatically reconnect when the WebSocket connection drops or the server restarts.

#### Scenario: Connection lost
- **WHEN** the WebSocket connection is lost
- **THEN** the library SHALL retry the connection with increasing backoff until it succeeds

#### Scenario: Server restart
- **WHEN** the server becomes reachable again after a restart
- **THEN** the library SHALL reconnect and resume rendering frames without manual intervention

### Requirement: Minimal footprint with no rendering logic
The device library SHALL be a single small Python file that performs no content rendering itself; all rendering logic SHALL remain on the server.

#### Scenario: Single-file distribution
- **WHEN** the device library is deployed to a Pi Zero
- **THEN** it SHALL consist of one Python source file (plus documented dependencies) with no server-side rendering or configuration logic
