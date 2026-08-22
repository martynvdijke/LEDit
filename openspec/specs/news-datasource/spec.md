# news-datasource Specification

## Purpose
TBD - created by archiving change matrix-dashboard-rendering. Update Purpose after archive.
## Requirements
### Requirement: News source configuration
The system SHALL allow an administrator to create, edit, and delete a news datasource configured with a name and one or more RSS/Atom feed URLs. Multiple URLs SHALL be separated by commas in the URL field.

#### Scenario: Create a news source with multiple feeds
- **WHEN** an administrator saves a news source named "Tech News" with three comma-separated feed URLs
- **THEN** the source is persisted and appears in the datasource list

#### Scenario: Edit feed URLs
- **WHEN** an administrator updates a news source's feed URLs
- **THEN** the new URLs are used for subsequent renders

### Requirement: Aggregate headlines across feeds
The system SHALL fetch each configured feed using the shared HTTP client with a 10-second timeout, parse items with the existing RSS parser, and aggregate headlines across all feeds, de-duplicating by title and interleaving newest items first.

#### Scenario: Aggregate from multiple feeds
- **WHEN** a news source with two feeds renders and both feeds return items
- **THEN** the output contains headlines from both feeds with duplicates removed

#### Scenario: One feed fails
- **WHEN** a news source renders and one of its feeds fails to fetch while others succeed
- **THEN** the headlines from the successful feeds are rendered and the failure is logged as a warning

### Requirement: Render news headlines
The system SHALL render the source with the configured title and up to 4 headlines, each labeled with a short source tag, truncating headlines that exceed the renderable width.

#### Scenario: Render headlines with source tags
- **WHEN** a news source renders with aggregated headlines
- **THEN** the output shows the source title and up to 4 headlines, each with a source tag, truncated to fit

### Requirement: News fallback on total failure
The system SHALL render a static placeholder when all configured feeds fail or return no items.

#### Scenario: All feeds fail
- **WHEN** a news source renders and every configured feed is unreachable
- **THEN** the system renders the fallback placeholder and logs a warning

#### Scenario: All feeds empty
- **WHEN** a news source renders and every configured feed returns no items
- **THEN** the system renders the fallback placeholder

