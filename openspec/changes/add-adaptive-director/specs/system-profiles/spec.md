# Spec: system-profiles

## ADDED Requirements

### Requirement: Profile model and members

The system SHALL provide named `SystemProfile` bundles, each optionally containing: a director playlist reference (`playlist_id`), profile-tagged event-rule membership (`DisplayRule.profile_id`), a brightness bundle (enabled flag, schedule windows, override level), a default theme reference (`theme_id`), an overlay bundle (enabled flag, position, height, text, speed, background, foreground) and zero or more profile schedule windows. Profile names SHALL be non-empty and unique. A seeded fallback profile (`is_fallback=true`) SHALL always exist, SHALL contain no members, and SHALL NOT be deletable. Event rules with no `profile_id` SHALL always be evaluated; rules tagged with a `profile_id` SHALL be evaluated only while that profile is active.

#### Scenario: Create a profile bundle

- **WHEN** an administrator creates a profile "Evening" with a playlist, a theme, an overlay and a brightness bundle
- **THEN** the system SHALL persist the profile and return it on the next load with identical member values

#### Scenario: Duplicate profile name rejected

- **WHEN** an administrator creates a profile whose name matches an existing profile
- **THEN** the server SHALL reject the save with a validation error and SHALL NOT create a second profile

#### Scenario: Fallback profile cannot be deleted

- **WHEN** an administrator attempts to delete the seeded fallback profile
- **THEN** the server SHALL reject the request and the fallback profile SHALL remain present and activatable

#### Scenario: Tagged rules switch with the profile

- **WHEN** profile "Party" has a tagged event rule and the active profile is "Party"
- **THEN** the evaluator SHALL evaluate that rule, and when another profile becomes active the rule SHALL no longer be evaluated until "Party" is active again

#### Scenario: Untagged rules always evaluate

- **WHEN** an event rule has no profile tag and any profile is active
- **THEN** the evaluator SHALL continue to evaluate the rule exactly as before profiles existed

### Requirement: Atomic validated activation with rollback

Profile activation SHALL be atomic: the system SHALL first load and validate the profile (enabled, members parse, referenced playlist/theme exist, at most 32 schedule windows), then commit the active-profile pointer together with any derived global mutations (default theme swap) in a single transaction, then make the switch visible to the feed. If validation fails, the server SHALL reject the request with per-member errors and the previously active profile SHALL remain active. Re-activating the active profile SHALL be idempotent. On startup the server SHALL re-apply the persisted active profile, falling back to the fallback profile when it is missing or invalid. Deleting or disabling the active profile SHALL activate the fallback profile.

#### Scenario: Valid profile activates atomically

- **WHEN** an administrator activates profile "Workday" whose members all resolve
- **THEN** the active-profile pointer SHALL change to "Workday", the profile's theme SHALL become the default theme, and the feed SHALL re-compose using the profile's members at its next boundary

#### Scenario: Invalid profile leaves prior state

- **WHEN** an administrator activates a profile referencing a playlist that does not exist
- **THEN** the server SHALL respond with a per-member error, no pointer or theme change SHALL be committed, and the previously active profile SHALL keep governing the wall

#### Scenario: Re-apply is idempotent

- **WHEN** the active profile is applied twice in a row with no configuration change in between
- **THEN** the resulting active pointer, default theme, and switch log SHALL be unchanged by the second apply except for the recorded attempt

#### Scenario: Startup re-applies the active profile

- **WHEN** the server restarts with a persisted valid active profile
- **THEN** the profile SHALL be re-applied and the wall SHALL render under that profile without operator action

#### Scenario: Deleting the active profile falls back

- **WHEN** an administrator deletes the currently active profile
- **THEN** the system SHALL activate the fallback profile and log the fallback reason

### Requirement: Inherited member resolution and never-blank fallback

Profile members SHALL apply only where device configuration is inherited. The director playlist SHALL apply only to devices whose effective content mode (device explicit, then group, then default) is `global`; devices in `playlist` or `scheduled` mode SHALL be unaffected. Profile brightness SHALL apply only when the device's effective brightness is not explicitly enabled; profile overlay SHALL apply only when the device's effective overlay is not explicitly enabled; the profile theme SHALL replace only the global default while per-source theme assignments continue to win. At feed composition, a profile director playlist that is disabled, missing, unparseable or empty SHALL be skipped with a warning and resolution SHALL continue down the fallback ladder (profile playlist → device global list → idle screensaver), keeping the current source list if nothing resolves. No profile member SHALL produce an empty source list.

#### Scenario: Global-mode device uses the director playlist

- **WHEN** the active profile has a director playlist and a device's effective content mode is `global`
- **THEN** the device feed SHALL cycle the director playlist's resolvable sources in authored order

#### Scenario: Playlist device keeps its saved playlist

- **WHEN** the active profile has a director playlist and a device's effective content mode is `playlist`
- **THEN** the device SHALL continue to play its own playlist in saved order and SHALL NOT use the director playlist

#### Scenario: Explicit device settings win

- **WHEN** a device has brightness explicitly enabled and an overlay explicitly enabled, and the active profile defines both
- **THEN** the device's own brightness and overlay SHALL be used and the profile values SHALL be ignored for that device

#### Scenario: Dangling director playlist falls back

- **WHEN** the active profile's director playlist has been deleted or contains no resolvable items
- **THEN** the feed SHALL log a warning and render the device's global source list instead of an empty rotation

#### Scenario: Nothing resolves keeps the wall alive

- **WHEN** the director playlist, the global source list and the idle screensaver all fail to resolve
- **THEN** the connection SHALL keep its current source list rather than emitting an empty rotation

