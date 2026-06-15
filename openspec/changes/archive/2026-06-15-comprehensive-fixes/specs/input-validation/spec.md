## ADDED Requirements

### Requirement: URL field validation

The system SHALL validate that URL form fields contain a valid URL format before saving.

#### Scenario: Invalid URL rejected

- **WHEN** a datasource form is submitted with an invalid URL (e.g., "not-a-url")
- **THEN** the system SHALL NOT save the record AND SHALL display a validation error flash message

#### Scenario: Valid URL accepted

- **WHEN** a datasource form is submitted with a valid HTTP or HTTPS URL
- **THEN** the system SHALL save the record

### Requirement: Token field length validation

The system SHALL validate that API token fields are non-empty and within reasonable length limits.

#### Scenario: Empty token rejected

- **WHEN** a datasource form is submitted with an empty token field
- **THEN** the system SHALL NOT save the record AND SHALL display a validation error

### Requirement: Numeric field range validation

The system SHALL validate that numeric fields (timeout, width, height, port, font size) are within acceptable ranges.

#### Scenario: Invalid timeout rejected

- **WHEN** a settings form is submitted with a timeout of 0 or negative
- **THEN** the system SHALL NOT save AND SHALL display a validation error

#### Scenario: Invalid matrix dimensions rejected

- **WHEN** a settings or device form is submitted with width or height outside 1-1024
- **THEN** the system SHALL NOT save AND SHALL display a validation error

#### Scenario: Invalid port rejected

- **WHEN** a device form is submitted with port outside 1-65535
- **THEN** the system SHALL NOT save AND SHALL display a validation error

### Requirement: Required field enforcement

The system SHALL ensure all required form fields are present before saving.

#### Scenario: Missing required field on text slide

- **WHEN** a text slide form is submitted with empty Content field
- **THEN** the system SHALL NOT save AND SHALL display "Content is required"
