# Spec: physical-inputs

## ADDED Requirements

### Requirement: Input event transport over protocol v2

The system SHALL carry local hardware input events from the device to the server additively within the existing protocol v2 negotiation. The server welcome SHALL add an `inputs` capability to the capabilities it already advertises for a `?protocol=2` connection, and the device client SHALL send a single new message shape, `{"type":"input","source":"<source>","event":"<event>","value":<string|number>,"ts":<epoch-ms>}`, only when the negotiated server advertised `inputs`. The handshake, existing frame keys, and existing `{"action":...}` control messages SHALL remain unchanged; a connection without `inputs` SHALL neither receive nor be expected to send input messages.

#### Scenario: Capability advertised on v2

- **WHEN** a device connects with `?protocol=2` to a server with input support
- **THEN** the welcome SHALL include `inputs` alongside `brightness`, `spectrum`, and `hold`, and no existing capability SHALL be removed or renamed

#### Scenario: v1 connection never sees input support

- **WHEN** a device connects with no `protocol` param, `?protocol=1`, or an unknown/malformed version
- **THEN** the server SHALL send no welcome, SHALL NOT advertise `inputs`, and SHALL ignore any input message on that connection exactly as it ignores unsupported control messages today

#### Scenario: Client gates on the negotiated capability

- **WHEN** a device client that supports inputs connects to a server whose welcome does not advertise `inputs`
- **THEN** the client SHALL NOT send input messages and SHALL keep using the legacy button `{"action":"next"|"pause"|"hold"}` behavior

### Requirement: Input event validation and bounded ingestion

The server SHALL validate every input message against a closed vocabulary: sources `button:next`, `button:pause`, `encoder`, `nfc`, `pir`, `mmwave`, and `lux`; events `press`, `rotate`, `tap`, `presence`, and `lux`; and per-event value types and bounds (a printable/hex tag id of bounded length for `tap`, a bounded integer step for `rotate`, a boolean or `present`/`absent` value for `presence`, and a finite physical-range number for `lux`). Malformed, oversized, unknown, or unsupported input messages SHALL be dropped with a debug log, SHALL NOT close or degrade the connection, and SHALL NOT affect frame delivery.

#### Scenario: Valid event accepted

- **WHEN** an `inputs` connection sends `{"type":"input","event":"tap","source":"nfc","value":"04a1b2c3"}`
- **THEN** the server SHALL accept it and pass it to input dispatch without error

#### Scenario: Unknown source or event dropped

- **WHEN** an input message carries a source or event outside the closed vocabulary
- **THEN** the server SHALL drop it, log at debug, and keep streaming frames on the same connection

#### Scenario: Invalid or oversized value dropped

- **WHEN** a `lux` event carries a non-numeric or non-finite value, or any input frame exceeds the bounded size
- **THEN** the server SHALL drop it without dispatching any action and without erroring the feed loop

### Requirement: Authenticated and rate-limited ingestion

Input events SHALL be accepted only on the token-authenticated device stream (`GET /ws/device/<token>`) after the device exists and is enabled, exactly like existing device traffic. Ingestion SHALL apply a per-connection rate limit and SHALL drop excess events while keeping the connection and rendering alive. No new unauthenticated endpoint, inbound port, or second listener SHALL be introduced for input events.

#### Scenario: Unknown or disabled device cannot inject events

- **WHEN** a caller attempts to submit input-shaped traffic without a valid enabled device token
- **THEN** the server SHALL reject it through the existing device-token check and SHALL NOT execute any input action

#### Scenario: Excess events are dropped, not fatal

- **WHEN** one connection exceeds the input rate limit
- **THEN** the server SHALL drop the excess events with a debug log, SHALL NOT disconnect the device, and SHALL continue sending frames

### Requirement: Device-side input sources with graceful degradation

The device client SHALL support optional local input sources: the existing GPIO buttons (extended, not duplicated), an NFC/RFID reader, a rotary encoder, a presence sensor (PIR or mmWave), and an ambient lux sensor. Each source SHALL start best-effort, and when its pin, bus, or Python library is absent it SHALL log one informative message and become an inert no-op. A missing or failing source SHALL NOT crash the client, disable other sources, or affect frame rendering or reconnection.

