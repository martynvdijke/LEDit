# Design: ambience-modes

## Context

`Datasource` is `GetPNG(width, height)`. `SystemStatsDS` is the precedent for a no-config built-in: constructed inline in `loadSources` (websocket.go:115), always present, no DB row. `render.RenderText` / `render.RenderDict` are the render entry points; `fonts/PixelifySans.ttf` is the pixel font. The matrix binding system (matrix-dashboard-rendering) maps cells to `source_type:source_id` pairs; render-upgrades introduces a `clock` built-in binding (id 0) that resolves without the DB. `serveFeed` re-renders each source every cycle at a fixed timeout, so time-varying frames animate at wall refresh rate.

## Goals / Non-Goals

**Goals**
- Three ambient modes: analog clock, matrix rain, countdown timers.
- Built-ins need zero config and appear in feed + matrix cells; countdowns are full CRUD entities.
- Animation emerges from the existing re-render loop (deterministic, time-based), never from extra render calls or per-frame state beyond time.

**Non-Goals**
- No digital clock or weather particles (owned by render-upgrades).
- No audio, no interactive/touch modes, no screen-saver auto-activation logic (ambience sources are picked like any other source; auto-idle activation is a future change).
- No new animation engine or tweening — animation is strictly time-indexed re-renders.

## Decisions

### 1. All ambience renders are pure functions of time
Each ambience renderer takes `time.Now()` (or an injected clock for tests) and computes its frame deterministically:
- **Analog clock**: hand angles from hour/min/sec; seconds hand advances every render. Hand drawn as Bresenham lines on a small canvas scaled to width/height; dial dots at 12/3/6/9. Digital strip below via `render.RenderText`-style pixel font.
- **Matrix rain**: per-column falling head position = `f(timeStep, col)` with a deterministic hash (`fnv` or LCG seeded by column); glyphs from a fixed katakana-ish set rendered as pixel blocks. Head bright, trail fading. Frame step derived from `(unixSeconds * fpsRate) % trailLength` so refresh-rate changes don't break the animation.
- **Countdown**: remaining = `target - now`; renders label + `Xd HH:MM:SS` (or `HH:MM:SS` under 24h, `MM:SS` under 1h). Past target → "DONE" state with a blinking indicator.
- **Testability**: all three accept an injected `now func() time.Time` (or `time.Time`) — renderers are pure, unit tests pass fixed times and assert pixel outputs/strings.

### 2. Two built-in no-config sources, one entity-backed source
- `datasource.AnalogClockDS{}` and `datasource.MatrixRainDS{}` constructed inline in `loadSources` exactly like `SystemStatsDS`. No ent entities.
- `CountdownDS` reads a `Countdown` row (name, target_time, label, enabled). Rendered via a small panel: label top, time below, theme colors from the default/cyber theme.
- **Alternative considered**: single `AmbienceDS` with a mode string. Rejected — mode-in-a-config makes matrix bindings ambiguous and splits the feed identity; separate types keep `source_type` clean (`analog-clock`, `matrix-rain`, `countdown`).

### 3. Matrix cell bindings
Bindings JSON (matrix-dashboard-rendering) gains three new selectable types, resolved like render-upgrades' `clock` binding:
- `analog-clock:0` and `matrix-rain:0` — built-ins resolved without DB lookup (id 0 sentinel).
- `countdown:<id>` — resolved via the `Countdown` query, same as other entity bindings.
Matrix editor's per-cell source dropdown is fed from `BINDING_OPTS` (web/frontend/app.ts) — ambience entries are appended there; grid preview renders them through the shared preview path.

### 4. Feed ordering
`loadSources` appends `AnalogClockDS` and `MatrixRainDS` after configured sources but keeps `SystemStats` position stable (append ambience right after it). Countdowns are appended with the other entity-backed sources in the same `loadSources` block, labeled `Countdown: <name>` (mirroring `Text: <content>` naming). Random/sequential modes then treat them exactly like any other source.

## Risks / Trade-offs

- [Matrix rain too busy / bright at night] → deterministic dimming: intensity scales with a fixed palette (head #b8ffb0, trail fading to #002200); no config in v1, a follow-up could add a `dim` variant.
- [Countdown targets pass while source disabled] → render "DONE" forever is fine (explicit state); admin sees the entity and can delete/retarget. Documented in the form ("past targets show DONE").
- [Analog clock at 32×32 too coarse] → hand radius scaled to min(width,height)/2 − padding; 32×32 still readable (12/3/6/9 dots + hands). Verified by render unit tests at 32 and 64.
- [Per-frame cost] → all three are O(width×height) with no allocations in the hot loop; rain uses a precomputed glyph table. Benchmark test guards against regressions.

## Migration Plan

Additive ent migration: new `Countdown` table + edge from `GeneralSettings`. No changes to existing tables, feed message format, or device client. Rollback = drop entity + stop appending built-ins.

## Open Questions

- Should ambience sources be auto-skippable in random mode when the wall is a pure-info dashboard? v1: no (they're normal sources); a per-type exclusion filter is a future change.
- Rain glyph set: katakana-only, or mix in digits (classic Matrix)? v1: katakana + digits mix, weighted toward katakana.
