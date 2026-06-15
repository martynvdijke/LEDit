## ADDED Requirements

### Requirement: Flash message system

The system SHALL provide a mechanism to display temporary success/error/info messages to the user after form submissions.

#### Scenario: Flash message after successful save

- **WHEN** a form is submitted successfully AND the user is redirected
- **THEN** the page SHALL display a green toast/alert with a success message at the top of the content area

#### Scenario: Flash message after error

- **WHEN** a form submission fails validation or a DB operation errors
- **THEN** the page SHALL display a red toast/alert with the error message

#### Scenario: Flash message disappears on next request

- **WHEN** a flash message is displayed AND the user navigates to another page
- **THEN** the flash message SHALL NOT persist across requests

### Requirement: Flash messages stored in session cookie

Flash messages SHALL be stored as JSON in the session cookie between the redirect and the next page render.

#### Scenario: Flash round-trip

- **WHEN** a handler calls `SetFlash(c, "success", "Saved!")` and redirects
- **THEN** the next page render SHALL include the flash message in the template context AND clear it from the session

### Requirement: All admin forms use flash messages

Every admin form handler that currently redirects without feedback SHALL set a flash message on success/error.

#### Scenario: Settings form flash

- **WHEN** general settings are saved
- **THEN** a success flash message "Settings saved" SHALL appear

#### Scenario: Datasource CRUD flash

- **WHEN** a datasource is created, updated, or deleted
- **THEN** an appropriate success flash message SHALL appear (e.g., "Sonarr source created")
