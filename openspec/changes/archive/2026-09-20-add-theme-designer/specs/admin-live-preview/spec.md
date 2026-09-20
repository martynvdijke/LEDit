## ADDED Requirements

### Requirement: Live preview on the theme editor
The system SHALL render a live preview from the theme editor using the existing preview endpoint family, so that an unsaved theme plus a selected source produces a preview image, and SHALL inherit the existing preview authentication, source-restriction, and feed-isolation requirements.

#### Scenario: Preview unsaved theme values
- **WHEN** an administrator edits theme tokens in the theme editor
- **THEN** the preview endpoint returns a PNG of the selected source rendered with those unsaved tokens

#### Scenario: Theme preview respects authentication
- **WHEN** an unauthenticated request hits a theme preview
- **THEN** the request is rejected with the standard admin authentication redirect/error

#### Scenario: Theme preview does not affect the feed
- **WHEN** an administrator generates theme previews while the feed is streaming
- **THEN** the feed continues streaming unchanged and theme previews do not appear in display analytics
