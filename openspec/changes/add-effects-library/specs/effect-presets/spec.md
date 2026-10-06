# Spec: effect-presets

## ADDED Requirements

### Requirement: Effect preset model and validation

The system SHALL persist named effect presets as first-class records containing a unique non-empty name, the effect to run, a validated versioned parameter document, an optional built-in flag, and creation/update timestamps. Saving a preset SHALL validate the effect against the effect registry and the parameters against that effect's declared bounds, rejecting unknown effects, duplicate names, and invalid parameter values with a field-level error at the point of save. A preset SHALL store behavior only — never resolved theme colors — so retheming an installation updates every preset.

#### Scenario: Save a valid preset

- **WHEN** an administrator saves a preset named "Ember" for the `fire` effect with in-range parameters
- **THEN** the preset SHALL be stored and SHALL appear in the preset list and source pickers

#### Scenario: Unknown effect rejected

- **WHEN** a preset names an effect that the registry does not provide
- **THEN** the save SHALL fail with an effect-field error and the preset SHALL NOT be stored

#### Scenario: Duplicate name rejected

- **WHEN** a preset is saved with a name that matches an existing preset
- **THEN** the form SHALL report a name conflict and the existing preset SHALL be unchanged

#### Scenario: Invalid parameters rejected

- **WHEN** a preset supplies out-of-range speed/intensity/density or an invalid direction
- **THEN** the save SHALL fail with a field-level validation error and the preset SHALL NOT be stored

#### Scenario: Preset stores behavior, not colors

- **WHEN** a preset is stored and the global theme is later changed
- **THEN** the preset's stored parameters SHALL be unchanged and its rendered output SHALL use the new effective theme palette

### Requirement: Built-in presets and immutability

The system SHALL seed a small set of built-in effect presets on first run when no presets exist, covering the built-in effect set, and SHALL mark them as built-in. Built-in presets SHALL NOT be editable or deletable, SHALL be duplicable into editable copies with all parameters preserved and a non-conflicting name, and SHALL NOT be re-seeded or overwritten on subsequent starts.

#### Scenario: Built-ins available on a fresh install

- **WHEN** the application starts with no stored effect presets
- **THEN** built-in presets for the built-in effects SHALL exist and be selectable as sources

#### Scenario: Built-ins are immutable

- **WHEN** an administrator attempts to edit or delete a built-in preset
- **THEN** the system SHALL refuse the change and offer duplicating it into an editable copy instead

#### Scenario: Duplicate copies parameters

- **WHEN** an administrator duplicates a preset
- **THEN** an editable copy SHALL be created with the same effect and parameters and a non-conflicting name

#### Scenario: Seeding does not repeat

- **WHEN** the application restarts after built-in presets have been seeded
- **THEN** no additional preset SHALL be created and existing presets SHALL be unchanged

### Requirement: Admin preset management and picker integration

The system SHALL provide session-authenticated admin pages to list, create, edit, duplicate, and delete effect presets, with a form populated from the effect registry's names, defaults, and bounds. Presets SHALL appear as a selectable group in the existing source, playlist, matrix-cell, composition, and schedule pickers so a preset can be bound anywhere a source can. All preset endpoints and pages SHALL require the standard admin session and SHALL reject unauthenticated access.

#### Scenario: Administrator creates a preset from the admin UI

- **WHEN** an authenticated administrator submits the create form with a valid name, effect, and parameters
- **THEN** the preset SHALL appear in the list and in pickers without a restart

#### Scenario: Picker lists presets

- **WHEN** an administrator opens any source picker
- **THEN** saved effect presets SHALL be listed as a group with their names

#### Scenario: Delete removes the preset

- **WHEN** an administrator deletes a non-built-in preset
- **THEN** it SHALL disappear from the list and pickers, and existing references SHALL degrade through the unresolved-source tolerance

#### Scenario: Unauthenticated access rejected

- **WHEN** an unauthenticated request hits an effect preset page or endpoint
- **THEN** the request SHALL be rejected with the standard admin authentication redirect/error

### Requirement: Effect live preview

The admin effect form SHALL render a live preview of the effect with the current unsaved form values through the existing preview endpoint family and its authentication, source-restriction, and feed-isolation rules. Preview updates SHALL be debounced so continuous slider movement does not issue one request per intermediate value, SHALL honor the selected or effective theme (including unsaved theme tokens where the preview carries them), and SHALL NOT block editing when a preview render fails.

#### Scenario: Unsaved values are previewed

- **WHEN** an administrator changes speed, intensity, density, direction, or seed without saving
- **THEN** the preview image SHALL update to show the effect with those unsaved values

#### Scenario: Preview is debounced

- **WHEN** an administrator drags a parameter slider continuously
- **THEN** the system SHALL issue preview requests only after editing pauses, not one per intermediate value

#### Scenario: Preview respects authentication

- **WHEN** an unauthenticated request hits an effect preview
- **THEN** the request SHALL be rejected with the standard admin authentication redirect/error

#### Scenario: Preview does not affect the feed

- **WHEN** previews are generated while the feed is streaming
- **THEN** the feed SHALL continue unchanged and preview renders SHALL NOT appear in display analytics

#### Scenario: Preview failure does not block editing

- **WHEN** a preview render fails
- **THEN** the form SHALL remain usable and the error SHALL NOT discard the administrator's unsaved values

### Requirement: Preset assignment and resolution

Presets SHALL be referenceable wherever an effect can be used, including effect sources (`effect:<preset id>`), scene actions, composition/matrix bindings, and the overlay strip. Resolving a reference SHALL read the preset's current parameters at render time, so editing a preset SHALL take effect on the next rendered frame without a cache purge, and deleting a referenced preset SHALL leave the feed running through the existing unresolved-source tolerance. One preset SHALL be safely reusable across multiple surfaces and devices at the same time.

#### Scenario: Parameter edit takes effect next frame

- **WHEN** an administrator edits a preset's parameters while it is live on a device
- **THEN** the next frame rendered for that reference SHALL use the edited parameters

#### Scenario: One preset, many surfaces

- **WHEN** the same preset is bound to a matrix cell, a playlist slot, and an overlay strip
- **THEN** all three SHALL render that preset with the same effect and parameters

#### Scenario: Deleted preset leaves the feed healthy

- **WHEN** a preset referenced by a device is deleted
- **THEN** the device feed SHALL continue cycling through the unresolved-source tolerance and SHALL NOT disconnect or blank

#### Scenario: Overlay reference resolves with preview parity

- **WHEN** an overlay strip references a preset
- **THEN** the live device frame and the device-accurate preview SHALL composite the same effect frame at the same instant
