## Context

Every LEDit source implements `Datasource.GetPNG(width, height) (*render.RenderedImage, error)` (`datasource/datasource.go:10`). The feed loop (`handlers/websocket.go` `serveFeed`) renders each source through the last-known-good cache (`handlers/websocket.go:1331`), sends one `{format, image, source, next}` message per slot, and, for sources implementing the `Animator` capability (`datasource/pixelart.go:19`), re-renders them inside the slot whenever `NextFrame(now)` changes (`handlers/websocket.go:1497`). Pixel art stores a palette plus frames with millisecond durations (`render/pixelart.go:15`), and `handlers/media_import.go` converts GIFs into that format with caps (`maxGIFFrames=64`, `maxTargetGrid=128`, 5 MB upload). `add-slide-transitions` blends whole frames at slot boundaries with pure progress-parameterized functions (`render/composite.go`). Multi-panel devices already render on a logical canvas (`render.PanelLogicalWidth`, `render/panel_canvas.go:14`) and slice bezel gaps at send time (`SlicePanelGapsPNG`, `handlers/websocket.go:1079`); `render/mapping.go` is the only physical frame mapping. `Composition` regions (`ent/schema/composition.go`) resolve children through `sourceIndex.Resolve` (`handlers/sources.go:618`), and scenes resolve actions to a fresh source instance on activation (`handlers/scenes.go:764`). The admin editor precedent is `web/frontend/layout_editor.ts`: hidden JSON inputs, a 50-deep undo snapshot stack, debounced preview POSTs, and a submit handler that syncs hidden inputs (`web/frontend/layout_editor.ts:146-182`, `:811`).

Constraints: one Go process, SQLite/Ent, server-rendered frames (no device JS), deterministic output for identical inputs/times, bounded per-tick work, and no change to the device protocol, existing pixel-art playback, or slide transitions.

Stakeholders: homelab operators authoring motion on one matrix or a chained wall; datasource authors who want draw-in/flash effects without new APIs.

## Goals / Non-Goals

**Goals:**
- Author motion as a versioned, bounded timeline document (tracks, keyframes, easing, playback) executed by the renderer.
- Keep motion deterministic and budgeted: same tick and same document produce byte-identical frames; per-tick work is capped by validation limits.
- Attach timelines to scenes, playlists, text slides, and composites through the existing source index without changing any existing datasource contract.
- Provide an admin timeline editor with scrub preview against the real renderer, copy/duplicate, and undo-safe form submission.
- Convert imported GIF frames into timelines where sensible.
- Choreograph across the logical panel canvas so elements can travel the wall; physical mapping stays in `render/mapping.go`.

**Non-Goals:**
- Client-side/device-side animation, APNG, or any WebSocket protocol change.
- Arbitrary scripting/expression languages or per-pixel particle systems; the property set is closed and typed.
- Real-time collaborative editing, video compositing, or alpha-channel rendering (LED frames are opaque RGB).
- Replacing `PixelFrameDoc` playback or the three slide transitions; timelines are additive.
- Per-panel coordinate addressing (v1 treats the wall as one logical canvas).

## Decisions

### D1 — One `MotionTimeline` entity with a versioned JSON `tracks` document and a wrapped base source

`ent/schema/motion_timeline.go` stores `name`, `enabled`, `schema_version` (default 1), `duration_ms`, `tick_ms`, `playback`, `background`, `base_type`, `base_id`, `tracks` (JSON text), `hooks` (JSON text), `created_at`, `updated_at`. This follows the `PixelArt`/`Composition` JSON-blob precedent (`ent/schema/pixelart.go`, `ent/schema/composition.go`) rather than normalizing tracks and keyframes into child tables.

- *Why:* A timeline is read/written as one unit by the editor; SQLite handles bounded JSON text fine; no join queries in the render path; `schema_version` makes additive format evolution safe.
- *Alternative:* `motion_track`/`motion_keyframe` tables — rejected: ent plumbing and per-tick queries for no benefit.
- *Alternative:* files on disk — rejected: the repo's CRUD, backup, and admin patterns are Ent-based.

