# outbound-alerting Specification

## Purpose
TBD - created by archiving change outbound-alerting. Update Purpose after archive.
## Requirements
### Requirement: Source failure alerts
The system SHALL send an alert when a datasource transitions into a failing state (consecutive render failures reaching the configured threshold).

#### Scenario: Source starts failing
- **WHEN** a datasource's consecutive failure count reaches the configured threshold and its previous alert state was not "failing"
- **THEN** the system SHALL send a "source failing" alert through every enabled channel naming the source

#### Scenario: Source recovers
- **WHEN** a datasource that previously triggered a failure alert renders successfully
- **THEN** the system SHALL send a "source recovered" alert through every enabled channel

#### Scenario: No alert during transient failures
- **WHEN** a datasource fails fewer times than the configured threshold
- **THEN** the system SHALL NOT send a failure alert

### Requirement: Device staleness alerts
The system SHALL alert when an enabled device goes stale (no `last_seen_at` update within the stale threshold).

#### Scenario: Device goes stale
- **WHEN** an enabled device has not reported within its stale threshold and its previous alert state was not "stale"
- **THEN** the system SHALL send a "device offline" alert naming the device

#### Scenario: Device comes back
- **WHEN** a device that previously triggered a staleness alert reconnects
- **THEN** the system SHALL send a "device back online" alert

#### Scenario: Stale threshold from refresh interval
- **WHEN** no explicit stale threshold is configured
- **THEN** the system SHALL use 3 times the device's refresh interval as the stale threshold

### Requirement: Alert cooldown
The system SHALL prevent alert storms by enforcing a per-key cooldown.

#### Scenario: Cooldown suppresses repeat alerts
- **WHEN** the same source/device would trigger the same alert state within the configured cooldown window
- **THEN** the system SHALL NOT send a duplicate alert

#### Scenario: Test alert bypasses cooldown
- **WHEN** an admin clicks "Send test alert"
- **THEN** the system SHALL send a test message through every enabled channel regardless of cooldown

### Requirement: Gotify channel
The system SHALL deliver alerts to a Gotify server when configured.

#### Scenario: Gotify alert delivery
- **WHEN** Gotify is enabled with a server URL and app token, and an alert fires
- **THEN** the system SHALL POST a message to `{server}/message` with `X-Gotify-Key` auth containing the alert title and message

#### Scenario: Gotify delivery failure
- **WHEN** the Gotify server is unreachable or returns an error
- **THEN** the system SHALL log the delivery failure and continue without crashing

### Requirement: Email channel
The system SHALL deliver alerts by email using the existing `EmailSettings` SMTP configuration when enabled.

#### Scenario: Email alert delivery
- **WHEN** the email channel is enabled and an alert fires
- **THEN** the system SHALL send a plain-text email to the configured recipient via the SMTP settings

#### Scenario: Email delivery failure
- **WHEN** SMTP delivery fails
- **THEN** the system SHALL log the delivery failure and continue without crashing

### Requirement: Alert administration
The system SHALL provide admin configuration for alerting.

#### Scenario: Configure alert settings
- **WHEN** an authenticated admin saves alert settings (channel toggles, failure threshold, stale threshold, cooldown, recipient email, Gotify URL/token)
- **THEN** the system SHALL persist the settings and apply them to the running alert engine

#### Scenario: Engine idle when no channels enabled
- **WHEN** no alert channel is enabled
- **THEN** the system SHALL NOT send any alerts

#### Scenario: Test alert button
- **WHEN** an authenticated admin clicks "Send test alert"
- **THEN** the system SHALL send a test message through each enabled channel and report per-channel success or failure

