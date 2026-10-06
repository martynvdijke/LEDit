## Context

LEDit's ambient visual vocabulary is a set of closed draw functions wired into the feed one at a time:

- `render/screensaver/` — `DrawStarfield`, `DrawDVD`, `DrawMatrix`, `DrawPlasma(width, height, elapsed)` returning `*image.RGBA`; wrapped by `datasource.ScreensaverDS` (`datasource/screensaver.go:22`) and registered as the `screensaver:<variant>` sources in `handlers/sources.go:315` and `handlers/websocket.go:190-192`.
- `render/matrixrain.go` — `RenderMatrixRain(now, w, h)` with a per-column LCG (`newRainLCG`, line 53) and a 10 fps quantized step (line 23); wrapped by `datasource.MatrixRainDS` (`datasource/ambience.go:19`) and indexed as `matrix-rain:0`.
- `render/visualizer/` — `DrawBars`/`DrawSpectrum`/`DrawWave` seeded by now-playing metadata, plus the client-bin tap (`datasource/audio.go:54`); `VisualizerDS` is indexed as `audio:1`.

All palettes are literal: `render/visualizer/visualizer.go:26` hardcodes `#50fa7b`, `render/matrixrain.go:14-18` hardcodes the green rain palette, `render/screensaver/plasma.go:36-38` hardcodes its gradient. Parameters do not exist beyond the variant name; every new look is new Go code. The theme system (`render/theme.go`, `handlers/theme.go:46 ResolveTheme`, `handlers/theme.go:565 themeOverrideFrom`) already resolves an effective palette for every source and preview, but no animation consumes it.

The render loop that any engine must extend rather than fork:

- `serveFeed` (`handlers/websocket.go`) resolves the source, renders through the LKG cache (`defaultLKG`, lines 1330-1344), optionally ramps a transition (~6-16 steps at 40 ms, lines 1355-1425), applies the overlay (`applyOverlay`, line 1427), dims by brightness (line 1428), slices panel bezels (`slicePanels`, line 1433), and sends the frame. Animated sources re-render in-slot when `datasource.Animator.NextFrame` changes (`handlers/websocket.go:1497`, interface at `datasource/pixelart.go:20`), polling every 50 ms.
- Multi-panel devices render on the logical canvas (`render.PanelLogicalWidth`, `render/panel_canvas.go:14`) and are sliced to the physical frame at send time.
- The push transports re-enter the same renderers through `renderPushSourcePNG` (`handlers/output_runner.go:318`).
- Preview renders share the resolver and paths (`handlers/preview.go`, `AdminPreviewDatasource` at line 143); `render/visualizer` is the only animation with a hardcoded color, so theme correctness is a real gap.

Constraints from the existing system: deterministic time-based rendering for testability (`openspec/specs/ambience-modes`, "Deterministic time-based rendering"), themes resolved through the existing pipeline (`openspec/specs/theme-designer`), no new required runtime dependency, and an LED update cadence of roughly 15 fps in animation slots (66 ms `NextFrame` in `datasource/screensaver.go:67` and `datasource/audio.go:129`) with transitions running ~12 steps at the default (`README.md`, slide transitions).

Stakeholders: homelab operators who want richer ambient motion and control over it without writing Go, and maintainers who want new looks to be registry entries rather than new packages wired in three places.

## Goals / Non-Goals

**Goals:**
- One `Effect` interface and named registry in `render/effects` with a typed, bounded parameter model (palette, speed, intensity, density, direction, seed).
- Deterministic per-tick rendering at the target resolution, quantized to the existing ~66 ms animation cadence; byte-identical output for identical `(effect, params, seed, tick)`.
- Theme-driven color: every effect derives its colors from the effective `render.Theme`, resolved by the existing pipeline, with unsaved-theme preview parity.
- Reuse on every surface: standalone source, screensaver/animated slot, scene content, matrix cell, composition region, background/base layer, and the per-device overlay strip.
- Persisted presets (Ent) with admin CRUD, picker, parameter form, and live preview through the existing preview endpoints.
- Documented, benchmark-enforced per-frame budgets at 64×32, 128×64, and 256×128 with buffer reuse (no per-frame scratch growth).
- Existing screensaver, visualizer, and matrix-rain behavior byte-identical unless explicitly opted into the engine; no WebSocket protocol change; no new dependency.

