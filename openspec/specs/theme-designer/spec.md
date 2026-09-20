# theme-designer Specification

## Purpose
TBD - created by archiving change add-theme-designer. Update Purpose after archive.
## Requirements
### Requirement: Named theme model and validation
The system SHALL represent a theme as a named record with tokens `background_color`, `accent_color`, `text_color`, `title`, and `font_size`, SHALL require names to be non-empty and unique, and SHALL reject invalid token values with a field-level validation error at the point of save.

#### Scenario: Save a valid theme
- **WHEN** an administrator saves a theme named "Midnight" with three valid `#rrggbb` colors and a font size within the allowed range
- **THEN** the theme is stored and appears in the theme list

#### Scenario: Reject an invalid color
- **WHEN** an administrator submits a color that is not a valid `#rrggbb` hex value
- **THEN** the form redisplays the theme with an error associated with that color field and the theme is not saved

#### Scenario: Reject a duplicate name
- **WHEN** an administrator saves a theme whose name matches an existing theme
- **THEN** the form reports a name conflict and the existing theme is unchanged

#### Scenario: Reject an out-of-range font size
- **WHEN** an administrator submits a font size outside the supported bounds
- **THEN** the form reports the font size as invalid and the theme is not saved

### Requirement: Built-in themes
The system SHALL seed the existing presets (`cyber`, `f1`, `untappd`) as built-in themes available without configuration, SHALL mark exactly one theme as the global default at all times, and SHALL NOT allow built-in themes to be edited or deleted.

#### Scenario: Built-ins available on a fresh install
- **WHEN** the application starts with no stored themes
- **THEN** the built-in presets exist and one of them is the global default

#### Scenario: Built-ins are immutable
- **WHEN** an administrator attempts to edit or delete a built-in theme
- **THEN** the system refuses the change and offers duplicating it into an editable copy instead

#### Scenario: Default is always set
- **WHEN** the current default theme is deleted
- **THEN** another theme becomes the global default in the same operation

### Requirement: Theme lifecycle management
The system SHALL let an administrator create, duplicate, rename, edit, delete, and set the global default for custom themes from the admin UI.

#### Scenario: Duplicate a theme
- **WHEN** an administrator duplicates an existing theme
- **THEN** an editable copy is created with all tokens copied and a non-conflicting name

#### Scenario: Set the default theme
- **WHEN** an administrator marks a custom theme as the global default
- **THEN** that theme becomes the default and no other theme is marked default

#### Scenario: Delete a custom theme in use
- **WHEN** an administrator deletes a custom theme that is assigned to a datasource
- **THEN** the assignment is cleared and the affected datasource falls back to the effective default

### Requirement: Theme editor with live preview
The system SHALL provide a theme editor with color pickers and token controls that renders a live preview of a selected datasource (or matrix layout) through the unsaved theme, updating after edits stop rather than on every keystroke.

#### Scenario: Unsaved values are previewed
- **WHEN** an administrator changes an accent color in the theme editor without saving
- **THEN** the preview image updates to show the selected source rendered with the new accent color

#### Scenario: Preview uses real data
- **WHEN** an administrator selects a configured datasource in the theme editor
- **THEN** the preview shows that datasource's real data rendered with the edited theme, matching the feed's render path

#### Scenario: Preview is debounced
- **WHEN** an administrator drags a color picker continuously
- **THEN** the system issues preview requests only after editing pauses, not one per intermediate value

#### Scenario: Preview failure does not block editing
- **WHEN** the selected datasource's upstream API fails during a theme preview
- **THEN** the editor shows the datasource's fallback/placeholder render and the theme controls remain usable

### Requirement: Effective theme resolution
The system SHALL resolve the theme applied to a source as datasource override, else global default, else the built-in default, and SHALL apply the same resolution to both the live feed and on-demand previews. Themes SHALL be applied to sources that support themed rendering (including clock, system stats, compositor regions, and matrix cells); sources without themed rendering SHALL continue to render with their own palette.

#### Scenario: Datasource override wins
- **WHEN** a themeable datasource has a theme assigned and a different theme is the global default
- **THEN** the feed and previews render that datasource with its assigned theme

#### Scenario: Fall back to the global default
- **WHEN** a themeable datasource has no theme assigned
- **THEN** the feed and previews render it with the global default theme

#### Scenario: Fall back to the built-in default
- **WHEN** no global default theme is available
- **THEN** the feed and previews render with the built-in default theme

#### Scenario: Assign and clear an override
- **WHEN** an administrator assigns a theme to a datasource and later clears the assignment
- **THEN** the datasource renders with the assigned theme while set and with the global default after clearing

#### Scenario: Source without themed rendering
- **WHEN** the effective theme resolves for a source that does not support themed rendering
- **THEN** the source renders with its own palette and the feed and previews remain unchanged

### Requirement: Legacy theme import
The system SHALL import an existing `GeneralSettings.theme` value as a custom theme on first run when no themes have been stored, preserving the configured colors, and SHALL NOT overwrite existing themes on subsequent starts.

#### Scenario: Existing custom theme is preserved
- **WHEN** the application starts with a non-empty legacy theme value and no stored themes
- **THEN** a custom theme is created from those values and becomes the global default

#### Scenario: Import does not repeat
- **WHEN** the application restarts after the import has already run
- **THEN** no additional theme is created and existing themes are unchanged

#### Scenario: Empty legacy value
- **WHEN** the legacy theme value is empty or unparseable
- **THEN** no custom theme is created and the built-in default remains in effect

