# Design: source-health-monitoring

## Decision 1: One package-level registry in handlers/health.go

A single `Registry` struct with `RWMutex` lives in `handlers/health.go` as a package-level singleton (`Health`). It is keyed by `"<type>:<id>"` (endpoint names as used by the feed and `sourceIndex`, e.g. `weather:3`, `rssfeed:1`, `matrix:2`; built-in sources like system stats use `system:0`). Entry shape:

```go
type SourceHealth struct {
    LastSuccessAt   time.Time
    LastError       string
    LastDuration    time.Duration
    ConsecutiveFails int
    Renders         int64
    Failures        int64
}
```

Placed in `handlers` (not a new package) to avoid import cycles: `websocket.go` feed loop and `preview.go` already live there, and nothing else needs it. The registry exposes `RecordSuccess(key, dur)`, `RecordFailure(key, err, dur)` and `Snapshot() map[string]SourceHealth`; a tiny interface (`HealthRecorder`) makes it stub-able in tests.

## Decision 2: Status classification thresholds

Derived state, computed on read (never stored), so thresholds can change without migration:

- **green**: last render succeeded (`ConsecutiveFails == 0`)
- **red**: `ConsecutiveFails >= 3` or last render failed with no prior success
- **yellow**: `1 <= ConsecutiveFails < 3` (transient — one bad upstream call shouldn't light up the board)

Dashboard dots and `GET /api/health` use these rules. The matrix editor's `bindingOptions` appends ` ⚠` to labels of red sources (dependency: matrix-dashboard-rendering's `handlers/sources.go`; guarded — if the label function doesn't exist yet, this task is deferred within the same change).

## Decision 3: Single writer, cheap readers

The feed loop (`serveFeed` after each `GetPNG` and error path) and the preview endpoint are the only writers. `RecordSuccess`/`RecordFailure` take the lock per render — one write per source per frame is negligible next to PNG encode. Snapshot for pages copies under the lock (bounded by configured source count, so no growth concern).

## Decision 4: No persistence — accepted trade-off

Health resets on restart. Rationale: per-frame DB writes are the wrong price for a status dot, and the feed already re-establishes truth within one cycle after boot. `LastSuccessAt` relative truth is enough. If fleet-wide uptime history is ever wanted, that is a separate analytics concern.

## Decision 5: Device fleet liveness

Alive = `time.Since(device.LastSeenAt) <= 3 × device.RefreshInterval` (grace factor 3 — a device that missed one interval may be transient; missing three is stale). Never = `LastSeenAt` zero. Frames-served is a **persisted** ent counter `frames_served` on `DeviceSettings` (incremented in `HandleDeviceWS` per frame written) — justified: it is the one metric with audit value across restarts, and it is one additive field + one `UpdateOneID(...).AddFramesServed(1)` per frame (SQLite in-memory-ish cost; if profiling shows contention, batch per connection close — note in code). Last error: the registry records device-level errors (write failures) under `device:<id>`; the devices page renders it when present. Devices page auto-refreshes with a 15 s `setInterval` fetch (or meta refresh if the template pattern prefers it — follow existing conventions).

## Decision 6: Render metrics

Same registry, extended per source:

```go
type RenderMetrics struct {
    EWMADurationMs float64 // alpha 0.3, warm-up from first sample
    CacheHits      int64
    CacheMisses    int64
}
```

EWMA: `ewma = alpha*newSample + (1-alpha)*ewma`, alpha 0.3 (responsive enough to catch regressions, smooth enough to ignore single spikes). Cache counters are updated inside `MatrixDS`'s panel cache (dependency on matrix-dashboard-rendering's `datasource/matrix.go`; guarded) or stay zero when matrix code is absent. Analytics page renders a metrics table (source, EWMA ms, renders, failures, cache hit/miss); dashboard adds EWMA ms + renders to the existing stat-card grid.

## Decision 7: /api/health exposure

`GET /api/health` returns `{"sources": {key: {...}}, "devices": {...}}` JSON, unauthenticated like the other `/api` read endpoints (`/api/feed/current`, `/api/trmnl/stats`). Documented in the change as intentionally read-only status (no control surface); if the wall is internet-exposed this is one more endpoint — same exposure class as existing ones, flag in settings docs.

## Risks

- In-memory loss on restart — accepted (Decision 4).
- EWMA warm-up: first samples are the raw value; fine for a status readout.
- Per-frame counter writes — one additive integer update per frame; revisit if SQLite write contention appears.
- Matrix-editor ⚠ and cache counters couple to an unmerged change — guarded, degrade to zero/plain labels.

## Migration

- One additive ent field (`frames_served` on DeviceSettings) → `Schema.Create` migration on next start.
- No datasource behavior changes; feed message format unchanged.