**Non-Goals:**
- A shader/GLSL runtime, user-uploaded effect code, or WASM.
- Audio/spectrum-reactive effects (owned by the in-flight `audio-visualizer` capability) and real audio capture.
- Re-implementing or re-routing the existing `screensaver:*`, `matrix-rain:0`, or `audio:1` sources; they stay on their current code paths.
- Vertical/chained panel topologies beyond what `panel-canvas` already does.
- A general alpha-blending compositor: LED frames are opaque, so "background layer" means an effect occupying the base layer (full-canvas source or composition/panel region), not alpha-compositing under arbitrary content.
- TRMNL e-ink rendering of animated effects (e-ink polls static images; effects are WS/push-only).
- New persistence for per-device effect tuning beyond the overlay strip reference (v1).

## Decisions

### D1 — `render/effects` is a leaf rendering package with a registry and a descriptor-driven parameter model

The engine lives in `render/effects/` (mirroring `render/screensaver/`) and imports only the standard library (`image`, `image/color`, `math`, `hash`), never `ledit/render` or `ledit/datasource`. That keeps it testable in isolation and lets both `render` (overlay composition) and `datasource` (source adapters) consume it without an import cycle.

```go
// render/effects
type Params struct {
    Palette   Palette   // Background, Accent, Text (color.RGBA)
    Speed     float64   // 0..100, default from descriptor
    Intensity float64   // 0..100
    Density   float64   // 0..100
    Direction string    // "up" | "down" | "left" | "right" | "none"
    Seed      int64
}

type Effect interface {
    Name() string
    Draw(dst *image.RGBA, p Params, elapsed time.Duration)
}

type Descriptor struct {
    Name     string
    Defaults Params
    Bounds   ParamsBounds // per-field min/max + direction enum
}

func Register(d Descriptor, f Effect)
func Lookup(name string) (Descriptor, Effect, bool)
func Names() []string // sorted, stable for pickers and tests
func Validate(name string, p Params) error
```

Initial built-ins: `plasma` (sin field, generalized from `render/screensaver/plasma.go`), `fire` (upward heat/dither field), `sparkle` (sparse particles at density), `scan` (sweep/scanline with direction), `noise` (deterministic value-noise field), `gradient` (drifting palette field), plus adapters `legacy-matrix-rain`, `legacy-plasma`, `legacy-starfield`, `legacy-dvd` that call the existing draw functions and are byte-identical for the same elapsed/dimensions. Names are the stable identifier in params JSON and admin routes.

Why: one interface, one registry, one validation path means a new look is a `Register` call and a preset row — no changes to `handlers/websocket.go`, source lists, or preview code. Alternatives: (a) extend `datasource.Datasource` directly — rejected, mixes rendering and IO lifecycle, and `Datasource.GetPNG` has no time parameter so determinism would be lost; (b) put effects in `render` proper — rejected, `render/screensaver` sets the precedent for a leaf subpackage and avoids growing an already large package; (c) hardcode a variant switch — rejected, that is the current state this change replaces.

### D2 — Parameters are typed, bounded, and validated at save time; rendering clamps for safety

`Speed`, `Intensity`, and `Density` are 0-100 percentages. `Direction` is a closed enum. `Seed` is a signed 64-bit integer; zero means "derive from the preset name hash" (the existing `newRainLCG` already substitutes `0x9e3779b9` for a zero seed, `render/matrixrain.go:58`). `Validate` rejects unknown effects, out-of-range values, and unknown directions; the admin form and `EffectPreset` write path call it, so a bad preset is never stored. `Draw` clamps defensively so a hand-edited database row or a future registry default change cannot produce out-of-bounds drawing. Params serialize as JSON with a `"v"` schema version for forward compatibility.

Why: the theme stack already validates at the point of save and rejects with field-level errors (`openspec/specs/theme-designer`, "Named theme model and validation"); presets should behave identically. Alternatives: free-form map — rejected, no validation and typo-prone; clamping only at save (no render-side clamp) — rejected, a DB edit could panic the feed.

### D3 — Rendering is a pure function of quantized tick, seed, params, and effect; no wall clock inside

`Effect.Draw` receives `elapsed time.Duration`, and the caller quantizes it to a tick: `tickInterval = 1000/15 ms`, matching the existing `NextFrame` cadence (`datasource/screensaver.go:67`) and the ~12-step transition cadence. Randomness comes from the existing LCG construct (`newRainLCG`, `render/matrixrain.go:53`) re-seeded per column/pixel group as `hash(effect, seed, tick, x, y)`, never from `math/rand`'s global source or `time.Now()`. Consequence: the same tick renders byte-identical frames in feed, preview, and push paths, and golden tests can hash frames.

