## ADDED Requirements

### Requirement: AI settings management
The system SHALL provide an admin page to configure AI/LLM integration settings.

#### Scenario: AI settings page
- **WHEN** an admin navigates to `/admin/settings/ai`
- **THEN** the system SHALL render a form with fields: provider (dropdown: openai, ollama, anthropic), API key, model name, and endpoint URL

#### Scenario: Save AI settings
- **WHEN** an admin fills in the AI settings form and submits
- **THEN** the system SHALL save the settings, log the operation via the central logging system, and redirect to the settings page

#### Scenario: Test AI connection
- **WHEN** an admin clicks "Test Connection" on the AI settings page
- **THEN** the system SHALL attempt to connect to the AI provider using the configured settings and log the result (success/failure) via the central logging system

#### Scenario: View existing AI settings
- **WHEN** an admin navigates to the AI settings page and settings exist
- **THEN** the form SHALL be pre-populated with the saved values (API key masked)

### Requirement: AI settings logging
All AI-related operations SHALL be logged through the central logging system with source="ai-settings".

#### Scenario: Log AI save
- **WHEN** AI settings are saved
- **THEN** a log entry SHALL be created at info level with source "ai-settings" and message containing "AI settings saved"

#### Scenario: Log test connection
- **WHEN** a test connection is performed
- **THEN** a log entry SHALL be created at info level with source "ai-settings" and the result

#### Scenario: Log test connection failure
- **WHEN** a test connection fails
- **THEN** a log entry SHALL be created at error level with source "ai-settings" and the error details
