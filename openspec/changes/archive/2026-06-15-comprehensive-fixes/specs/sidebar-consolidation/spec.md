## ADDED Requirements

### Requirement: All pages use shared sidebar partial

The `base.html` and `index.html` templates SHALL use `{{template "sidebar" .}}` instead of their current inline sidebar HTML.

#### Scenario: base.html uses sidebar partial

- **WHEN** rendering `base.html`
- **THEN** the sidebar SHALL be rendered via `{{template "sidebar" .}}` from the existing `sidebar.html` partial

#### Scenario: index.html uses sidebar partial

- **WHEN** rendering `index.html`
- **THEN** the sidebar SHALL be rendered via `{{template "sidebar" .}}` from the existing `sidebar.html` partial

### Requirement: Active page highlighting

The sidebar SHALL visually indicate which page is currently active.

#### Scenario: Dashboard sidebar link is active

- **WHEN** the current page is `/admin/`
- **THEN** the "Dashboard" link in the sidebar SHALL have Bootstrap's `active` class applied

#### Scenario: Settings sidebar link is active

- **WHEN** the current page is `/admin/settings`
- **THEN** the "Settings" link in the sidebar SHALL have Bootstrap's `active` class applied

### Requirement: Sidebar collapsible on mobile

The sidebar SHALL collapse off-screen on small viewports (Bootstrap `sm` breakpoint and below) with a hamburger toggle button.

#### Scenario: Mobile viewport shows hamburger

- **WHEN** the viewport width is below 576px (Bootstrap `sm` breakpoint)
- **THEN** the sidebar SHALL be hidden off-screen AND a hamburger menu button SHALL be visible

#### Scenario: Hamburger toggle shows sidebar

- **WHEN** the hamburger button is clicked on a mobile viewport
- **THEN** the sidebar SHALL slide in from the left using Bootstrap off-canvas behavior
