## ADDED Requirements

### Requirement: Remove Python Django server code
The system SHALL remove all files and directories that belong to the deprecated Python Django application, after audit confirms no feature gaps.

#### Scenario: Django project directory removed
- **WHEN** the `server-py/` directory is deleted
- **THEN** no `.py` file SHALL remain at `server-py/` or any subdirectory

#### Scenario: Build unaffected by removal
- **WHEN** running `go build ./...` after removal
- **THEN** the build SHALL succeed with zero errors

#### Scenario: Tests unaffected by removal
- **WHEN** running `go test ./...` after removal
- **THEN** all tests SHALL pass

### Requirement: Remove orphaned Python client
The system SHALL remove the empty Python client package under `client/` that is not referenced by any Go code.

#### Scenario: Client directory removed
- **WHEN** the `client/` directory is deleted
- **THEN** the Go build SHALL still succeed

### Requirement: Remove empty Go scaffold directories
The system SHALL remove Go directories that are empty and serve no purpose (scaffolded but never populated).

#### Scenario: Empty directories removed
- **WHEN** `middleware/` and `models/` are empty
- **THEN** they SHALL be deleted
- **AND** the Go build SHALL still succeed

### Requirement: Verify no stale references remain
The system SHALL ensure no configuration files, documentation, or workflows reference the removed Python code.

#### Scenario: No server-py references in non-Python files
- **WHEN** searching all non-Python project files (excluding `.git`, `node_modules`, `openspec/`)
- **THEN** there SHALL be zero references to `server-py`, `manage.py`, or `django`
