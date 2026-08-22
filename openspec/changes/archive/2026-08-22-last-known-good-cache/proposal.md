# Change Proposal: last-known-good-cache

## Why

In `serveFeed` (handlers/websocket.go:342), a failing datasource is handled with a bare `continue` — the frame is skipped and the wall holds the previous frame or sits idle. When a source's API dies, LEDit shows *nothing* rather than the last thing it knew, and the admin preview endpoint fails the same way. A self-hosted wall should be resilient: stale data beats blank data.

## What Changes

1. **Last-known-good cache** — a per-source cache of the last successful render, keyed by `<type>:<id>` + resolution (width × height). Every successful `GetPNG` writes the rendered frame (PNG bytes + format) into the cache; every failure reads it back.

2. **Serve stale on failure** — when `GetPNG` fails, the feed loop renders the cached frame instead of skipping. The WebSocket message gains a `stale: true` flag (and the cached frame's age) so clients can show a subtle indicator; preview endpoints return the cached PNG with an HTTP header (`X-LEDit-Stale: 1`, `X-LEDit-Stale-Age: <seconds>`).

3. **Bounded memory** — an LRU cache (default 256 entries, configurable via a `GeneralSettings`-adjacent setting or a constant) so resolutions × sources can't grow unbounded. Renders are small (tens of KB at 400×400, few KB at 64×64) so 256 entries is a few MB worst case.

4. **Cache-health hooks** — the cache records each hit/miss and the age of served stale frames, surfaced alongside the `source-health-monitoring` dashboard stats (a "serving stale" badge per source) when that change is present. Standalone, it degrades to an internal counter.

5. **First-failure behavior unchanged** — if a source has never rendered successfully (no cache entry), failure still behaves as today (skip / placeholder). The cache only ever makes the wall *more* resilient, never changes successful-path behavior.

## Capabilities

### New Capabilities
- `last-known-good-cache`: serving the last successful render of a datasource on failure, with bounded LRU storage and stale-marking of feed/preview output.

### Modified Capabilities
- (none — existing capabilities keep their requirements; this is additive and changes only the failure path)

## Impact

- New `handlers/lkg.go` (or `render/lkg.go`) cache + integration in `serveFeed` (websocket.go), `AdminPreview` (preview.go), and matrix cell previews.
- WebSocket message gains optional `stale` / `stale_age` fields — additive, old clients ignore them; message format unchanged for successful renders.
- Preview endpoint adds two response headers on stale serves.
- No DB changes, no ent entities, no new dependencies, no datasource changes (cache is a wrapper around `GetPNG`).
- Pairs naturally with `source-health-monitoring` (dashboard badge) and `outbound-alerting` (repeated failures that now serve stale data still trigger alerts); independent of both.
