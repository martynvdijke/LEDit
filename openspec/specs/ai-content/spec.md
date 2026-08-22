# ai-content Specification

## Purpose
TBD - created by archiving change ai-features. Update Purpose after archive.
## Requirements
### Requirement: AI text slide generation
The system SHALL provide an admin endpoint that generates text slide content using the configured LLM provider from `AISettings`.

#### Scenario: Generate slide content from prompt
- **WHEN** an authenticated admin submits a prompt to the slide generation endpoint
- **THEN** the system SHALL call the configured LLM provider's chat-completions API with the prompt and a system instruction to produce short LED-appropriate text
- **AND** the response SHALL be returned as JSON content for the slide form

#### Scenario: Generated content is not persisted automatically
- **WHEN** the generation endpoint returns generated content
- **THEN** the system SHALL NOT write the content to the database; the admin SHALL submit the slide form to save it

#### Scenario: LLM provider failure on generate
- **WHEN** the LLM provider returns an error, times out, or is unreachable during generation
- **THEN** the endpoint SHALL return an error response with a human-readable message
- **AND** the feed SHALL continue operating normally

### Requirement: AI news digest datasource
The system SHALL provide an `AIDigest` datasource that summarizes headlines from configured RSS/news feeds via the LLM and renders the digest on the display.

#### Scenario: Digest rendered from feeds
- **WHEN** an enabled AI digest is included in the feed and its cached digest is expired
- **THEN** the system SHALL fetch headlines from the digest's configured feeds, request a short digest from the LLM, and render it

#### Scenario: Digest cached for TTL
- **WHEN** a digest has been generated within its configured TTL
- **THEN** the system SHALL render the cached digest without calling the LLM again

#### Scenario: Concurrent generation is single-flight
- **WHEN** multiple render requests arrive while a digest is being generated
- **THEN** the system SHALL perform exactly one LLM generation and share its result

#### Scenario: LLM failure uses stale cache
- **WHEN** the LLM or feed fetch fails and a previously generated digest exists
- **THEN** the system SHALL render the stale cached digest instead of a blank placeholder

#### Scenario: No cache and LLM failure
- **WHEN** the LLM or feed fetch fails and no cached digest exists
- **THEN** the system SHALL render the standard fallback placeholder
- **AND** the feed SHALL continue cycling without interruption

#### Scenario: Manual digest refresh
- **WHEN** an admin triggers a manual refresh on the digest admin form
- **THEN** the system SHALL invalidate the cached digest and regenerate it on the next render

### Requirement: AIDigest entity management
The system SHALL provide full CRUD administration for AI digest datasources.

#### Scenario: Admin creates an AI digest
- **WHEN** an authenticated admin creates an AI digest with name, prompt, source feeds, TTL, and enabled state
- **THEN** the digest SHALL appear in the datasource list, dashboard counts, and feed source selection

#### Scenario: Admin disables an AI digest
- **WHEN** an authenticated admin disables an AI digest
- **THEN** the digest SHALL be excluded from the feed and matrix cell bindings

### Requirement: LLM provider configuration reuse
The system SHALL use the existing `AISettings` (provider, API key, model, endpoint) for all AI content generation; no separate AI configuration SHALL be introduced.

#### Scenario: Generation uses saved settings
- **WHEN** a generation or digest request is made
- **THEN** the request SHALL use the endpoint, model, and API key from `AISettings`

#### Scenario: Missing API key
- **WHEN** an AI feature is invoked and no API key is configured
- **THEN** the system SHALL return a clear error explaining that AI settings are not configured

