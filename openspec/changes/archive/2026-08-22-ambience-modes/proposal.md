# Change Proposal: ambience-modes

## Why

LEDit's wall is an *information* device — every frame is data. But between datasources, or in rooms where you just want something alive on the wall, there's no option to show art, a clock, or a countdown. The render-upgrades change plans a digital clock and weather particles; this change fills the rest of the ambience space: an analog clock, matrix rain, and configurable countdown timers — all pure render, no datasource required.

## What Changes

1. **Analog clock** — a built-in, no-config source that renders a pixel-art analog clock (12-hour dial, hour/minute/second hands) with a digital readout strip. Time-based hand positions mean the existing feed loop's re-renders produce a ticking animation for free. Available in the feed source list and as a matrix cell binding type.

2. **Matrix rain** — a built-in, no-config source that renders the classic green digital rain (falling katakana/glyph columns). Deterministic pseudo-random based on `(frame, column)` so re-renders animate smoothly and stay cheap; green-on-black to match the LED aesthetic. Same availability as the clock.

3. **Countdown timers** — a new `Countdown` ent entity (name, target time, optional label, enabled). Renders `label` + remaining time (`12d 04:33:07`) against a theme background, ticking every feed cycle. Full CRUD + admin form + sidebar link, following the textslide datasource pattern.

4. **Ambience in matrix cells** — analog clock and matrix rain register as matrix cell binding types (like the planned `clock` binding in render-upgrades), so a dashboard cell can show rain while others show data. Countdowns bind by id.

5. **First-class feed citizens** — built-in ambience sources appear in the feed source list between configured sources (like the existing always-available `System Stats` built-in at websocket.go:115). Ordering relative to configured sources stays under the existing random/sequential settings.

## Capabilities

### New Capabilities
- `ambience-modes`: built-in ambient render sources (analog clock, matrix rain) and user-configured countdown timers, available in the feed and matrix cells.

### Modified Capabilities
- (none — existing capabilities keep their requirements; this is additive)

## Impact

- New render functions in `render/` (`analogclock.go`, `matrixrain.go`, `countdown.go`) using the existing pixel-font/panel infrastructure; new `datasource/ambience.go` with `AnalogClockDS`, `MatrixRainDS` (both no-config) and `CountdownDS`.
- New `Countdown` ent entity + edges from `GeneralSettings`, full CRUD (5 admin routes), sidebar link, dashboard count, feed wiring via `handlers/datasource_registry.go` + `loadSources` (websocket.go).
- Matrix cell binding options (from matrix-dashboard-rendering) gain `analog-clock`, `matrix-rain` (built-in, id 0) and `countdown:<id>` types.
- No new dependencies (all render code, stdlib time/math/rand). Feed message format unchanged.
- Complement of render-upgrades: that change owns the digital clock + weather particles; this change owns analog clock, matrix rain, and countdowns. No overlap by design.
