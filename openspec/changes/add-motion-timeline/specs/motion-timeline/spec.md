# Spec: motion-timeline

## ADDED Requirements

### Requirement: Timeline document model

The system SHALL persist motion timelines as `MotionTimeline` entities with `name`, `enabled`, a `schema_version`, `duration_ms`, `tick_ms`, `playback`, a `background` color, a wrapped base source (`base_type` + `base_id` using the shared `<type>:<id>` source keys), a `tracks` JSON document, and a `hooks` JSON document. The `tracks` document SHALL contain one or more tracks, each with a name, a property, and an ordered list of keyframes (`at_ms`, `value`, `easing`). Documents SHALL be versioned so future format changes remain distinguishable.

#### Scenario: Create and read a timeline
- **WHEN** an administrator creates a timeline named "Intro" wrapping a composition with a 2000 ms duration and two tracks
- **THEN** the timeline SHALL persist and round-trip with its name, duration, playback, base reference, and tracks intact

#### Scenario: Disabled timeline is inert
- **WHEN** a timeline has `enabled=false`
- **THEN** the system SHALL NOT offer it as a feed source or resolve it through the source index

#### Scenario: Version is recorded
- **WHEN** a timeline is saved
- **THEN** its `schema_version` SHALL be stored and returned on read

#### Scenario: Invalid base rejected
- **WHEN** an administrator saves a timeline whose `base_type`/`base_id` does not resolve to a configured source, or whose base is another timeline
- **THEN** the system SHALL reject the save with a descriptive validation error and persist nothing

### Requirement: Closed property set with typed keyframes

Timeline tracks SHALL support exactly the properties `x` and `y` (integer logical-canvas pixels), `opacity` (0–100), `brightness` (0–100), `scale` (25–400 percent), `color` (palette index or `#rrggbb`), `frame` (integer base frame index), and `reveal` (character count). Continuous properties SHALL interpolate between keyframes; discrete properties (`frame`, palette-index `color`, `reveal`) SHALL step at tick boundaries. Unknown property names SHALL be rejected.

#### Scenario: Continuous property interpolates
- **WHEN** an `x` track has keyframes at `0 → 0` and `1000 → 40` with linear easing and the timeline is sampled at 500 ms
- **THEN** the sampled `x` SHALL be 20 logical-canvas pixels

#### Scenario: Discrete property steps
- **WHEN** a `frame` track has keyframes at `0 → 0` and `500 → 3` with `steps` easing and the timeline is sampled at 499 ms
- **THEN** the sampled frame index SHALL remain 0

#### Scenario: Unknown property rejected
- **WHEN** a track declares a property outside the supported set
- **THEN** the system SHALL reject the save with a 400-style validation error naming the property

#### Scenario: Property value out of range rejected
- **WHEN** an `opacity` keyframe is 140 or a `scale` keyframe is 10
- **THEN** the system SHALL reject the save with a descriptive validation error

### Requirement: Per-segment easing

Each keyframe SHALL carry the easing applied on the segment leading from that keyframe to the next. Supported easings SHALL be `linear`, `ease_in`, `ease_out`, `ease_in_out`, and `steps`; unsupported values SHALL be rejected. Easing SHALL be evaluated deterministically and quantized before any pixel transform.

#### Scenario: Linear midpoint
- **WHEN** a segment runs from 0 to 100 over 1000 ms with linear easing and is sampled at 500 ms
- **THEN** the eased progress SHALL be 50

#### Scenario: Ease-in starts slow
- **WHEN** the same segment uses `ease_in` and is sampled at 250 ms
- **THEN** the eased progress SHALL be less than the linear progress at the same time

#### Scenario: Unknown easing rejected
- **WHEN** a keyframe declares an easing other than the supported set
- **THEN** the system SHALL reject the document with a 400-style validation error

### Requirement: Playback modes and tick-based timing

The system SHALL support `loop`, `once`, and `ping_pong` playback. `once` SHALL hold the final keyframe after the duration elapses; `loop` SHALL wrap to the start; `ping_pong` SHALL reverse direction at each end without duplicating endpoint frames. Sampling SHALL floor time to the timeline's `tick_ms`, and two samples within the same tick SHALL produce byte-identical frames.

#### Scenario: Loop wraps
- **WHEN** a 1000 ms loop timeline with `tick_ms=100` is sampled at 1100 ms
- **THEN** it SHALL evaluate the keyframe state at the equivalent of 100 ms

#### Scenario: Once holds the end
- **WHEN** a 1000 ms `once` timeline is sampled after 1500 ms
- **THEN** it SHALL render the final keyframe state and stop emitting changed frames

#### Scenario: Ping-pong reverses
- **WHEN** a 1000 ms `ping_pong` timeline is sampled at 750 ms
- **THEN** it SHALL evaluate the keyframe state of the reverse pass, equivalent to 250 ms forward

#### Scenario: Same tick is deterministic
- **WHEN** the same timeline document and base are rendered twice within one tick
- **THEN** the two output PNGs SHALL be byte-identical

