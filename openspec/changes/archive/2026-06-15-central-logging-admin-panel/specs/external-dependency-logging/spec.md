## ADDED Requirements

### Requirement: Datasource API call logging
The system SHALL log all external API calls made by datasource implementations through the central logging system.

#### Scenario: Log Sonarr API calls
- **WHEN** the Sonarr datasource fetches series data
- **THEN** the system SHALL log: API call start at info level with source "sonarr" and attrs (url), API success at info level with source "sonarr" and attrs (series count), API failure at error level with source "sonarr" and attrs (error details)

#### Scenario: Log Radarr API calls
- **WHEN** the Radarr datasource fetches movie data
- **THEN** the system SHALL log: API call start at info level with source "radarr", success at info level, failure at error level

#### Scenario: Log Weather API calls
- **WHEN** the Weather datasource fetches weather data
- **THEN** the system SHALL log: API call start at info level with source "weather" and location, success at info level with temperature summary, failure at error level

#### Scenario: Log HomeAssistant API calls
- **WHEN** the HomeAssistant datasource fetches entity states
- **THEN** the system SHALL log: API call start at info level with source "homeassistant", success at info level with entity count, failure at error level

#### Scenario: Log F1 API calls
- **WHEN** the F1 datasource fetches race data
- **THEN** the system SHALL log: API call start at info level with source "f1", success at info level, failure at error level

#### Scenario: Log Untappd API calls
- **WHEN** the Untappd datasource fetches beer data
- **THEN** the system SHALL log: API call start at info level with source "untappd", success at info level, failure at error level

#### Scenario: Log Crypto API calls
- **WHEN** the Crypto datasource fetches price data
- **THEN** the system SHALL log: API call start at info level with source "crypto", success at info level, failure at error level

#### Scenario: Log Stock API calls
- **WHEN** the Stock datasource fetches price data
- **THEN** the system SHALL log: API call start at info level with source "stock", success at info level, failure at error level

#### Scenario: Log RSS Feed fetches
- **WHEN** the RSS Feed datasource fetches feed data
- **THEN** the system SHALL log: fetch start at info level with source "rssfeed" and feed name, success at info level with entry count, failure at error level

#### Scenario: Log Calendar fetches
- **WHEN** the Calendar datasource fetches calendar events
- **THEN** the system SHALL log: fetch start at info level with source "calendar", success at info level, failure at error level

#### Scenario: Log Image/Video file reads
- **WHEN** the Image or Video datasource reads a file
- **THEN** the system SHALL log: read start at debug level with source "images"/"videos" and file path, success at info level, failure at error level

### Requirement: Datasource fallback logging
When a datasource API call fails and falls back to a placeholder render, the system SHALL log the fallback event.

#### Scenario: Log Sonarr fallback
- **WHEN** the Sonarr datasource API fails and fallbackSonarr() is called
- **THEN** the system SHALL log at warn level: source "sonarr", message "using fallback render", attrs: error details

#### Scenario: Log generic fallback
- **WHEN** any datasource uses a fallback render due to API error
- **THEN** the system SHALL log at warn level with the appropriate source tag and error details

### Requirement: WebSocket event logging
The system SHALL replace raw `log.Printf` in the WebSocket handler with structured `slog` calls.

#### Scenario: Log WebSocket upgrade
- **WHEN** a WebSocket connection upgrade fails
- **THEN** the system SHALL log at error level: source "websocket", message describing the upgrade failure

#### Scenario: Log settings load failure
- **WHEN** the WebSocket handler fails to load settings
- **THEN** the system SHALL log at error level: source "websocket", message describing the load failure

#### Scenario: Log source render errors
- **WHEN** a datasource's GetPNG() fails during WebSocket feed loop
- **THEN** the system SHALL log at error level: source "websocket", message containing the source name and error

#### Scenario: Log WebSocket write errors
- **WHEN** a WebSocket write operation fails
- **THEN** the system SHALL log at error level: source "websocket", message describing the write failure

### Requirement: Logging infrastructure self-logging
The logging infrastructure (store, cleanup, slog init) SHALL use `slog` instead of raw `log.Printf`/`log.Println`.

#### Scenario: Log store queue full
- **WHEN** the log store queue is full and an entry is dropped
- **THEN** the system SHALL log at warn level: source "logging", message "log store queue full, dropping entry"

#### Scenario: Log store flush error
- **WHEN** the log store batch flush fails
- **THEN** the system SHALL log at error level: source "logging", message containing the database error

#### Scenario: Log cleanup operations
- **WHEN** the log cleanup goroutine deletes old entries
- **THEN** the system SHALL log at info level: source "logging", message with count of deleted entries and retention days

#### Scenario: Log cleanup error
- **WHEN** the log cleanup goroutine encounters an error
- **THEN** the system SHALL log at error level: source "logging", message containing the error

#### Scenario: Log init
- **WHEN** the logging system is initialized
- **THEN** the system SHALL log at info level: source "logging", message with minimum level setting

### Requirement: AI provider test connection logging
The AI settings "Test Connection" feature SHALL log the full lifecycle of connection tests.

#### Scenario: Log test connection start
- **WHEN** an admin clicks "Test Connection" on AI settings
- **THEN** the system SHALL log at info level: source "ai-settings", message "testing connection to <provider>", attrs: provider, model, endpoint

#### Scenario: Log test connection success
- **WHEN** a test connection succeeds
- **THEN** the system SHALL log at info level: source "ai-settings", message "connection successful", attrs: latency in ms

#### Scenario: Log test connection failure
- **WHEN** a test connection fails
- **THEN** the system SHALL log at error level: source "ai-settings", message "connection failed", attrs: error details, latency in ms

### Requirement: Datasource not-configured logging
When a datasource is visited but has no configuration, the system SHALL log this state.

#### Scenario: Log not configured
- **WHEN** a datasource's GetPNG() is called but URL or token is empty
- **THEN** the system SHALL log at warn level with source "<ds-type>", message "<name> not configured"