Why: `openspec/specs/ambience-modes` requires deterministic time-based rendering and the existing screensaver tests assert reproducible hashes (`render/screensaver/screensaver_test.go:22`). Alternatives: unquantized wall-clock — rejected, preview and feed would differ and tests would flake; `math/rand` global — rejected, race-prone and non-reproducible.

### D4 — `effects.Renderer` reuses one scratch buffer per instance; steady state allocates no scratch

`Renderer{eff Effect; img *image.RGBA}` exposes `Frame(width, height int, p Params, elapsed time.Duration) *image.RGBA` and lazily (re)sizes `img` only when the bounds change, then calls `Draw` in place. The caller must consume the frame before the next call (PNG encoding copies into the output buffer, so the existing `png.Encode` path is safe). Each `EffectDS` and each overlay composition owns a renderer, so connections never share a scratch buffer and no locking is needed. A benchmark asserts allocation counts stop growing after warm-up.

Why: the feed loop runs at 15 fps per animated source and allocations were the first failure mode called out for the existing variants (~4.2k allocs/op at 64×64 in the baseline below). Alternatives: allocate per frame — rejected; pooled global buffers — rejected, cross-connection lifetime hazards and locks for no benefit.

### D5 — Theme palettes are mapped at the edge; effects contain no literal brand colors

`effects.Params.Palette` carries `Background`, `Accent`, and `Text` roles. The adapter layer maps `render.Theme` → `effects.Palette` (background/accent/text), and effects build every shade by interpolating or scaling those roles, exactly like the existing matrix-rain trail gradient interpolates its endpoints (`render/matrixrain.go:100`). Effective-theme resolution is the existing one: per-source override → global default → built-in default (`handlers/theme.go:53`), and previews pass unsaved tokens through `themeOverrideFrom` (`handlers/theme.go:565`). The LKG cache key already fingerprints the theme (`themeCacheSig`, `handlers/theme.go:615`), so a palette change never serves a stale frame. Legacy sources keep their own palettes; only effects entered through the engine (or a legacy adapter explicitly configured with a palette) take the theme.

Why: the change's core promise is theme integration; a literal color inside the engine would silently break it. Alternatives: per-effect fixed palettes — rejected, that is the status quo; adding color fields to each preset — rejected, presets should store behavior, not colors, so retheming an install updates every effect.

### D6 — One adapter reaches every surface; composition happens in existing seams

`datasource.EffectDS` implements `Datasource`, `ThemedRenderer` (`datasource/datasource.go:39`), `Animator` (`datasource/pixelart.go:20`), and `Ambienter` (`datasource/datasource.go:23`). It is registered per preset as `effect:<presetID>` in `buildSourceIndex` (`handlers/sources.go:302`), the WebSocket source list (`handlers/websocket.go:184-192`), and `bindingOptions` (`handlers/sources.go:66`). From there every existing surface works without further plumbing:

- **Standalone source / slide**: a playlist or global-list entry `{source_type:"effect", source_id:<id>}`.
- **Screensaver mode**: effects are ordinary sources, so an off-hours playlist can select one; `idle_screensaver` keeps its legacy variants untouched.
- **Scene content**: `resolveSceneSource` resolves through `sourceIndex` (`handlers/scenes.go:769`), so a scene action with `source_type:"effect"` works unchanged.
- **Matrix cell / composition / panel**: `MatrixDS` and `CompositorDS` resolve children through the same index (`handlers/sources.go:574`, `datasource/compositor.go:51`); `Ambienter` keeps cells animating past the panel TTL cache (`datasource/datasource.go:22`); rendering happens at the cell/region size and at the logical canvas width for bezel devices, with the existing slice untouched.
- **Background/base layer**: an effect bound as a full-canvas source or as a first, full-span composition region. Documented as the v1 pattern; no new compositor machinery.
- **Overlay strip**: `render.OverlaySpec` gains optional effect fields (`EffectName`, `EffectParams`, `EffectSeed`); when set, `CompositeOverlayPNG` (`render/overlay.go:85`) renders the effect into the strip before the text. When unset, the function returns the input unchanged exactly as today. Device/group overlay rows gain a nullable `overlay_effect_preset_id` (additive, default null), resolved through `overlaySpecForDeviceWithGroup` (`handlers/effective_policy.go:86`) so preview and live devices match.

