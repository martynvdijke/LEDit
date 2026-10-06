## 1. Timeline Document Model & Ent Schema

- [ ] 1.1 Add `ent/schema/motion_timeline.go` with `name`, `enabled`, `schema_version` (default 1), `duration_ms`, `tick_ms`, `playback`, `background`, `base_type`, `base_id`, `tracks` (JSON text), `hooks` (JSON text), `created_at`, `updated_at`; add the `GeneralSettings` edge; run `go generate ./ent` and verify the additive migration against an existing database
- [ ] 1.2 Define the document types and caps in `render/motion.go` (`MotionTimelineDoc`, `MotionTrack`, `MotionKeyframe`, `MotionHook`, `MaxTracks=8`, `MaxKeyframesPerTrack=256`, `MaxHooks=16`, `MaxDurationMs=600000`, tick 40–1000, `x`/`y` ±4096, `scale` 25–400) and implement `ParseMotionTimeline`/`ValidateMotionTimeline` mirroring `ParsePixelFrames`/`ValidatePixelDoc` (`render/pixelart.go:50`)
- [ ] 1.3 Implement pure sampling in `render/motion.go`: tick floor from `tick_ms`, local time for `loop`/`once`/`ping_pong`, segment lookup, and easing (`linear`, `ease_in`, `ease_out`, `ease_in_out`, `steps`) quantized before pixel math
- [ ] 1.4 Implement the typed property sampler for `x`, `y`, `opacity`, `brightness`, `scale`, `color`, `frame`, `reveal` (continuous lerp, discrete step) and reject unknown properties
- [ ] 1.5 Add `render/motion_test.go`: malformed JSON, every bound violation, unknown property/easing/playback, keyframe ordering, duration/tick ranges, loop wrap, `once` hold, ping-pong triangle, and same-tick byte determinism

## 2. Renderer Transform Primitives

- [ ] 2.1 Add pure transform primitives to `render/motion.go`: integer translate with clipping, nearest-neighbor scale around the canvas/anchor (`scaleNearestNeighbor`, `render/composite.go:122`), brightness (reuse `ApplyBrightness`), opacity lerp toward the timeline background (same math as `BlendFade`, `render/composite.go:147`), and palette-index/hex tint
- [ ] 2.2 Implement `frame` selection against the base `PixelFrameDoc` and the `reveal` fallback mask using the simple-font metrics (`simpleCharW`, `render/render.go:290`)
- [ ] 2.3 Implement `ApplyMotion(base, sample, opts)` applying the fixed order `frame → color → scale → translate → brightness → opacity → reveal`, with the order documented in the function comment
- [ ] 2.4 Add golden tests in `render/motion_test.go`: transform-order sensitivity, endpoint values, scale/translate clipping at canvas edges, tint determinism, and repeated-render byte equality

## 3. Timeline Datasource & Optional Capabilities

- [ ] 3.1 Add `datasource/timeline.go` with `TimelineDS` implementing `Datasource` (`GetPNG`) and `Animator` (`FrameCount` = total ticks, `NextFrame` = sampled tick index); resolve the base through an injected resolver func so the package does not import handlers
- [ ] 3.2 Add the optional `PlaybackStarter { StartAt(now time.Time) }` interface and implement it on `TimelineDS` so scenes/slots restart playback without touching `Animator` implementers (`datasource/pixelart.go:19`)
- [ ] 3.3 Implement `Ambienter` (true while the timeline can still change) and `StateProvider` passthrough when the base provides it (`datasource/datasource.go:16`); missing provider stays inert
- [ ] 3.4 Degrade gracefully: invalid document, unresolvable base, or base render error returns the base fallback/last-known-good frame and never panics the feed
- [ ] 3.5 Add `datasource/timeline_test.go`: sampling cadence, restart via `StartAt`, ambient lifecycle, state passthrough, and error fallback

## 4. Catalog, Feed & LKG Integration

