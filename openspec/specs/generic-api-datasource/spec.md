# generic-api-datasource Specification

## Purpose
TBD - created by archiving change matrix-dashboard-rendering. Update Purpose after archive.
## Requirements
### Requirement: Generic API source configuration
The system SHALL allow an administrator to create, edit, and delete a generic API datasource configured with a name, a JSON API URL, an optional token, optional additional HTTP headers, an optional title, and a set of row mappings where each mapping has a label and a dot-notation path into the JSON response.

#### Scenario: Create a generic API source
- **WHEN** an administrator saves a generic API source named "Pi-hole" with a URL, a token, and two row mappings
- **THEN** the source is persisted and appears in the datasource list

#### Scenario: Edit configuration
- **WHEN** an administrator updates a generic API source's URL or row mappings
- **THEN** the new configuration is used for subsequent renders

#### Scenario: Reject missing URL
- **WHEN** an administrator saves a generic API source without a URL
- **THEN** the save is rejected with a validation error

### Requirement: Authenticated JSON fetch
The system SHALL fetch the configured URL with the shared HTTP client and a 10-second timeout, sending the token as an `X-API-Key` header when the token is present, plus any additional configured headers. The response SHALL be parsed as JSON.

#### Scenario: Fetch with token header
- **WHEN** a generic API source with a token renders
- **THEN** the request includes the token in the `X-API-Key` header and the JSON response is parsed

#### Scenario: Fetch failure
- **WHEN** the configured URL is unreachable or returns a non-2xx status or non-JSON content
- **THEN** the system renders the fallback placeholder and logs a warning

### Requirement: Field extraction with dot paths
The system SHALL extract a scalar value for each configured mapping by resolving its dot-notation path against the parsed JSON, supporting numeric array indices (for example `data.btc.usd` or `items.0.name`).

#### Scenario: Extract mapped fields
- **WHEN** a generic API source renders and the response matches the configured paths
- **THEN** each mapping's label is rendered with the extracted value

#### Scenario: Path does not resolve
- **WHEN** a configured path does not resolve against the response
- **THEN** the mapping renders with a placeholder value and a warning is logged

### Requirement: Render generic API values
The system SHALL render the source with the configured title (or a default title) and the extracted label/value rows, truncating values that exceed the renderable width.

#### Scenario: Render extracted rows
- **WHEN** a generic API source renders with extracted values
- **THEN** the output shows the title and each configured label with its extracted value, truncated to fit

### Requirement: Admin test preview
The system SHALL provide a test/preview action in the generic API form that fetches the configured URL and shows the extracted rows before saving.

#### Scenario: Test a configuration
- **WHEN** an administrator clicks test on a filled generic API form
- **THEN** the system fetches the URL with the configured auth and displays the extracted rows or an error message

