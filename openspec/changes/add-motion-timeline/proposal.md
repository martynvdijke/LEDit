## Why

LEDit's only authored motion today is frame-by-frame: `PixelArtDS` steps through `PixelFrame.Duration` frames via the `Animator` interface (`datasource/pixelart.go:20`) and `serveFeed` re-renders animated sources in-slot (`handlers/websocket.go:1497`); `add-slide-transitions` adds exactly three whole-frame blends at slot boundaries (`render/composite.go`). Neither can express motion *within* a rendered frame — an element travelling across a three-panel wall, a title revealing character by character, a score flashing when it changes — so all motion is either a flicker between static frames or a Go constant (`render/pixelart.go`, `datasource/pixelart.go`). This change adds a server-rendered, keyframe-based motion timeline so motion is authored as versioned data and executed deterministically by the renderer, while every existing pixel-art animation and slide transition keeps working unchanged.

## What Changes

- Add a **versioned timeline artifact**: a `MotionTimeline` entity storing `schema_version`, `duration_ms`, `tick_ms`, `playback` (`loop` | `once` | `ping_pong`), a wrapped base source (`base_type`/`base_id` using the same `<type>:<id>` keys as `handlers/sources.go` `buildSourceIndex`), and a bounded `tracks` JSON document of tracks, keyframes, and per-segment easing (`linear`, `ease_in`, `ease_out`, `ease_in_out`, `steps`).
- Add **properties**: `x`, `y`, `opacity`, `brightness`, `scale`, `color` (palette index / hex tint), `frame` (base frame selection), and `reveal` (text character count). Interpolation is type-aware: continuous properties lerp, discrete properties snap via easing/steps.
- Execute timelines **server-side in the renderer**: `datasource/timeline.go` (`TimelineDS`) samples the document at the current render tick and applies pure pixel transforms from a new `render/motion.go`; devices keep receiving the unchanged `{format, image, source, next}` WebSocket shape (no JS on devices, no client-side rendering).
- Add the **timeline editor** at `/admin/timelines` (`web/templates/admin/timeline_editor.html` + vanilla TS `web/frontend/timeline_editor.ts` modeled on `layout_editor.ts`): timeline ruler/playhead, drag/add/delete/copy/duplicate keyframes, easing pickers, debounced scrub preview against the real renderer through a new session-authenticated preview endpoint (reusing the `admin-live-preview` / `web-live-preview` seams), and a single hidden `tracks` input synced on submit so edits stay undo-safe and atomic.
- **Attachments without contract changes**: timelines resolve as ordinary feed sources and nested composition children, so scenes can reference `source_type: "timeline"` and start playback on scene activation, playlist items can select them once `timeline` joins `KnownSourceTypes`, and texts slides can be wrapped as a timeline base using the existing `Datasource`, `Animator`, and `StateProvider` interfaces (`datasource/datasource.go:16`).
- **Text choreography**: a `reveal` property plus an optional `RevealRenderer` capability for character-accurate reveal (fallback: pixel column mask), and **state-triggered effects** — chart draw-in, score flash, countdown tick — driven by values the base already exposes through `StateProvider`.
- **GIF conversion**: the media importer's existing frame pipeline (`handlers/media_import.go`, caps `maxGIFFrames`/`maxTargetGrid`) can emit a timeline whose `frame` track replays the GIF's per-frame delays.
- **Multi-panel choreography**: `x`/`y` are logical-canvas coordinates (`render.PanelLogicalWidth`, `render/panel_canvas.go`), transforms run before bezel slicing (`SlicePanelGapsPNG`), and `render/mapping.go` remains the only physical mapping step.
- **Bounds and errors**: max tracks/keyframes/hooks/duration are validated on save and preview with 400-style errors; malformed or oversized documents never reach the renderer.

## Capabilities

### New Capabilities
- `motion-timeline`: versioned keyframe timeline document model, bounded validation, deterministic renderer execution (tick sampling, easing, playback modes, pixel transforms), timeline datasource and feed/LKG integration, scene/playlist attachment, GIF conversion, admin editor UI, and the logical multi-panel coordinate space.
- `text-choreography`: text reveal property and optional character-accurate `RevealRenderer`, text-slide timeline attachment, and `StateProvider`-driven effects (chart draw-in, score flash, countdown tick) that leave existing datasource contracts unchanged.

### Modified Capabilities
- None — all behavior is additive. Existing specs (`matrix-grid-layout`, `live-feed-control-room`, `admin-live-preview`, `web-live-preview`, `pixel-art-*` changes) are consumed as seams, not changed: pixel-art JSON stays the frame format, device protocol is untouched, and transitions still blend whole frames.

## Impact

- **New server code**: `render/motion.go` (document types, validation, easing, tick sampling, transform primitives), `datasource/timeline.go` (`TimelineDS`), `ent/schema/motion_timeline.go`, `handlers/timelines.go` (CRUD, preview, GIF conversion), plus generated ent code (`go generate ./ent`).
- **Modified server code**: `handlers/server.go` (admin routes), `handlers/sources.go` (`buildSourceIndex`/`Resolve` index `timeline:*`), `handlers/websocket.go` (`serveFeed` calls the optional playback-start hook at slot start; message shape unchanged), `handlers/media_import.go` (timeline output path), `datasource/playlist.go` (`KnownSourceTypes["timeline"]`), `handlers/handlers.go` (settings eager-load for timeline edges).
- **New frontend**: `web/templates/admin/timelines.html`, `web/templates/admin/timeline_editor.html`, `web/frontend/timeline_editor.ts` → `web/static/assets/timeline_editor.js` (Vite build, no new npm dependency).
- **Device protocol**: unchanged; motion is emitted as additional PNG frames on the existing v1/v2 message shape, exactly like today's animated sources.
- **Risk**: motion must remain deterministic and budgeted per tick; tick snapping, bounded track/keyframe counts, and pure sampling keep repeated renders byte-identical and cheap, while the existing animated-source LKG bypass (`handlers/websocket.go:1512`) keeps cache semantics unchanged.