### Requirement: Profiles never override feed tiers or suppress incidents and notifications

Profiles SHALL be a configuration layer and SHALL NOT introduce, clear, or reorder any feed tier. The precedence `notification > incident > alarm > scene > pinned rule > rotation` SHALL remain unchanged. Profile activation SHALL NOT pin a source, SHALL NOT dismiss or resolve an active incident, and SHALL NOT remove a pending or active notification. Scene overlays SHALL continue to replace the profile overlay while a scene holds.

#### Scenario: Active incident survives a profile switch

- **WHEN** a profile is activated while an incident is active
- **THEN** the incident SHALL remain active, SHALL keep rendering as the incident tier, and the switch SHALL be logged without clearing incident state

#### Scenario: Notification still interrupts

- **WHEN** a notification is added while a profile is active
- **THEN** the next slot SHALL emit the notification frame, after which profile-governed rotation SHALL resume

#### Scenario: Pinned rule outranks the director playlist

- **WHEN** an event rule is pinning a source and the profile also provides a director playlist
- **THEN** the pinned source SHALL be rendered and the director playlist SHALL NOT displace it

#### Scenario: Profile switch does not pin

- **WHEN** a profile without any rule activity is activated
- **THEN** no feed controller SHALL report a pinned key as a result of the activation

### Requirement: Time-of-day profile scheduling

Profiles SHALL support zero or more schedule windows using the existing window model (`days`, `start`, `end`, `priority`, time mode) and the existing matching semantics. A singleton profile-selection mode SHALL be `manual` (default) or `scheduled`. In `scheduled` mode the system SHALL resolve the active profile on the existing evaluator tick by highest matching window priority, breaking ties by profile order (lowest id first), and SHALL activate a new winner only when it differs from the active profile; when no window matches, the fallback profile SHALL be activated. In `manual` mode the scheduler SHALL NOT change the active profile; an operator activation while in `scheduled` mode SHALL switch selection to `manual`, and an explicit "resume schedule" action SHALL switch it back. Resolution SHALL use an injectable clock and server-local time and SHALL NOT introduce a new cron, ticker, or goroutine.

#### Scenario: Scheduled window activates a profile

- **WHEN** selection mode is `scheduled`, the time enters a window for "Workday" and the active profile is different
- **THEN** the system SHALL activate "Workday" within one evaluator tick and record the switch with reason `scheduled`

#### Scenario: No matching window falls back

- **WHEN** selection mode is `scheduled` and no profile window matches the current time
- **THEN** the fallback profile SHALL be activated

#### Scenario: Priority and order break ties

- **WHEN** two profiles have matching windows at the current time and one has the higher window priority
- **THEN** the higher-priority profile SHALL be active; equal priorities SHALL resolve to the lower profile id deterministically

#### Scenario: Manual activation wins over schedule

- **WHEN** selection mode is `scheduled`, a window is currently matching, and an administrator manually activates another profile
- **THEN** the manual profile SHALL stay active and selection mode SHALL become `manual` until "resume schedule" is invoked

#### Scenario: Resolution is pure and time-injectable

- **WHEN** the schedule resolver is called twice with the same clock value and profile set
- **THEN** it SHALL return the same profile and the same matched window

### Requirement: Profile switch logging and admin/API surface

Every profile activation, fallback, and scheduled switch SHALL be logged with the profile from/to, timestamp, reason (`manual|api|scheduled|fallback|rollback`), actor identity when known, and the active rule set. The system SHALL expose a session/API-token-authenticated `GET /api/profiles` list, an admin-only `POST /api/profiles/:id/activate`, a read-only `GET /api/profiles/resolve` debug endpoint returning the resolved profile, matched window, selection mode and next switch time, and admin CRUD pages at `/admin/profiles` following existing admin patterns. Mutations SHALL require admin; listing SHALL be available to viewers.

#### Scenario: Switch is auditable

- **WHEN** a profile is activated from the admin UI by an admin session
- **THEN** a switch record SHALL contain from, to, reason, actor, and timestamp and SHALL be visible in the admin history

#### Scenario: Viewer cannot activate

- **WHEN** a viewer session calls `POST /api/profiles/:id/activate`
- **THEN** the response SHALL be 403 and the active profile SHALL be unchanged

#### Scenario: Debug endpoint explains resolution

- **WHEN** an authenticated client calls `GET /api/profiles/resolve` at a given time
- **THEN** the response SHALL contain the resolved profile id, the matched window (when any), the selection mode, and the next switch time

#### Scenario: Manual rollback to default

- **WHEN** an administrator activates the fallback profile
- **THEN** all profile-provided members SHALL stop being applied while device/group configuration continues to render, and the switch SHALL be logged

### Requirement: Profile validation and caps

The system SHALL validate profiles server-side: names non-empty and unique; referenced playlists/themes must exist when set; brightness and overlay JSON must parse and use the same allowed ranges as the corresponding device fields; schedule windows SHALL reuse the existing validation (HH:MM times, non-empty day subsets, at most 32 windows); an override level SHALL be within 0-100. Invalid input SHALL be rejected with a descriptive error and SHALL NOT be persisted or activated.

#### Scenario: Invalid window rejected

- **WHEN** a profile is saved with a window whose start is not HH:MM
- **THEN** the server SHALL reject the save with a validation error

#### Scenario: Too many windows rejected

- **WHEN** a profile is saved with 33 schedule windows
- **THEN** the server SHALL reject the request

#### Scenario: Unknown theme rejected at activation

- **WHEN** a profile references a theme id that does not exist
- **THEN** activation SHALL fail with a per-member error and no global state SHALL change