### Requirement: Bounded validation and 400-style errors

The system SHALL validate timelines against fixed bounds: at most 8 tracks, at most 256 keyframes per track, at most 16 hooks, `duration_ms` at most 600000 and greater than zero, `tick_ms` between 40 and 1000, `x`/`y` within ±4096, `scale` between 25 and 400, `opacity`/`brightness` between 0 and 100, and keyframes sorted and non-negative with at least one per track. Malformed JSON, missing required fields, or any bound violation SHALL fail validation with a descriptive 400-style error on save and with `400 {"error": ...}` on preview or API endpoints, and SHALL never reach the renderer.

#### Scenario: Over-cap document rejected
- **WHEN** a timeline declares 9 tracks or a track declares 257 keyframes
- **THEN** the system SHALL reject it with a descriptive error and persist no change

#### Scenario: Malformed JSON rejected
- **WHEN** the `tracks` field is not valid JSON
- **THEN** the system SHALL reject the save and re-render the form with the error

#### Scenario: Duration out of range rejected
- **WHEN** `duration_ms` is zero, negative, or greater than 600000
- **THEN** the system SHALL reject the document with a 400-style error

#### Scenario: Preview returns 400 JSON
- **WHEN** an invalid timeline document is submitted to the timeline preview endpoint
- **THEN** the endpoint SHALL respond `400` with a JSON `error` and SHALL NOT return a PNG

### Requirement: Server-side renderer execution

Timelines SHALL be executed by the server renderer; no device or frontend code SHALL be required to evaluate a timeline. `TimelineDS` SHALL render the resolved base for the device resolution and apply the fixed transform order: `frame` selection, `color`/tint, `scale`, `translate`, `brightness`, `opacity`, `reveal`. The WebSocket frame message shape (`format`, `image`, `source`, `next`) SHALL remain unchanged.

#### Scenario: Device message shape unchanged
- **WHEN** a device streams a timeline source
- **THEN** every frame SHALL use the existing PNG message shape and the device SHALL need no code change

#### Scenario: Transform order is deterministic
- **WHEN** a timeline declares color, scale, x/y, brightness, opacity, and reveal keyframes that are all active at the same tick
- **THEN** the transforms SHALL be applied in the documented order and repeated renders SHALL be byte-identical

#### Scenario: Base failure degrades gracefully
- **WHEN** the wrapped base fails to render at a tick
- **THEN** the timeline SHALL return the base's fallback or the last-known-good frame rather than crash the feed

### Requirement: Timeline datasource and feed integration

Enabled timelines SHALL be indexed in the shared source lookup as `timeline:<id>` and SHALL appear in the feed source pool, exactly like other sources. A timeline SHALL behave as an animated source (implementing the `Animator` capability) so it is re-rendered within its display slot; its initial render SHALL use the last-known-good cache, and in-slot re-renders SHALL bypass that cache. Timelines SHALL be usable as composition children.

#### Scenario: Timeline appears in the feed
- **WHEN** an enabled timeline exists
- **THEN** it SHALL resolve through the source index and stream its base with motion applied during its slot

#### Scenario: In-slot animation
- **WHEN** a timeline's slot is displayed for longer than one tick
- **THEN** the feed SHALL send additional frames as the sampled tick changes, without waiting for the slot to end

#### Scenario: Composition child
- **WHEN** a composition region references `timeline:<id>`
- **THEN** the region SHALL render the timeline's animated output and the composition SHALL report ambient while the timeline can change

#### Scenario: Disabled timeline omitted
- **WHEN** a timeline is disabled
- **THEN** it SHALL NOT appear in the source index, feed pool, or binding options

### Requirement: Attachment to scenes and playlists

Timelines SHALL be attachable without changing existing datasource contracts: playlist items SHALL accept `source_type: "timeline"`, and scene actions SHALL reference timelines through the existing generic source reference. Playback SHALL start when a timeline's slot begins, and a scene activation SHALL start its timeline from the beginning. Existing playlist items and scene actions that do not reference timelines SHALL behave exactly as before.

#### Scenario: Playlist item plays a timeline
- **WHEN** a playlist contains `{"source_type":"timeline","source_id":N}`
- **THEN** the feed SHALL play that timeline when the playlist reaches the item

#### Scenario: Scene activation restarts a once timeline
- **WHEN** a scene whose action references a `once` timeline activates a second time
- **THEN** the timeline SHALL restart from its first keyframe at the new activation

#### Scenario: Slot start resets playback
- **WHEN** a loop or `once` timeline's slot begins again after skipping away
- **THEN** its playback SHALL restart from the slot start rather than resuming a stale clock

### Requirement: Timeline editor UI

The system SHALL provide session-authenticated admin pages to list, create, edit, and delete timelines. The editor SHALL present a timeline ruler and playhead with one lane per track, allow adding, moving, deleting, copying, and duplicating keyframes, allow choosing each keyframe's easing, and SHALL serialize the edited document into a hidden form field that is synced on submit so undo/redo state and saved JSON cannot diverge. Server-side validation SHALL remain authoritative.

