## ADDED Requirements

### Requirement: Shared visual tokens
The administration UI MUST define its colors, typography, spacing, borders, radii, focus treatment, breakpoints, and motion settings as project-owned shared tokens rather than relying on Bootstrap defaults or page-local magic values.

#### Scenario: Consistent component styling
- **WHEN** a user navigates between dashboard, settings, and datasource pages
- **THEN** panels, controls, headings, statuses, spacing, and focus states use the same visual token system

#### Scenario: Reduced motion
- **WHEN** the browser advertises `prefers-reduced-motion: reduce`
- **THEN** decorative animation and non-essential transitions are disabled or reduced without hiding content or controls

### Requirement: CRT Control Room presentation
The UI MUST present a dark control-room visual language with restrained matrix/CRT texture, phosphor green and amber semantic accents, readable body text, and monospace telemetry, while preserving sufficient contrast for all text and controls.

#### Scenario: Normal administration view
- **WHEN** a user opens any administration page outside E-Ink mode
- **THEN** the page renders the CRT Control Room shell, surface hierarchy, accent system, and readable content styling

#### Scenario: E-Ink presentation
- **WHEN** E-Ink mode is enabled
- **THEN** decorative gradients, scanlines, shadows, animation, and color-only status cues are removed or simplified into high-contrast static presentation

### Requirement: Responsive application shell
The shared shell MUST provide semantic navigation, a usable content region, and a responsive navigation mode that remains operable by keyboard and touch on narrow viewports.

#### Scenario: Desktop navigation
- **WHEN** the viewport is desktop width
- **THEN** primary navigation is visible with grouped labels/icons, the active route is distinguishable without color alone, and the main content has a stable readable width

#### Scenario: Mobile navigation
- **WHEN** the viewport is narrow
- **THEN** navigation collapses into an accessible toggle/drawer, the content does not overflow horizontally, and all routes remain reachable

#### Scenario: Keyboard navigation
- **WHEN** a keyboard user tabs through the shell
- **THEN** focus is visible, focus order follows the visual/semantic order, and the navigation toggle can be opened and closed without a pointer

### Requirement: Reusable states and controls
Shared buttons, links, inputs, selects, tables, panels, badges, alerts, loading states, empty states, and error states MUST have consistent semantics, focus styling, disabled styling, and accessible names.

#### Scenario: Invalid form field
- **WHEN** a server-rendered form returns a validation error
- **THEN** the invalid field and error message are visually associated and understandable without relying on color alone

#### Scenario: Empty data collection
- **WHEN** a page has no configured records
- **THEN** it shows a clear empty state with an explanation and the relevant next action, rather than a blank region

### Requirement: Owned frontend assets
The UI MUST load compiled, version-controlled frontend assets from the application’s static asset path and MUST NOT require Bootstrap or other CDN CSS/JS for core rendering or interaction.

#### Scenario: Offline or restricted network
- **WHEN** the application is loaded without access to external CDNs
- **THEN** the shell, styles, icons, forms, and core interactions still render and operate

### Requirement: LEDit branding assets
The project MUST maintain one LED matrix-inspired source mark and generate the favicon, browser touch icon, and PWA icon sizes from that source through a reproducible build command.

#### Scenario: Browser metadata
- **WHEN** a browser loads the application
- **THEN** the document title, favicon, touch icon, manifest, and theme color reference valid LEDit-owned assets

#### Scenario: Small icon size
- **WHEN** the mark is rendered at favicon size
- **THEN** its silhouette remains recognizable and does not depend on tiny text
