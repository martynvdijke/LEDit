## ADDED Requirements

### Requirement: E-ink toggle button in sidebar
The admin sidebar SHALL include an "E-Ink Mode" toggle button visible on all admin pages.

#### Scenario: Toggle button displayed in sidebar
- **WHEN** any admin page is rendered
- **THEN** the sidebar SHALL contain an "E-Ink Mode" toggle button positioned above the "Add Datasource" dropdown

#### Scenario: Toggle reflects current state
- **WHEN** e-ink mode is active (cookie `ledit_eink=true`)
- **THEN** the toggle SHALL show "E-Ink Mode: On" with active/highlighted styling
- **WHEN** e-ink mode is inactive
- **THEN** the toggle SHALL show "E-Ink Mode: Off" with default styling

### Requirement: Toggle endpoint
The system SHALL provide a server endpoint to toggle e-ink mode and set/clear the preference cookie.

#### Scenario: POST to toggle enables e-ink mode
- **WHEN** user clicks the e-ink toggle or sends `POST /admin/eink/toggle`
- **THEN** if e-ink mode was off, set cookie `ledit_eink=true` with 1-year expiry and redirect back to the referring page

#### Scenario: POST to toggle disables e-ink mode
- **WHEN** user clicks the e-ink toggle or sends `POST /admin/eink/toggle`
- **THEN** if e-ink mode was on, clear cookie `ledit_eink` and redirect back to the referring page

### Requirement: E-ink toggle on live feed page
The live feed page SHALL also provide an e-ink mode toggle, accessible without admin authentication.

#### Scenario: Toggle on feed page header
- **WHEN** the live feed page (`/`) is rendered
- **THEN** a toggle button SHALL be present in the feed controls area (alongside Pause/Skip/Fullscreen)

#### Scenario: Feed toggle uses same cookie
- **WHEN** user clicks the e-ink toggle on the feed page
- **THEN** the same `ledit_eink` cookie SHALL be set/cleared and the page SHALL reload to apply the change

### Requirement: Server-side default setting
The system SHALL support a server-side default e-ink mode setting in the admin Settings page.

#### Scenario: Default rendered on first visit
- **WHEN** a user visits any page with no `ledit_eink` cookie
- **THEN** the system SHALL check for a server-side default e-ink setting; if enabled, behave as if e-ink mode is active

#### Scenario: Cookie overrides server default
- **WHEN** a user has `ledit_eink` cookie set
- **THEN** the cookie value SHALL take precedence over the server-side default

### Requirement: Instant client-side toggle feedback
The toggle SHALL provide immediate visual feedback without requiring a full page load.

#### Scenario: JS adds .eink-mode class on toggle click
- **WHEN** user clicks the e-ink toggle button
- **THEN** JavaScript SHALL immediately add/remove `.eink-mode` class on `<body>` before the server redirect completes, providing instant visual feedback

### Requirement: Middleware injects e-ink state into templates
The Go server SHALL inject e-ink mode state into all template contexts.

#### Scenario: EInkMode available in template data
- **WHEN** any page template is rendered
- **THEN** the template context SHALL include `.eink_mode` boolean reflecting the current e-ink state (cookie or server default)

#### Scenario: Template uses eink_mode for conditional includes
- **WHEN** `.eink_mode` is true in template context
- **THEN** the `<body>` tag SHALL include the `.eink-mode` class and the `eink.css` stylesheet SHALL be linked