#### Scenario: Save an edited document
- **WHEN** an administrator moves a keyframe and submits the form
- **THEN** the hidden `tracks` field SHALL contain the edited document and the server SHALL persist it

#### Scenario: Copy and duplicate keyframes
- **WHEN** an administrator copies a keyframe and pastes or duplicates it
- **THEN** a new keyframe SHALL be inserted with the same property/value/easing and an adjusted time, undoable in one step

#### Scenario: Invalid form keeps input
- **WHEN** the form is submitted with an out-of-bounds keyframe
- **THEN** the server SHALL re-render the editor with the error and the submitted values preserved

#### Scenario: Undo is safe
- **WHEN** an administrator undoes a keyframe edit and submits
- **THEN** the saved document SHALL match the visible editor state

### Requirement: Scrub preview against the real renderer

The system SHALL provide a session-authenticated timeline preview endpoint that accepts the current editor document and a sample time and returns a server-rendered PNG produced by the same `TimelineDS` execution path as the feed. The endpoint SHALL render only stored or form-supplied timeline documents and configured sources, SHALL return `400` for invalid input, and SHALL NOT modify feed state, ordering, pause/skip state, or display analytics.

#### Scenario: Preview at a sample time
- **WHEN** the editor requests a preview at 750 ms with an unsaved document
- **THEN** the endpoint SHALL return the PNG that the feed would render for that tick

#### Scenario: Preview requires authentication
- **WHEN** an unauthenticated request hits the timeline preview endpoint
- **THEN** the request SHALL be rejected with the standard admin authentication response

#### Scenario: Preview does not affect the feed
- **WHEN** previews are requested while the feed is streaming
- **THEN** the feed SHALL continue unchanged and no preview SHALL appear in display analytics

#### Scenario: Unbounded preview rejected
- **WHEN** a preview document exceeds the documented bounds
- **THEN** the endpoint SHALL return `400` with an error and no PNG

### Requirement: GIF import conversion

The system SHALL allow an imported GIF to be converted into a timeline where sensible: the existing import pipeline's palette and frames SHALL become a Pixel Art base, and the timeline SHALL contain a `frame` track using `steps` easing at the GIF's cumulative per-frame delays. Existing upload, frame-count, grid-size, and palette caps SHALL apply, and the resulting Pixel Art SHALL remain playable on its own.

#### Scenario: GIF converts to a timeline
- **WHEN** a GIF with three frames and differing delays is imported and converted
- **THEN** a timeline SHALL be created whose base is the imported Pixel Art and whose frame track steps through those frames at the GIF delays

#### Scenario: Over-cap GIF rejected
- **WHEN** a GIF exceeds the existing frame-count, decoded-size, or upload-size caps
- **THEN** the system SHALL reject the import with a descriptive 400-style error and create nothing

#### Scenario: Pixel Art still plays standalone
- **WHEN** a converted GIF's Pixel Art is streamed without its timeline
- **THEN** it SHALL animate exactly as any other pixel-art source

### Requirement: Multi-panel choreography

Timeline `x`/`y` SHALL be interpreted in the logical panel canvas coordinate space (the width including bezel gaps) so motion can span a wall of panels. Transforms SHALL run before bezel-gap slicing, the sent frame SHALL remain the device's physical size, and physical mapping (gamma, color order, serpentine, origin) SHALL remain solely in the existing mapping path. Device-accurate previews SHALL apply the same logical-canvas rendering and slicing.

#### Scenario: Element travels across the wall
- **WHEN** a timeline moves a shape from `x=0` to `x=logical_width` on a two-panel device with a bezel gap
- **THEN** the shape SHALL cross the bezel seam continuously and the device SHALL receive frames of its exact physical width

#### Scenario: Single-panel device is unaffected
- **WHEN** a timeline is rendered for a device with `panel_cols=1` or `panel_gap=0`
- **THEN** the logical canvas SHALL equal the physical frame and no slicing SHALL occur

#### Scenario: Preview matches the device
- **WHEN** an administrator previews a timeline for a bezel-enabled device
- **THEN** the preview SHALL be sliced exactly like the live device feed

### Requirement: Existing motion behavior is preserved

Adding timelines SHALL NOT change existing frame-by-frame pixel-art playback, the three slide transitions, or any source that is not a timeline. `PixelFrameDoc` playback, `BlendFade`/`BlendWipe`/`BlendDissolve`, the WebSocket protocol, health recording, and last-known-good semantics SHALL keep their current requirements.

#### Scenario: Pixel art still animates
- **WHEN** a Pixel Art source with multiple frames streams with no timeline involved
- **THEN** it SHALL animate in-slot exactly as before this change

#### Scenario: Transitions still blend
- **WHEN** slide transitions are enabled and the feed advances between two non-timeline sources
- **THEN** the ramp frames SHALL be byte-identical to a run before timelines existed

#### Scenario: No timelines configured
- **WHEN** no `MotionTimeline` row exists
- **THEN** feed ordering, rendering, and device frames SHALL be identical to a build without this capability