Why: the render loop and protocol already have the right seams; adding a second registry alongside `sourceIndex` would fork resolution. Alternatives: a bespoke overlay pipeline — rejected, `CompositeOverlayPNG` already decodes/draws/re-encodes once per overlay-enabled frame; a new `background` device column — deferred (non-goal for opaque content).

### D7 — Presets are immutable-able artifacts in Ent, seeded like built-in themes

`ent/schema/effectpreset.go`: `name` (unique, non-empty), `effect` (validated against `effects.Names()`), `params` (JSON text with schema version), `built_in` (bool), `created_at`, `updated_at`. Startup seeds a small built-in set (for example "Plasma", "Ember", "Sweep", "Static") when none exist; built-ins are immutable and "duplicate to edit" like built-in themes (`openspec/specs/theme-designer`, "Built-in themes"). Deleting a user preset follows the existing dangling-reference tolerance: playlists and bindings that reference it degrade through the normal unresolved-source path rather than failing the feed (`openspec/specs/matrix-grid-layout`, "Unresolved binding tolerance").

Why: "stored as artifacts" means the tuned effect survives restarts and is selectable everywhere without per-surface config; Ent + SQLite is the existing persistence path and `theme`/`matrixlayout` set the schema pattern. Alternatives: store params in playlist JSON — rejected, not reusable and not editable once referenced; a config file — rejected, the project is DB-first and auto-migrates.

### D8 — Admin authoring reuses the preview family and the debounced theme-editor pattern

Routes: `GET /admin/effects` (list), `GET|POST /admin/effects/new`, `GET|POST /admin/effects/:id/edit`, `POST /admin/effects/:id/delete`, and preview via a new `POST /admin/preview/effect` that mirrors `AdminPreviewDatasource` (`handlers/preview.go:143`) — it builds an `EffectDS` from unsaved form values and renders it, never touching the database. The form populates the effect dropdown from `effects.Names()` plus each `Descriptor.Defaults/Bounds`, shows palette roles from the selected/effective theme, and debounces preview at ~300 ms like `web/frontend/theme_editor.ts:42`. Picker entries appear in `bindingOptions` under an `effect` group, so the layout editor, playlist editor, and composition editor inherit them. Session auth is the existing admin middleware; no device-facing surface changes.

Why: preview parity is an explicit existing contract (`openspec/specs/admin-live-preview`) and the theme editor already demonstrates the exact debounce/unsaved-values behavior. Alternatives: preview only saved presets — rejected, tuning would be save-and-guess; a separate preview protocol — rejected, duplicates auth, sizing, and format handling.

### D9 — Performance guardrails: measured baseline, per-resolution budgets, tick quantization, allocation checks

Reference measurements on the dev host (AMD Ryzen 9 5900X, Go 1.27, `go test -bench`):

| Operation (64×64 draw; PNG encode at size) | Time |
| --- | --- |
| `DrawStarfield` 64×64 | ~62 µs |
| `DrawDVD` 64×64 | ~68 µs |
| `DrawMatrix` 64×64 | ~80 µs |
| `DrawPlasma` 64×64 | ~347 µs (~85 ns/pixel) |
| PNG encode 64×32 (synthetic gradient) | ~196 µs |
| PNG encode 128×64 | ~327 µs |
| PNG encode 256×128 | ~933 µs |

Budgets (draw pass only, reference host, with CI margin): ≤ 0.5 ms at 64×32, ≤ 2 ms at 128×64, ≤ 6 ms at 256×128. The 256×128 budget is above the plasma extrapolation (~2.8 ms) and stays under 10% of the 66 ms tick, so encode and network remain the dominant per-frame costs. Each built-in gets `BenchmarkEffect<Name>` at the three resolutions with `b.ReportAllocs()`; a guard test fails when a benchmark exceeds budget by more than 2× (generous CI margin) and an `AllocsPerRun` check asserts steady-state scratch growth is zero after warm-up. Determinism is protected separately by golden hash tests across ticks and seeds.

Why: scope explicitly requires bounded per-frame cost at common resolutions with no allocation storms, and the numbers above show why (plasma already costs more than encode at 64×64). Alternatives: unbounded "fast enough" claims — rejected, this is the regression class the change is meant to prevent; a fixed global cap across resolutions — rejected, cost is O(pixels), so budgets must scale with area.

