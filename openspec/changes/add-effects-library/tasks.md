## 1. Effect Engine Core (`render/effects`)

- [ ] 1.1 Create `render/effects/params.go` with `Params` (palette roles, speed/intensity/density 0–100, direction enum `up|down|left|right|none`, seed), the `Effect` interface, `Descriptor` (defaults + bounds), and versioned JSON serialization
- [ ] 1.2 Create `render/effects/registry.go` with `Register`, `Lookup`, `Names` (sorted, stable), and `Validate(name, params)` returning field-level errors for unknown effects, out-of-range values, and invalid directions; render-time clamping helper
- [ ] 1.3 Create `render/effects/renderer.go` with a buffer-reusing `Renderer` (`Frame(width, height, params, elapsed)`), lazy scratch resize, and the shared 66 ms tick quantization helper
- [ ] 1.4 Implement built-ins: `plasma.go` (sin field generalized from `render/screensaver/plasma.go`), `fire.go`, `sparkle.go`, `scan.go` (sweep with direction), `noise.go` (deterministic value noise), `gradient.go` (drifting palette field); all colors derived from `Params.Palette` only
- [ ] 1.5 Implement legacy adapters `legacy-matrix-rain`, `legacy-plasma`, `legacy-starfield`, `legacy-dvd` delegating to the existing functions (`render.RenderMatrixRain`, `screensaver.DrawPlasma`/`DrawStarfield`/`DrawDVD`) with byte-identical output for the same elapsed/dimensions
- [ ] 1.6 Add unit tests: registry contents/order, validation errors, parameter round-trip, defensive clamping, bounds safety at 1×1 and non-square sizes, palette sensitivity (different palette → different frame), determinism across call order
- [ ] 1.7 Add golden hash tests (following `render/screensaver/screensaver_test.go`) that pin per-effect frames per tick and seed and assert different ticks/seeds differ
- [ ] 1.8 Add `BenchmarkEffect*` at 64×32, 128×64, and 256×128 with `b.ReportAllocs()`, plus a guard test that fails when a built-in exceeds its documented budget beyond the CI margin
- [ ] 1.9 Add a steady-state allocation test (`AllocsPerRun` warm vs steady) and a `-race` test with concurrent independent renderers

## 2. Datasource Adapter & Existing Surfaces

- [ ] 2.1 Create `datasource/effect.go` with `EffectDS` implementing `Datasource`, `ThemedRenderer` (`GetPNGThemed`), `Animator` (`FrameCount`, `NextFrame` on the 66 ms tick), and `Ambienter`
- [ ] 2.2 Map `render.Theme` → `effects.Params.Palette` in the adapter (background/accent/text) and render at the requested width/height; zero network access and always-healthy behavior
- [ ] 2.3 Register each preset as `effect:<presetID>` in `buildSourceIndex` (`handlers/sources.go`), the WS source list (`handlers/websocket.go`), and `bindingOptions`, with preset-name labels
- [ ] 2.4 Include effect params in the datasource config signature and rely on `themeCacheSig` in the LKG key; verify animated re-renders follow the `handlers/websocket.go:1497` Animator branch and panel cells bypass the TTL cache via `Ambienter`
- [ ] 2.5 Add tests: effect source appears in feed/picker, playlist item round-trips, frames change across ticks during a slot, deleted preset degrades through unresolved-source tolerance, health stays green
- [ ] 2.6 Add integration tests that bind an effect to a matrix cell, a composition region, and a scene action (`resolveSceneSource`) and assert render + animation at cell/region size
- [ ] 2.7 Verify logical-width rendering (multi-panel device) renders the effect at `render.PanelLogicalWidth` and the existing gap slicing output is unchanged

## 3. Effect Preset Persistence

- [ ] 3.1 Add `ent/schema/effectpreset.go` (`name` unique/non-empty, `effect`, `params` JSON with version, `built_in`, `created_at`, `updated_at`) and run `go generate ./ent`
- [ ] 3.2 Seed built-in presets on startup when none exist (immutable, duplicate-to-edit), following the built-in theme seeding pattern; verify seeding is idempotent across restarts
- [ ] 3.3 Implement preset CRUD and validation in `handlers/effects.go` reusing `effects.Validate`, with duplicate-name and unknown-effect rejection
- [ ] 3.4 Wire admin routes (`/admin/effects`, `/admin/effects/new`, `/admin/effects/:id/edit`, delete) and a sidebar entry
- [ ] 3.5 Add tests: create/read/update/delete, duplicate name/effect/params rejection, built-in immutability and duplication, delete leaves referenced sources resolvable through tolerance

## 4. Admin UI & Live Preview

- [ ] 4.1 Add `web/templates/admin/effects.html` and `effect_form.html` with an effect select populated from `effects.Names()` descriptors and controls for speed, intensity, density, direction, and seed
- [ ] 4.2 Add `web/frontend/effect_editor.ts` with a ~300 ms debounced preview (mirroring `theme_editor.ts`) and palette role display from the selected/effective theme
- [ ] 4.3 Add `POST /admin/preview/effect` mirroring `AdminPreviewDatasource`: builds an `EffectDS` from unsaved form values, never touches the DB, honours `themeOverrideFrom`, and returns PNG/WebP through the existing writer
- [ ] 4.4 Ensure pickers (playlist, matrix, composition, schedule) list the effect preset group and that layout/composition editors can select presets
- [ ] 4.5 Add tests: unauthenticated rejection on pages/endpoints/preview, unsaved values reflected in preview, debounce behavior, preview does not mutate feed state, preview failure keeps form values

## 5. Overlay & Background Composition

- [ ] 5.1 Extend `render.OverlaySpec` with optional effect fields and render the effect inside `CompositeOverlayPNG` behind the text; no effect configured returns the input unchanged (byte-identical)
- [ ] 5.2 Add nullable `overlay_effect_preset_id` to device and group settings (additive default null), parse/validate in the form handlers, and `go generate ./ent`
- [ ] 5.3 Resolve the preset in `overlaySpecForDeviceWithGroup` (`handlers/effective_policy.go`) so live feed and device-accurate preview composite the same strip effect
- [ ] 5.4 Add tests: overlay with no effect byte-identical, effect renders behind text, brightness dim applies to the strip effect, animated frames carry the effect, preview parity
- [ ] 5.5 Document and test the background/base-layer pattern: an effect as a full-canvas source or first full-span composition region, with no compositor geometry changes

## 6. Performance & Compatibility Verification

- [ ] 6.1 Run the effect benchmarks and record measured numbers for 64×32, 128×64, and 256×128 in the change notes; tune defaults if a built-in exceeds budget
- [ ] 6.2 Confirm the budget guard and allocation-growth tests fail when thresholds are tightened (sanity-check the guards are real)
- [ ] 6.3 Re-run the existing golden tests for `render/screensaver`, `render/matrixrain`, and the visualizer and confirm hashes/behavior are unchanged
- [ ] 6.4 Verify `go.mod` has no new direct requirement and that no device-facing route or WebSocket message shape changed

## 7. Wiring, Docs & Verification

- [ ] 7.1 Update `README.md` (effects library, presets, overlay effect, performance notes) and admin help text
- [ ] 7.2 Run `task pre-push` (gofmt, tests, build) and fix failures
- [ ] 7.3 Run `openspec validate add-effects-library --strict` and confirm it passes
