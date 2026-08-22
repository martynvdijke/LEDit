# Design: last-known-good-cache

## Context

`Datasource` is a single method: `GetPNG(width, height) (*render.RenderedImage, error)`. `serveFeed` (handlers/websocket.go:342) skips a source on error; `AdminPreview` (handlers/preview.go) returns an error page on render failure. Sources are constructed in `loadSources` (websocket.go) from `GeneralSettings` edges. `RenderedImage` carries `Format` + `Data` bytes. Rendering is CPU-only (fonts, panels) — there is no "data" layer to cache, only rendered frames.

## Goals / Non-Goals

**Goals**
- Failure path serves the last good frame instead of skipping/erroring, for both the feed loop and previews.
- Mark stale output so clients can distinguish live from cached.
- Bounded memory; zero changes to the successful path; zero DB impact.

**Non-Goals**
- No caching of *upstream API responses* (that's a datasource concern; e.g., the uptime TTL cache in more-datasources). This cache is purely the last rendered frame.
- No disk persistence (restart clears the cache — same accepted trade-off as the health registry; a 64×64 PNG is regenerated within a cycle anyway).
- No change to how long stale frames are shown (the feed timeout still governs).

## Decisions

### 1. Cache key: source identity + resolution
Key = `<type>:<id>@<width>x<height>`. Identity comes from the existing `<type>:<id>` convention used by the health registry (`source-health-monitoring`), so both systems can share the key space. Resolution is in the key because a 400×400 preview frame is not a valid 64×64 device frame.
- **Alternative considered**: one entry per source, re-scaled on demand. Rejected — nearest-neighbor downscale of a 400×400 to 64×64 loses the crisp pixel font rendering; storing per-resolution frames is cheap and exact.

### 2. LRU with bounded capacity
A simple `sync.Mutex`-guarded LRU (`map[string]*entry` + doubly-linked list, ~80 lines, no dependency). Default max 256 entries; a knob (`lkg_max_entries`, default constant 256) lives in the same settings surface as the health config. Worst case ≈ 256 × 400×400 PNG (tens of KB each) ≈ a few MB.
- **Alternative considered**: `github.com/hashicorp/golang-lru`. Rejected — one small hand-rolled LRU avoids a dependency for a single use site; swapping later is trivial.

### 3. Integration via a thin wrapper, not fork-and-branch
Rather than editing every `GetPNG` call site, introduce:

```go
// handlers/lkg.go
type LKGCache struct { mu sync.Mutex; max int; entries map[string]*lkgEntry; lru *list.List }

func (c *LKGCache) GetPNG(key string, get func() (*render.RenderedImage, error)) (*render.RenderedImage, bool /*stale*/, error)
```

Semantics:
- `get` succeeds → store frame, return `(img, false, nil)`.
- `get` fails and cache has `key` → return `(cachedImg, true, nil)`.
- `get` fails, no cache → return `(nil, false, err)` (caller keeps today's skip/placeholder behavior).

`serveFeed` and `AdminPreview` wrap their existing `GetPNG` call with a cache-key construction (each source's `sourceWithName` gains a `cacheKey` field set in `loadSources`; preview builds it from `type`+`id`). This keeps the cache orthogonal to the datasource registry — no changes to `datasource/`, no interface churn.
- **Alternative considered**: a `CachedDatasource` decorator implementing `Datasource`. Rejected — the health registry also keys on `<type>:<id>`, and constructing the key needs the same id the feed doesn't currently carry through; threading it through a decorator adds more plumbing than the wrapper.

### 4. Stale marking
- Feed: message JSON adds `"stale": true` and `"stale_age": <seconds>` only when serving a cache hit. Old device clients ignore unknown fields (JSON decode of the relevant subset).
- Preview: HTTP headers `X-LEDit-Stale: 1` + `X-LEDit-Stale-Age: <seconds>`; the preview `<img>` JS (`app.ts` debounce) doesn't need changes — headers are inspectable via devtools/curl and used by the health dashboard badge later.
- Device client (Python): optional — read `stale` and overlay a dim corner dot. v1: log it; visual overlay is a follow-up.

### 5. Statistics
`LKGCache` exposes `Stats() (hits, misses, staleServes uint64)` used by the health dashboard integration; also track per-key stale-serve age EWMA. Standalone, stats are logged at `slog.Debug` on cache eviction.

## Risks / Trade-offs

- [Stale data shown as if live] → `stale: true` + age marking everywhere; no ambiguity in the protocol.
- [Memory growth] → bounded LRU; eviction logs; knobs documented.
- [Stale frame after config change] → key includes `<type>:<id>` but not config hash; a source whose URL changes keeps a stale frame until first success. Mitigation: cache entry records the config signature (cheap hash of URL/token fields) and misses on mismatch — treated as "no cache" on the first attempt with new config.
- [Cache clears on restart] → accepted (in-memory), consistent with health registry; first-failure-after-restart behaves like today.

## Migration Plan

None — purely additive in-process behavior. No ent migrations, no config file changes, no protocol breakage (new optional JSON fields + headers only). Rollback = remove the wrapper calls; `LKGCache` is self-contained.

## Open Questions

- Should a stale frame be visually distinct on the wall itself (e.g., a dim border pixel) or only in admin? v1: admin/preview only + protocol flag; wall visual is a design decision for render-upgrades.
- Is 256 entries the right default, or should `max` be user-configurable in admin settings? v1: constant 256; revisit if users run huge source fleets.