### D10 — Extend, don't fork: no render-loop or protocol changes beyond guarded branches

`serveFeed` keeps its structure. `EffectDS` enters through the existing `Animator` branch (`handlers/websocket.go:1497`); LKG, health, transitions, overlay, brightness, and panel slicing are unchanged. The only new send-time branch is the optional overlay effect inside `CompositeOverlayPNG`, guarded by config and defaulting off, exactly like the existing overlay feature. The WebSocket message shape (`{format, image, source, next}`) and the device protocol are untouched. No `go.mod` change.

Why: the constraint is explicit, and the seams already exist; a second render loop would double the maintenance surface for LKG, brightness, and bezel slicing. Alternatives: a dedicated effect ticker per source — rejected, the animator path already polls at 50 ms and re-renders only when the tick index changes; a new WS frame type — rejected, unnecessary.

## Risks / Trade-offs

- [Plasma-like effects at 256×128 exceed the encode budget] → Per-resolution budgets + benchmark guard; tick quantization caps work at 15 fps; density/bounds defaults tuned per effect.
- [Allocation churn in the feed loop] → Per-instance scratch reuse (`effects.Renderer`), `ReportAllocs` benchmarks, and a steady-state allocation test.
- [Theme change serves a stale cached frame] → The existing LKG key already includes `themeCacheSig` (`handlers/theme.go:615`); effect params join the datasource config signature.
- [Non-determinism between feed and preview] → Quantized tick, seeded LCG, no wall clock inside `Draw`; golden hash tests; preview builds the same `EffectDS`.
- [Legacy behavior regresses] → Existing sources stay on their current code paths; golden tests for `render/screensaver`, `render/matrixrain`, and the visualizer remain untouched and must stay green; byte-identical overlay behavior when no effect is configured.
- [Preset deleted while referenced] → Dangling-reference tolerance matches existing matrix/playlist behavior; the admin UI warns on delete when references exist.
- [Param semantics drift across versions] → `"v"` schema version in the params JSON and registry-owned defaults/bounds; unknown future fields ignored on read.
- [Overlay strip effect hides text] → Effect renders behind the text in the strip; contrast is the operator's choice via theme roles, and disabling the effect restores byte-identical output.
- [Admin preview load] → Debounce (~300 ms) plus the existing preview isolation rules (no feed state, no display analytics).

## Migration Plan

1. Land `render/effects` (registry, params, built-ins, legacy adapters) with unit, golden, and benchmark tests; nothing imports it yet, so the feature is inert.
2. Add `datasource/effect.go` (`EffectDS` + theme mapping) and register presets as `effect:<id>` in `buildSourceIndex`, the WS source list, and `bindingOptions`; effects only appear once a preset exists.
3. Add `ent/schema/effectpreset.go`, run `go generate ./ent`, seed built-in presets, add `/admin/effects` CRUD and sidebar entry; auto-migration is additive.
4. Add the optional overlay effect fields (`OverlaySpec`, preview, device/group columns nullable default null) and verify disabled devices are byte-identical.
5. Add `POST /admin/preview/effect`, the effect form template, and `web/frontend/effect_editor.ts`; verify preview parity against the feed path.
6. Update docs (`README.md`, admin help text), run `task pre-push` (gofmt, tests, build) and `openspec validate add-effects-library --strict`.
7. Rollback: remove `effect:<id>` entries from playlists/global list and unset overlay effect references; legacy sources never depended on the engine, the schema additions are additive, and no protocol or dependency changes need reverting.

## Open Questions

- Should built-in presets be immutable-seeded or editable defaults? Assumed seeded immutables with "duplicate to edit", matching `theme-designer`.
- Should one preset carry per-surface speed overrides (full-screen vs cell)? Assumed no for v1; resolution is handled automatically and speed stays preset-wide.
- Should legacy screensaver variants be re-implemented on the engine immediately? Assumed no; adapters expose them byte-identically and new effects are added natively.
- Should the effect tick be a global constant or per-effect? Assumed a global 66 ms tick for parity with existing animations, with effects allowed to internally quantize slower.
- Should the overlay strip effect be per-device only or also per-group? Assumed both through the existing device/group overlay precedence (`handlers/effective_policy.go:86`), additively and default-null.