- [ ] 4.1 Index enabled timelines as `timeline:<id>` in `handlers/sources.go` `buildSourceIndex` (`handlers/sources.go:302`), resolving `base_type`/`base_id` through the same index and rejecting `base_type == "timeline"`
- [ ] 4.2 Append enabled timelines to the feed pool in `loadSources` (`handlers/websocket.go:129`) and eager-load the timeline edge in the settings queries used by `WSHub` (`handlers/websocket.go:711`, `:784`, `:962`) and `sceneSourceIndex` (`handlers/scenes.go:736`)
- [ ] 4.3 Call `PlaybackStarter.StartAt(now)` in `serveFeed` for the source entering a slot, immediately after `feed.SetCurrent` (`handlers/websocket.go:1313`), type-asserted so every existing source is a no-op
- [ ] 4.4 Add `"timeline": true` to `KnownSourceTypes` (`datasource/playlist.go:30`) and extend `handlers/sources_parity_test.go` so the timeline key exists consistently in both catalogs
- [ ] 4.5 Feed tests: a timeline animates in-slot through the existing animated branch (`handlers/websocket.go:1497`), the first frame uses LKG, re-renders bypass it, skip is prompt, and pause stops ticking
- [ ] 4.6 Compatibility tests: with no timeline rows, pixel-art playback, transition ramps (`render/composite.go`), message shape, and health/LKG snapshots are unchanged

## 5. Admin CRUD & Preview Endpoints

- [ ] 5.1 Add `handlers/timelines.go` list/new/edit/update/delete handlers following `handlers/pixelart.go` validation style (flash + form re-render for bad input), including base resolution and bound checks from `render.ParseMotionTimeline`
- [ ] 5.2 Implement `POST /admin/timelines/preview` accepting the current form document plus `at_ms`, rendering through `TimelineDS` and returning a PNG; invalid documents return `400 {"error": ...}` like `AdminLayoutPreview` (`handlers/layout_editor.go:332`), session-authenticated like other previews
- [ ] 5.3 Wire routes `GET/POST /admin/timelines`, `/admin/timelines/new`, `/admin/timelines/:id/edit`, `/admin/timelines/:id/delete`, `/admin/timelines/preview` in `handlers/server.go` (next to the pixel-art/composition blocks) and add the sidebar nav entry
- [ ] 5.4 Add handler tests: CRUD round-trip, every 400 path (malformed JSON, over caps, bad easing/playback/property, unresolvable base), preview returns a decodable PNG at the requested tick, unauthenticated preview rejected, preview does not touch feed controllers
- [ ] 5.5 Add `MotionTimeline` to backup export and import (`handlers/backup.go:340` export block, import switch near `:987`) with secret-free round-trip, and extend `handlers/backup_test.go`

## 6. Timeline Editor UI

- [ ] 6.1 Add `web/frontend/timeline_editor.ts` state core mirroring `layout_editor.ts:146-182`: parse the hidden `tracks` input, `pushSnapshot`/`applySnapshot` with a 50-entry undo stack, `syncHiddenInputs`, and a `submit` listener (`layout_editor.ts:811`) so saved JSON always matches the visible document
- [ ] 6.2 Render the timeline surface: one lane per track, ruler with tick marks, draggable playhead, keyframe handles, add/delete via toolbar, drag-to-move with snapping, and property/easing pickers limited to the closed sets
- [ ] 6.3 Implement keyframe copy/duplicate (toolbar buttons and keyboard shortcuts) as single undo steps, plus delete and keyboard arrow nudging
- [ ] 6.4 Implement the debounced scrub preview: POST the unsaved form plus `at_ms` to `data-preview-endpoint` with single in-flight/latest-wins semantics (`layout_editor.ts:260`), swap the returned PNG, and show validation warnings from `400` responses
- [ ] 6.5 Add `web/templates/admin/timelines.html` and `web/templates/admin/timeline_editor.html` with the hidden inputs, lanes container, toolbar, and preview image; build via the existing Vite pipeline into `web/static/assets/timeline_editor.js`
- [ ] 6.6 Add a Playwright E2E: create a timeline, add and drag a keyframe, scrub the playhead and see the preview change, copy a keyframe, undo, save, and reload with the document intact
- [ ] 6.7 Verify keyboard/ARIA basics on the editor surface (focusable handles, labels, no pointer-only operations) and that an invalid form submission preserves values

## 7. Scene, Playlist & Text-Slide Attachment

