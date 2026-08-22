## ADDED Requirements

### Requirement: Schedule field renamed to time_range

The `cron` field in the schedule schema, form, and templates SHALL be renamed to `time_range` to accurately reflect its actual usage as a time range (e.g., "08:00-12:00") rather than a cron expression.

#### Scenario: Schema field renamed

- **WHEN** inspecting the Schedule Ent schema
- **THEN** the field SHALL be named `time_range` instead of `cron`

#### Scenario: Form field renamed

- **WHEN** viewing the new/edit schedule form
- **THEN** the input label SHALL read "Time Range" and the input name attribute SHALL be `time_range`

#### Scenario: Migration of existing data

- **WHEN** the application starts after this change AND existing schedule records have data in the old `cron` field
- **THEN** the system SHALL migrate existing `cron` values to the new `time_range` field