### D2 — Closed, typed property set; coordinates in logical-canvas pixels

Supported properties: `x`, `y` (integer pixels on the logical canvas), `opacity` (0–100), `brightness` (0–100), `scale` (25–400 percent), `color` (palette index integer or `#rrggbb` tint), `frame` (integer index into the base's pixel frames when present), `reveal` (character count). Continuous properties interpolate; discrete properties (`frame`, `color` index, `reveal` with steps) snap at tick boundaries. Unknown property names are rejected at validation.

- *Why:* A closed set is testable, bounded, and covers the requested motion without an expression evaluator; typing lets the sampler quantize deterministically.
- *Alternative:* JSONPath/expression bindings per keyframe — rejected: unbounded input surface and per-tick eval cost for negligible expressiveness gain over named properties.
- *Alternative:* storing raw affine transforms — rejected: less debuggable, and users reason in pixels/percent.

### D3 — Tick-quantized deterministic sampling (`tick_ms` default 80)

Time is floored to `tick_ms` (valid 40–1000, default 80 ≈ 12.5 fps, aligned with the feed's ~50 ms skip poll and the 6–16 step transition budget). `NextFrame(now)` returns the integer tick index; easing is evaluated in float64 and quantized (1/1000) before pixel math; `x`/`y` are rounded to integer pixels; `opacity`/`brightness` are integer percent. `duration_ms` clamps total ticks to `ceil(duration_ms / tick_ms)`.

- *Why:* Two renders within the same tick must be byte-identical (the `add-slide-transitions` determinism requirement and the existing animation tests both assert reproducible frames); quantizing once at the sampling boundary keeps every transform integer-driven.
- *Alternative:* continuous wall-clock interpolation — rejected: sub-tick jitter and non-reproducible frames.
- *Alternative:* hard-coding 12 steps like the transition ramp — rejected: transition steps are a boundary effect; timelines need an author-visible tick that adapts to device refresh.

### D4 — Playback modes plus an additive slot-start hook

`playback` is `loop` (tick modulo total ticks), `once` (tick clamped to the last tick, so the final frame holds), or `ping_pong` (triangle wave without duplicated endpoints). A new optional interface:

```go
type PlaybackStarter interface{ StartAt(now time.Time) }
```

is called by `serveFeed` immediately after `feed.SetCurrent` (`handlers/websocket.go:1313`) for the source entering the slot, type-asserted, and is a no-op for every existing source. Scene activation needs no hook: `resolveSceneSource` (`handlers/scenes.go:764`) constructs a fresh `TimelineDS` per activation, so the instance's start time is the activation time. The editor preview passes an explicit sample time.

- *Why:* `Animator.NextFrame` has no slot lifecycle, and a `once` timeline must restart when its slot comes around; an optional interface is additive while extending `Animator` would break every existing implementer.
- *Alternative:* lazy start on first `GetPNG` plus an idle heuristic — rejected: fragile with pauses/skips and repeated rotations.
- *Alternative:* extending `Animator` with `Start` — rejected: contract break for `PixelArtDS`, `VisualizerDS`, `ScreensaverDS`.

### D5 — Execution split: `datasource/timeline.go` orchestrates, `render/motion.go` is pure

`TimelineDS` (implementing `Datasource`, `Animator`, optional `Ambienter`, and `StateProvider` passthrough) resolves the base through the shared source index, parses the document, samples tracks, and applies pure `render/motion.go` transform primitives to the base PNG. The transform order is fixed and documented: `frame` selection → `color`/tint → `scale` → `translate` → `brightness` → `opacity` → `reveal` mask. No device or frontend rendering.

- *Why:* Mirrors the established split where `datasource/pixelart.go` binds data and `render/pixelart.go` renders; keeps `render` pure/testable and the orchestrator small.
- *Alternative:* executing inside `serveFeed` — rejected: the feed loop should stay source-agnostic; timeline semantics belong to the datasource seam.
- *Alternative:* pre-rendering all frames at save — rejected: unbounded memory and incompatible with state-triggered effects.

### D6 — Opacity is compositing over an opaque declared background

`DecodeNRGBA` composites to opaque black and forces alpha 255 (`render/composite.go:16`); LED output has no translucency. `opacity` is therefore a per-pixel linear interpolation toward the timeline's `background` (default `#000000`), using the same lerp math as `BlendFade` (`render/composite.go:147`). The reveal mask uses the same background.

- *Why:* Deterministic, matches `BlendFade`/`ApplyBrightness` semantics, and avoids pretending the device supports alpha.
- *Alternative:* carrying an alpha channel through compositing — rejected: `render/mapping.go` and devices consume RGB only.

### D7 — Integer nearest-neighbor scale around a deterministic anchor

`scale` uses `scaleNearestNeighbor` (`render/composite.go:122`) so pixel edges stay crisp; the anchor is the canvas center by default, overridable per timeline by a keyframed origin (`origin_x`, `origin_y`) later if needed. Scale is integer percent applied as a float ratio to the base bounds with integer output dimensions.

- *Why:* Same rationale as the pixel-art renderer's integer scaling (`render/pixelart.go:132`); bilinear would blur at LED scale.
- *Alternative:* bilinear (`scaleBilinear`, `render/composite.go:63`) — deferred as an opt-in quality knob, not the default.

### D8 — Validation is a pure, bounded function; save shows a 400-style form error, APIs return 400 JSON

Caps: `MaxTracks=8`, `MaxKeyframesPerTrack=256`, `MaxHooks=16`, `MaxDurationMs=600000`, `tick_ms` 40–1000, `x`/`y` within ±4096, `scale` 25–400, `opacity`/`brightness` 0–100, closed `easing`/`playback`/`effect` enums, keyframes sorted, non-negative, at least one per track, and the base must resolve to a non-timeline source. `render.ParseMotionTimeline` mirrors `render.ParsePixelFrames`/`ValidatePixelDoc` (`render/pixelart.go:50`); form handlers follow `validatePixelArtForm` (`handlers/pixelart.go:42`) and `validateRegionConfig` (`handlers/layout_editor.go:63`); preview errors follow `AdminLayoutPreview`'s `400 {"error": ...}` (`handlers/layout_editor.go:332`).

- *Why:* Bounded documents are what make the per-tick budget provable; silent clamping hides misconfiguration.
- *Alternative:* validate only at render — rejected: invalid data would only surface in production frames.
- *Alternative:* no caps — rejected: a 10-minute, 64-track document would blow the tick budget.

### D9 — Timeline editor mirrors `layout_editor.ts` patterns

`web/frontend/timeline_editor.ts` renders lanes as DOM (one row per track, keyframes as handles), a ruler/playhead with scrub, drag-to-move with snapping, toolbar copy/duplicate/delete, and easing pickers. State is serialized into a hidden `tracks` input; `pushSnapshot`/`applySnapshot` keep a 50-entry undo stack; `form.addEventListener("submit", syncHiddenInputs)` makes saves atomic and undo-safe; a debounced POST to `data-preview-endpoint` sends the current unsaved document plus `at_ms` and swaps in the returned PNG (single in-flight, latest-wins), matching `doPreview` in `layout_editor.ts:260`.

- *Why:* No frontend framework or new npm dependency exists; DOM gives keyboard/ARIA behavior for free; the undo/submit/preview pattern is already battle-tested in the repo.
- *Alternative:* canvas-rendered timeline — rejected: harder accessibility and no capability gain.
- *Alternative:* a timeline npm package — rejected: supply-chain weight for a core-sized feature.

### D10 — Attachment through the existing source index, no new join edges

`buildSourceIndex` (`handlers/sources.go:302`) adds `timeline:<id>` entries resolving to a `TimelineDS` wrapping the base source resolved through the same index; `loadSources` (`handlers/websocket.go:129`) appends enabled timelines to the feed pool; `KnownSourceTypes` gains `timeline`. Scenes reference `source_type: "timeline"` via `SceneActions.SourceType` (already generic JSON) and playlist items store `{"source_type":"timeline","source_id":N}`. A timeline's `base_type` must not be `timeline` (depth 1), which keeps resolution and cost bounded. The catalog parity test (`handlers/sources_parity_test.go`) is extended so both catalogs stay consistent.

- *Why:* Scenes, playlists, and compositions already resolve arbitrary indexed sources (`handlers/sources.go:618`, `handlers/scenes.go:764`); adding a `SceneActions.TimelineID` would duplicate resolution and touch the scene contract.
- *Alternative:* dedicated scene/playlist columns — rejected: contract churn for a case the generic source reference already covers.

### D11 — GIF conversion emits a PixelArt base plus a `frame` track; no new decoder

The existing import pipeline (`runImportPipeline`, `handlers/media_import.go:425`) produces palette + frames + per-frame delays. A conversion action creates (or reuses) a `PixelArt` row from that result and a timeline whose base is `pixelart:<id>` and whose single `frame` track uses `steps` easing with keyframes at cumulative delay boundaries. All existing caps apply (`maxUploadBytes`, `maxGIFFrames`, `maxTargetGrid`, `maxPaletteSize`).

- *Why:* One decoder and dithering path; GIF delays are already milliseconds; the timeline becomes the single authored-motion format.
- *Alternative:* extend `PixelFrameDoc` with more motion fields — rejected: pixel-art playback must keep working unchanged, and the timeline format is versioned and richer.

### D12 — Multi-panel choreography uses the logical canvas; mapping is untouched

`serveFeed` already renders at `render.PanelLogicalWidth(width, panelCols, panelGap)` and slices bezels at send (`handlers/websocket.go:1078-1089`). Timeline `x`/`y` validate against the logical width supplied by the resolver, transforms run before `SlicePanelGapsPNG`, and `render/mapping.go` (gamma, color order, serpentine) remains the only physical step. Device previews (`HandleDevicePreviewWS`) inherit the same path, satisfying `panel-canvas` preview parity.

- *Why:* One coordinate system lets an element travel the wall continuously across bezels.
- *Alternative:* per-panel offsets — rejected: a chained wall is one logical surface; panel physics already lives in `render/mapping.go`.

### D13 — Cache, health, and ambient semantics reuse the animated-source path

`TimelineDS.FrameCount() > 1` routes it through the existing animated in-slot branch (`handlers/websocket.go:1497`): initial render goes through LKG (stale fallback preserved), re-renders bypass LKG and record health exactly like `PixelArtDS`. `Ambient()` returns true while the timeline can still change, so a composition containing a timeline re-renders (the `content-composition` ambient passthrough). No cache key or TTL changes.

- *Why:* Zero divergence from established animated-source behavior; the `add-pixel-maker-animator` LKG tests keep covering it.
- *Alternative:* a timeline-specific cache — rejected: duplicate cache semantics and more failure modes.

### D14 — Text reveal is exact when the base opts in, masked otherwise

A new optional interface:

```go
type RevealRenderer interface {
    GetPNGRevealed(width, height int, reveal int) (*render.RenderedImage, error)
}
```

`TextSlideDS` implements it so `reveal` counts characters exactly. For any other base, the renderer falls back to a deterministic pixel column mask computed from the simple-font metrics (`simpleCharW`, `render/render.go:290`), masking to the background.

- *Why:* Character-accurate reveal for text without changing `Datasource`; other bases degrade gracefully.
- *Alternative:* masking every base — rejected: a pixel mask mid-glyph looks broken on text, the primary use case.
- *Alternative:* adding `reveal` to `Datasource` — rejected: contract break for every source.

### D15 — State-triggered effects read `StateProvider`, not new per-datasource APIs

`hooks` JSON entries are `{name, path, effect: "draw_in" | "flash" | "tick", duration_ms, target}`. Each tick, `TimelineDS` samples the base's `CurrentState` (the existing optional interface, `datasource/datasource.go:16`), compares to its previous value, and maps `draw_in` to a reveal-progress envelope (chart area), `flash` to a brightness/`color` envelope, and `tick` to a periodic pulse. A base without `StateProvider`, a fetch error, or a missing path leaves the hook inert and keeps the last known values. All effects are composed from the same bounded transform primitives.

- *Why:* Chart draw-in, score flash, and countdown tick become data-reactable without modifying a single datasource contract; fakes make rendering tests trivial.
- *Alternative:* adding effect methods to datasource interfaces — rejected: every existing source would need changes and tests.
- *Alternative:* evaluating hooks in the feed loop — rejected: hooks are part of the timeline execution, not the feed.

## Risks / Trade-offs

- [Non-determinism from wall-clock sampling] → tick floor + integer/quantized math + golden tests that render the same document twice at the same tick and compare bytes.
- [Per-tick budget blow-up] → caps (D8) keep tracks ≤ 8 and keyframes ≤ 2048 total; transforms are O(canvas pixels × properties) with at most one buffer per transform; sampling is O(keyframes). No per-tick allocation of document structures.
- [LKG/first-frame pinning] → animated re-render path already bypasses the cache; only the initial frame is cached, preserving stale-fallback behavior.
- [Once-mode restarts across slots] → `PlaybackStarter.StartAt` at `feed.SetCurrent`; scenes get a fresh instance per activation; preview passes explicit time.
- [Base cycles or timeline nesting] → validate `base_type != "timeline"` and resolve bases through the depth-capped composition resolver.
- [Editor/server JSON drift] → hidden input is synced on submit and the server re-validates; preview reuses the same validator, so unsaved states surface 400s identically.
- [Regression of pixel art or transitions] → timelines are inert until an entity exists; `PixelFrameDoc` and `render/composite.go` are untouched; parity guards are extended, not replaced.
- [Reveal fallback looks approximate on non-text bases] → documented as a mask fallback; text bases use `RevealRenderer` (D14).

## Migration Plan

1. Add `ent/schema/motion_timeline.go`; run `go generate ./ent`; auto-migration is additive, existing rows unaffected.
2. Add `render/motion.go` (document types, `ParseMotionTimeline`, easing, sampler, transforms) with unit/golden tests; add `datasource/timeline.go` (`TimelineDS`), `RevealRenderer` on `TextSlideDS`, and the optional `PlaybackStarter` hook.
3. Add `handlers/timelines.go` (CRUD, preview, GIF conversion) and wire `/admin/timelines...` routes in `handlers/server.go`; add sidebar entries.
4. Index timelines in `buildSourceIndex`/`loadSources`, add `timeline` to `KnownSourceTypes`, and extend the catalog parity test.
5. Add `web/templates/admin/timelines.html`, `web/templates/admin/timeline_editor.html`, and `web/frontend/timeline_editor.ts`; build via the existing Vite pipeline (`web/static/assets/timeline_editor.js`).
6. Call the optional playback-start hook at slot start in `serveFeed`; verify the animated branch and transitions still behave (existing tests).
7. Rollback: delete/disable timelines; schema additions are additive and harmless; no device or protocol migration. Pixel art, transitions, and all existing feeds are unchanged without any timeline row.

## Open Questions

- Should `x`/`y` support a per-panel addressing mode in addition to logical-canvas pixels? Assumed logical canvas only for v1.
- Should hooks be configured per timeline or per base source? Assumed per timeline so no datasource contract changes.
- Should a finished `once` timeline advance the feed early? Assumed no — feed timing remains the configured timeout.
- How do palette-index `color` keyframes interact with pixel-art slot bindings? Assumed bindings resolve first, then the timeline applies a tint/index; documented in the spec.
- Should `ping_pong` repeat endpoint frames? Assumed a triangle without duplicated endpoints.
- Should timeline JSON be included in backup export/import in this change, or follow the existing backup pattern automatically? Assumed included via the standard entity registration.