- [ ] 7.1 Verify a scene action referencing `source_type: "timeline"` resolves through `resolveSceneSource` (`handlers/scenes.go:764`) and starts playback on activation; add a test that a `once` timeline restarts on re-activation
- [ ] 7.2 Verify playlist items round-trip `{"source_type":"timeline"}` through `ParsePlaylistItems` and play in the feed (`handlers/playlists.go`), with a test for the feed reaching the item
- [ ] 7.3 Verify a text slide resolves as a timeline base through the source index and renders with reveal applied
- [ ] 7.4 Add regression tests proving scenes/playlists that do not reference timelines behave byte-identically, and that no `Datasource`/`Animator`/`StateProvider` interface changed

## 8. GIF Import Conversion

- [ ] 8.1 Add a conversion path in `handlers/timelines.go` that reuses `runImportPipeline` (`handlers/media_import.go:425`) to create a Pixel Art base and a timeline with a `steps` `frame` track at cumulative GIF delays, honoring `maxGIFFrames`/`maxTargetGrid`/`maxPaletteSize`/upload caps
- [ ] 8.2 Add the convert action to the media import admin flow (`web/static/assets/media_import.js` + template wiring) and return the created timeline id
- [ ] 8.3 Tests: delays map to keyframes, over-cap GIF returns 400 without creating rows, and the converted Pixel Art still animates standalone with its original durations

## 9. Text Choreography (Reveal & State Hooks)

- [ ] 9.1 Add the optional `RevealRenderer` interface and implement it on `TextSlideDS` for exact character reveals; keep `Datasource` unchanged
- [ ] 9.2 Implement the reveal fallback pixel-column mask in `render/motion.go` for bases that do not implement `RevealRenderer`
- [ ] 9.3 Implement hook evaluation in `TimelineDS`: sample the base's `CurrentState`, detect value changes per hook, and run bounded `draw_in` (progressive reveal), `flash` (brightness/color envelope), and `tick` (periodic pulse) effects from the existing transform primitives
- [ ] 9.4 Validate hooks in `render.ValidateMotionTimeline`: cap 16, effect enum, bounded durations, known target, and null-safe behavior; 400-style errors on save/preview
- [ ] 9.5 Tests: exact reveal rendering on a text slide, masked fallback, flash/draw-in/tick determinism at the same tick, missing `StateProvider` inert, state-fetch error keeps last value, and no extra upstream fetch during an effect
- [ ] 9.6 Tests: skip mid-effect ends the slot promptly, transitions around a text timeline still use `render/composite.go` blends, and scene activation resets reveal/hooks

## 10. Multi-Panel Choreography & Budget Verification

- [ ] 10.1 Validate `x`/`y` against the logical canvas width (`render.PanelLogicalWidth`, `render/panel_canvas.go:14`) at save/preview, and confirm transforms run before `SlicePanelGapsPNG` in the feed (`handlers/websocket.go:1078`)
- [ ] 10.2 Add panel tests: an element crosses a bezel seam continuously on a 2–4 panel device, the sent frame is the exact physical size, single-panel devices remain byte-identical, and device previews slice identically (`HandleDevicePreviewWS`)
- [ ] 10.3 Add a deterministic golden suite that renders the same timeline document twice at the same tick across resolutions and panels and asserts byte equality, plus a benchmark bounding per-tick cost at the maximum caps
- [ ] 10.4 Confirm `render/mapping.go` needs no changes: gamma, color order, serpentine, and origin apply after motion exactly as for static frames

## 11. Wiring, Documentation & Verification

- [ ] 11.1 Verify `sceneSourceIndex`/`buildSourceIndex` load order and depth handling cannot recurse (`base_type == "timeline"` rejected) and that disabled timelines never resolve
- [ ] 11.2 Add the new capability and format limits to `README.md`/`CHANGELOG.md` and document the timeline JSON schema, tick semantics, and 400 error behavior for API users
- [ ] 11.3 Run `task pre-push` (gofmt, tests, build), the Vite build, and the Playwright suite; fix failures
- [ ] 11.4 Run `openspec validate add-motion-timeline --strict` and confirm it passes

## Reconciliation notes

- Timeline execution lives in `datasource/timeline.go` + `render/motion.go`; `handlers/websocket.go` only gains the additive `PlaybackStarter` call at slot start, so the feed loop and device protocol stay source-agnostic.
- Scenes, playlists, and text slides attach through the existing `<type>:<id>` source index; no `SceneActions`, `Datasource`, or `Animator` contract changes are required.
- Multi-panel motion uses the logical canvas already introduced by `panel-canvas`; `render/mapping.go` remains the only physical mapping step.
