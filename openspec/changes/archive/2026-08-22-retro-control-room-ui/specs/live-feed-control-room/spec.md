## ADDED Requirements

### Requirement: Live feed stage
The live feed MUST present the current media in a bounded, responsive stage with an intentional loading/empty state and enough visual hierarchy to make the feed the primary surface.

#### Scenario: Waiting for feed
- **WHEN** the WebSocket has not delivered media
- **THEN** the stage shows a clear waiting/connection state without collapsing the surrounding layout

#### Scenario: Image or video received
- **WHEN** an image or MP4 message is received
- **THEN** the appropriate media element is shown in the stage, the prior media is hidden cleanly, and existing clock/marquee overlays remain positioned correctly

### Requirement: Feed telemetry
The live feed MUST expose current source, next item, connection status, and clock as distinct readable telemetry, with status conveyed by text and not color alone.

#### Scenario: Connected and receiving
- **WHEN** the WebSocket is connected and media arrives
- **THEN** the source, next item, and receiving/connected state are visible and updated from the existing messages

#### Scenario: Reconnecting
- **WHEN** the WebSocket closes and reconnect attempts remain
- **THEN** the UI shows the reconnecting countdown and does not present stale media as current connection health

### Requirement: Feed controls
The feed MUST retain pause/resume, skip, fullscreen, E-Ink toggle, and E-Ink refresh behavior, with controls grouped by purpose and accessible on desktop and mobile.

#### Scenario: Pause and resume
- **WHEN** the user activates pause or resume
- **THEN** the control label and WebSocket action reflect the new state without changing the existing protocol

#### Scenario: Skip
- **WHEN** the user activates skip
- **THEN** the existing next-feed action is sent and the UI remains responsive while the next item is received

#### Scenario: Fullscreen
- **WHEN** the user activates fullscreen
- **THEN** the feed stage expands into the existing fullscreen presentation, navigation is hidden appropriately, and the action provides an accessible exit label

#### Scenario: E-Ink mode
- **WHEN** the user enables E-Ink mode
- **THEN** the page applies the static high-contrast presentation, freezes the clock as currently specified, exposes refresh, and avoids animated transitions

### Requirement: Feed accessibility and responsive behavior
The feed MUST remain operable with keyboard and touch, expose accessible names and state to assistive technology, and avoid horizontal overflow at supported viewport widths.

#### Scenario: Keyboard control
- **WHEN** a keyboard user focuses a feed action
- **THEN** focus is visible and activating the control produces the same behavior as pointer activation

#### Scenario: Narrow viewport
- **WHEN** the feed is viewed on a phone-sized viewport
- **THEN** media, telemetry, and actions fit within the viewport or stack without clipping or requiring horizontal scrolling
