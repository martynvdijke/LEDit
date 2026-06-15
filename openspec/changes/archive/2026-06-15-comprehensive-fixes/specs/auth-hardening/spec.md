## ADDED Requirements

### Requirement: Admin credentials stored in database

The system SHALL store admin credentials in a new `AdminSettings` Ent schema with bcrypt-hashed password storage.

#### Scenario: First-run bootstrap from env var

- **WHEN** the application starts with no `AdminSettings` record in the database AND `LEDIT_ADMIN_PASSWORD` env var is set
- **THEN** the system SHALL create an `AdminSettings` record with the env var value hashed via bcrypt AND the default username set to "admin"

#### Scenario: First-run with default password

- **WHEN** the application starts with no `AdminSettings` record AND `LEDIT_ADMIN_PASSWORD` env var is not set
- **THEN** the system SHALL create an `AdminSettings` record with "ledit" hashed via bcrypt AND log a warning that the default password is in use

#### Scenario: Existing admin settings on startup

- **WHEN** the application starts and an `AdminSettings` record already exists
- **THEN** the system SHALL use the existing record as-is

### Requirement: Authentication enabled by default

The admin panel SHOULD require authentication by default when admin settings exist.

#### Scenario: Unauthenticated request to admin

- **WHEN** a request is made to any `/admin/` route without a valid session cookie
- **THEN** the system SHALL redirect to `/login` with HTTP 302

#### Scenario: Disable auth via env var

- **WHEN** `LEDIT_AUTH_DISABLE=true` is set
- **THEN** the system SHALL skip auth middleware for all `/admin/` routes

### Requirement: Login with configured credentials

#### Scenario: Successful login

- **WHEN** a POST request to `/login` contains the correct username and password matching the stored `AdminSettings` record
- **THEN** the system SHALL create a session cookie valid for 24 hours AND redirect to `/admin/`

#### Scenario: Failed login

- **WHEN** a POST request to `/login` contains incorrect credentials
- **THEN** the system SHALL display an error message on the login page

### Requirement: Logout invalidates session

#### Scenario: Logout action

- **WHEN** a request is made to `/logout`
- **THEN** the system SHALL clear the session cookie AND redirect to `/login`

### Requirement: Password change via admin panel

#### Scenario: Admin changes password

- **WHEN** an authenticated admin submits new credentials via the admin settings page
- **THEN** the system SHALL hash the new password with bcrypt AND update the `AdminSettings` record
