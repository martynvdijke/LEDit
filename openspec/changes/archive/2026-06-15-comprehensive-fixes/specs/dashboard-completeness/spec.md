## ADDED Requirements

### Requirement: All 14 datasource types shown in dashboard

The admin dashboard SHALL display stat cards and table entries for all 14 datasource types.

#### Scenario: Missing types have stat cards

- **WHEN** viewing the admin dashboard
- **THEN** stat cards SHALL be displayed for RSS Feed, Calendar, Stock, and Text Slides datasource types in addition to the 10 currently shown

#### Scenario: Missing types appear in source table

- **WHEN** viewing the admin dashboard datasource table
- **THEN** records for Stock, RSS Feed, and Calendar datasources SHALL appear in the table with correct Edit/Delete actions

#### Scenario: Text Slides appear in source table

- **WHEN** viewing the admin dashboard datasource table
- **THEN** Text Slide records SHALL appear with their Content and Color fields displayed
