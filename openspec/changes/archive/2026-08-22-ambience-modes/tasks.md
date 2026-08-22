## 1. Render Primitives

- [x] 1.1 Add `render/analogclock.go`: pure `RenderAnalogClock(now, width, height) (*RenderedImage, error)` with Bresenham hands + 12/3/6/9 dots + digital strip; injected time param
- [x] 1.2 Add `render/matrixrain.go`: pure `RenderMatrixRain(now, width, height)` with deterministic per-column LCG, head/trail palette, precomputed glyph table
- [x] 1.3 Add `render/countdown.go`: pure `RenderCountdown(label, target, now, width, height)` with `Xd HH:MM:SS` / `HH:MM:SS` / `MM:SS` / DONE formatting
- [x] 1.4 Unit tests: analog hand angles at fixed times, rain determinism (same time → same frame), countdown formatting boundaries (24h/1h/DONE), 32×32 and 64×64 renders succeed
- [x] 1.5 Add a render benchmark test guarding per-frame allocations

## 2. Ambience Datasources

- [x] 2.1 Add `datasource/ambience.go`: `AnalogClockDS` and `MatrixRainDS` implementing `Datasource.GetPNG` (no-config, delegate to render funcs)
- [x] 2.2 Add `datasource/countdown.go`: `CountdownDS` reading target/label, delegating to `RenderCountdown`

## 3. Countdown Entity + CRUD

- [x] 3.1 Add `ent/schema/countdown.go`: name, target_time, label, enabled + edge from `GeneralSettings`; run codegen
- [x] 3.2 Register `Countdown` in `handlers/datasource_registry.go` with full CRUD (5 admin routes), sidebar link, dashboard count
- [x] 3.3 Add countdown admin form template (name, datetime-local target, label, enabled) with live preview wiring
- [x] 3.4 Playwright test: countdown CRUD flow + form shows target/done state

## 4. Feed + Matrix Integration

- [x] 4.1 Append `AnalogClockDS` and `MatrixRainDS` in `loadSources` (websocket.go) next to `SystemStatsDS`
- [x] 4.2 Append enabled countdowns in `loadSources` labeled `Countdown: <name>`
- [x] 4.3 Add `analog-clock:0`, `matrix-rain:0`, `countdown:<id>` binding options to matrix cell source selection (`BINDING_OPTS` in web/frontend/app.ts) and resolve them in the matrix cell render path
- [x] 4.4 Verify matrix grid preview renders ambience cells through the shared preview endpoint

## 5. Verification

- [x] 5.1 Run `task pre-push` (gofmt, tests, build) and fix failures
- [x] 5.2 Confirm feed message format unchanged and no new dependencies
