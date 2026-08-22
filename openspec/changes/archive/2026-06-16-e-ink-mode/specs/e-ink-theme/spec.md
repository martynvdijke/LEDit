## ADDED Requirements

### Requirement: High-contrast e-ink theme stylesheet
The system SHALL provide a CSS stylesheet (`eink.css`) that applies high-contrast styling when the `.eink-mode` class is present on the `<body>` element.

#### Scenario: E-ink stylesheet is loaded on all pages
- **WHEN** any page is rendered with `EInkMode` context set to true
- **THEN** the `<link rel="stylesheet">` for `eink.css` SHALL be included in the page `<head>`

#### Scenario: E-ink overrides are scoped to `.eink-mode` class
- **WHEN** `.eink-mode` class is present on `<body>`
- **THEN** all e-ink CSS rules SHALL activate; no rule SHALL apply without the `.eink-mode` ancestor

### Requirement: High-contrast color scheme
The e-ink theme SHALL replace Bootstrap's default color scheme with high-contrast alternatives.

#### Scenario: Text becomes black on white background
- **WHEN** `.eink-mode` is active
- **THEN** body background SHALL be white (#FFFFFF) and default text color SHALL be black (#000000)

#### Scenario: Colored Bootstrap cards become bordered
- **WHEN** `.eink-mode` is active on a page with `.card.text-white.bg-*` elements
- **THEN** those cards SHALL have white background, black text, and a left border in the original color as a subtle differentiator

#### Scenario: Bootstrap badges and alerts become bordered
- **WHEN** `.eink-mode` is active
- **THEN** `.badge` and `.alert` elements SHALL use high-contrast (black-on-white) styling with thin colored borders instead of filled backgrounds

### Requirement: All CSS animations and transitions disabled
The e-ink theme SHALL disable all CSS animations, transitions, and keyframe-based motion.

#### Scenario: Fade transitions suppressed
- **WHEN** `.eink-mode` is active
- **THEN** `.fade-out` and all `transition` properties SHALL be set to `none`

#### Scenario: Hover effects become static outlines
- **WHEN** `.eink-mode` is active and user hovers over a button or link
- **THEN** no background color change or scale animation SHALL occur; a thin solid outline MAY indicate focus

### Requirement: Larger touch targets
The e-ink theme SHALL increase interactive element sizing for imprecise e-ink input.

#### Scenario: Sidebar links have minimum height
- **WHEN** `.eink-mode` is active
- **THEN** all sidebar `.nav-link` elements SHALL have a minimum height of 48px and increased padding

#### Scenario: Buttons are enlarged
- **WHEN** `.eink-mode` is active
- **THEN** `.btn` elements SHALL have minimum 48px height and 16px horizontal padding

#### Scenario: Form controls are enlarged
- **WHEN** `.eink-mode` is active
- **THEN** `.form-control`, `.form-select`, and `.form-control-color` SHALL have minimum 48px height

### Requirement: Reduced visual noise
The e-ink theme SHALL reduce decorative elements for cleaner rendering on e-ink.

#### Scenario: Dividers and borders are simplified
- **WHEN** `.eink-mode` is active
- **THEN** `hr` elements SHALL use solid black borders; table borders SHALL be 1px solid black; sidebar border-right SHALL be 1px solid #CCC

#### Scenario: Dropdown menus have solid backgrounds
- **WHEN** `.eink-mode` is active
- **THEN** `.dropdown-menu` SHALL have solid white background with black 1px border and no shadow

### Requirement: Fullscreen feed page uses pure black background
The live feed fullscreen mode SHALL use pure black background for maximum contrast on e-ink.

#### Scenario: Fullscreen background in e-ink mode
- **WHEN** `.eink-mode` is active and `.fullscreen-active` is present
- **THEN** background SHALL be pure black (#000000) with no gradient or overlay effects
