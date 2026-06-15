## ADDED Requirements

### Requirement: WebSocket origin validation

The `CheckOrigin` function in the WebSocket upgrader SHALL validate the request origin against a configured list of allowed origins instead of allowing all origins.

#### Scenario: Allowed origin accepted

- **WHEN** a WebSocket connection request has an Origin header matching a configured allowed origin
- **THEN** the upgrade SHALL proceed normally

#### Scenario: Unknown origin rejected

- **WHEN** a WebSocket connection request has an Origin header not in the allowed list
- **THEN** the upgrade SHALL be rejected with HTTP 403

#### Scenario: No origin allowed when unconfigured

- **WHEN** no allowed origins are configured
- **THEN** the system SHALL allow requests with no Origin header (for direct device connections) but reject requests with an unknown Origin header

### Requirement: Unique filenames for file uploads

Uploaded image and video files SHALL be saved with unique filenames to prevent collisions.

#### Scenario: File upload generates unique name

- **WHEN** an image or video file is uploaded
- **THEN** the file SHALL be saved with a unique filename (e.g., UUID + original extension) instead of the original filename

#### Scenario: Collision impossible

- **WHEN** two files with the same original name are uploaded
- **THEN** each SHALL be saved with a different unique filename AND neither SHALL overwrite the other

### Requirement: DB operation error propagation

The `updateTokenURLDS` and `deleteTokenURLDS` functions SHALL check and surface errors from database operations instead of silently ignoring them.

#### Scenario: Update error surfaced

- **WHEN** a datasource update fails
- **THEN** the system SHALL log the error AND display an error flash message to the user

#### Scenario: Delete error surfaced

- **WHEN** a datasource delete fails
- **THEN** the system SHALL log the error AND display an error flash message to the user
