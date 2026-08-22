# google-calendar-datasource Specification

## Purpose
TBD - created by archiving change matrix-dashboard-rendering. Update Purpose after archive.
## Requirements
### Requirement: Google Calendar source configuration
The system SHALL allow an administrator to create, edit, and delete a Google Calendar datasource configured with a name and an iCal feed URL, including Google Calendar private iCal URLs of the form `https://calendar.google.com/calendar/ical/<calendarId>/private-<hash>/basic.ics`.

#### Scenario: Create a Google Calendar source
- **WHEN** an administrator saves a Google Calendar source named "Family" with a Google private iCal URL
- **THEN** the source is persisted and appears in the datasource list

#### Scenario: Edit a Google Calendar source
- **WHEN** an administrator updates an existing Google Calendar source's URL
- **THEN** the new URL is used for subsequent renders

### Requirement: Fetch and parse Google Calendar events
The system SHALL fetch the configured iCal feed using the shared HTTP client with a 10-second timeout and SHALL parse events using the existing iCal parser, treating the feed as an unauthenticated GET (the private URL carries its own authorization).

#### Scenario: Parse events from a valid feed
- **WHEN** the configured feed returns valid iCal data
- **THEN** the system parses the upcoming events and uses them for rendering

#### Scenario: Feed fetch fails
- **WHEN** the configured feed is unreachable or returns a non-2xx status
- **THEN** the system renders the fallback calendar placeholder and logs a warning

### Requirement: Render Google Calendar events
The system SHALL render the source with the title "GOOGLE CAL" (or the configured source name) and up to 4 upcoming events, truncating event text that exceeds the renderable width.

#### Scenario: Render with events
- **WHEN** a Google Calendar source renders and parsed events exist
- **THEN** the output shows the source title and up to 4 events, with over-long event text truncated

#### Scenario: Render with no events
- **WHEN** a Google Calendar source renders and the feed contains no upcoming events
- **THEN** the system renders the fallback calendar placeholder

