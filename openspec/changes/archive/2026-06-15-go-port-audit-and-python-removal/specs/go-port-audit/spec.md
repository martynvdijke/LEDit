## ADDED Requirements

### Requirement: Audit Python-to-Go feature parity
The system SHALL provide a documented comparison of every feature in the Python Django application and confirm its presence (or intentional absence) in the Go implementation.

#### Scenario: Datasource coverage verified
- **WHEN** enumerating all datasource models in Python (Sonarr, Radarr, F1, Weather, HomeAssistant, Untappd, Image, Video)
- **THEN** each SHALL have a corresponding Go ent schema and datasource implementation

#### Scenario: Theme coverage verified
- **WHEN** enumerating all Python themes (default, f1, untapped)
- **THEN** each SHALL have a corresponding Go theme in `render/themes/`

#### Scenario: WebSocket feed verified
- **WHEN** comparing Python `consumers.py` websocket implementation
- **THEN** the Go websocket handler SHALL cover the same feed loop pattern (datasource rotation, timeout, random shuffle)

#### Scenario: Device settings verified
- **WHEN** checking Python `submodels/device_settings.py`
- **THEN** the Go ent schema SHALL include a `DeviceSettings` model (or equivalent)

#### Scenario: Auth system verified
- **WHEN** checking Python Django admin/auth
- **THEN** the Go handlers SHALL include auth functionality

#### Scenario: Remaining gaps documented
- **WHEN** any Python feature lacks a Go equivalent
- **THEN** the gap SHALL be documented with a decision (planned implementation vs intentional omission)

### Requirement: Audit the Go codebase for empty/unused structures
The system SHALL identify and document all empty or unused directories and files in the Go project.

#### Scenario: Empty directories identified
- **WHEN** scanning the Go project directory structure
- **THEN** any empty Go package directories SHALL be listed

#### Scenario: Orphaned client code identified
- **WHEN** scanning non-Go code directories at the project root
- **THEN** any directories with only trivial or dead Python content SHALL be listed