#### Scenario: Source reports when hardware is present

- **WHEN** a configured source reads an input (button press, tag read, encoder rotation, presence edge, lux sample)
- **THEN** the client SHALL emit the corresponding `{"type":"input",...}` event over the open device WebSocket

#### Scenario: Missing hardware or library degrades to a no-op

- **WHEN** a reader, encoder, sensor, or its optional Python dependency is not present
- **THEN** the client SHALL log once and continue running, and no input events SHALL be sent for that source

#### Scenario: One broken source does not disable others

- **WHEN** one configured source raises an error during setup or while running
- **THEN** the other sources SHALL keep working and the WebSocket feed SHALL continue rendering frames

### Requirement: Encoder and button defaults with configurable bindings

Buttons SHALL keep their existing short-press/long-press behavior and event meanings. When `inputs` is negotiated, the device SHALL report encoder rotation as `rotate` with a signed step and the encoder switch as a `press` event. When no admin binding matches, the server SHALL apply built-in defaults: `button:next` press → next, `button:pause` press → pause, `hold` → unchanged no-op, `rotate` positive → next, `rotate` negative → previous, and encoder press → brightness cycle; every default SHALL be overridable by an admin binding. When `inputs` is not negotiated, device behavior SHALL be byte-identical to today.

#### Scenario: Defaults apply with no bindings configured

- **WHEN** a physical next button or a clockwise encoder rotation arrives and no binding matches
- **THEN** the server SHALL advance the device feed and a counter-clockwise rotation SHALL step it back, with no configuration required

#### Scenario: A binding overrides a default

- **WHEN** an enabled binding maps `button:next` press to a scene action
- **THEN** that press SHALL run the scene action instead of the built-in next default

#### Scenario: Legacy behavior without `inputs`

- **WHEN** the server does not advertise `inputs`
- **THEN** the buttons SHALL keep sending the existing `{"action":"next"|"pause"|"hold"}` messages and the server handling SHALL be unchanged

### Requirement: NFC/RFID tap reporting

The device client SHALL report a recognised NFC/RFID tag as `{"type":"input","event":"tap","source":"nfc","value":"<uid>"}` with a normalized lowercase tag id. The device SHALL suppress repeated reads of the same tag within a configurable dedupe window. An absent or unreadable reader SHALL remain inert, and a partial/garbled read SHALL be dropped without emitting an event.

#### Scenario: Tag read emits a tap event

- **WHEN** a tag is presented to the configured reader
- **THEN** the client SHALL send one `tap` event carrying the normalized tag id

#### Scenario: Duplicate read suppressed

- **WHEN** the same tag is read again within the dedupe window
- **THEN** the client SHALL NOT emit another tap event until the window elapses

#### Scenario: No reader configured

- **WHEN** no NFC/RFID reader is configured or the optional library is missing
- **THEN** the client SHALL log once and the absence of taps SHALL have no other effect

### Requirement: Presence and ambient lux reporting

The device client SHALL report presence only on state edges, as `{"type":"input","event":"presence","source":"pir"|"mmwave","value":"present"|"absent"}`, and SHALL NOT repeatedly report an unchanged presence state. The client SHALL report ambient lux as `{"type":"input","event":"lux","source":"lux","value":<number>}` at a configurable interval and SHALL only emit readings within the sensor's physical range. A missing or failed presence/lux sensor SHALL be inert.

#### Scenario: Presence edge reported once

- **WHEN** the presence sensor transitions from absent to present and stays present
- **THEN** exactly one `presence: present` event SHALL be sent until a transition back to absent

#### Scenario: Lux reported periodically

- **WHEN** the lux sensor is configured and readable
- **THEN** the client SHALL report lux values at the configured interval as finite numbers

#### Scenario: Sensor absent or unreadable

- **WHEN** the presence or lux sensor is not configured or a read fails
- **THEN** the client SHALL continue running without emitting that event type and SHALL NOT crash on repeated read failures
