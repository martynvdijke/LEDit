## ADDED Requirements

### Requirement: Email settings management
The system SHALL provide an admin page to configure SMTP email settings.

#### Scenario: Email settings page
- **WHEN** an admin navigates to `/admin/settings/email`
- **THEN** the system SHALL render a form with fields: SMTP host, port, username, password, from address, and TLS toggle

#### Scenario: Save email settings
- **WHEN** an admin fills in the email settings form and submits
- **THEN** the system SHALL save the settings, log the operation via the central logging system, and redirect to the settings page

#### Scenario: Test email
- **WHEN** an admin clicks "Test Email" on the email settings page
- **THEN** the system SHALL attempt to send a test email using the configured settings and log the result (success/failure) via the central logging system

#### Scenario: View existing email settings
- **WHEN** an admin navigates to the email settings page and settings exist
- **THEN** the form SHALL be pre-populated with the saved values (password masked)

### Requirement: Email settings logging
All email-related operations SHALL be logged through the central logging system with source="email-settings".

#### Scenario: Log email save
- **WHEN** email settings are saved
- **THEN** a log entry SHALL be created at info level with source "email-settings" and message containing "Email settings saved"

#### Scenario: Log test email
- **WHEN** a test email is sent
- **THEN** a log entry SHALL be created at info level with source "email-settings" and the result

#### Scenario: Log test email failure
- **WHEN** a test email fails to send
- **THEN** a log entry SHALL be created at error level with source "email-settings" and the error details
