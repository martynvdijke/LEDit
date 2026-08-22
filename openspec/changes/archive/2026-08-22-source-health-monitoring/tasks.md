# Tasks: source-health-monitoring

## 1. Source health status

- [x] 1.1 Add `handlers/health.go`: package-level `Health` registry singleton (RWMutex) with `SourceHealth{LastSuccessAt, LastError, LastDuration, ConsecutiveFails, Renders, Failures}` and `RecordSuccess(key string, duration time.Duration)` / `RecordFailure(key string, err error, duration time.Duration)` / `Snapshot()` methods, plus status classification (green/yellow/red); add unit tests.
- [x] 1.2 Integrate recording into the feed loop: after each `GetPNG` call in `serveFeed` (and in the preview endpoint), record success/failure + duration keyed by source name/type+id; add tests.
- [x] 1.3 Dashboard health display: per-source status dots in the datasources table and a green/yellow/red summary; unit tests for classification and summary aggregation.
- [x] 1.4 Matrix editor warnings: annotate failing sources with a warning mark in `bindingOptions` (guarded so it degrades gracefully); tests.
- [x] 1.5 Add `GET /api/health` read-only JSON endpoint (unauthenticated like other `/api` reads); tests for payload and read-only enforcement.

## 2. Device fleet health

- [x] 2.1 Add additive `frames_served` int field to `DeviceSettings` ent schema, run codegen, increment it in `HandleDeviceWS` per frame; tests.
- [x] 2.2 Devices page: alive/stale/never badges based on `LastSeenAt` vs 3× `RefreshInterval`, and 15-second auto-refresh; tests.
- [x] 2.3 Record device errors in the health registry under `device:<id>` (render/stream errors in `HandleDeviceWS`); tests.

## 3. Render metrics

- [x] 3.1 Extend health registry with `EWMADurationMs` (alpha 0.3) updated on every render; add matrix panel cache hit/miss counters (guarded); tests for EWMA math and cache counters.
- [x] 3.2 Display render metrics on the dashboard (average duration, matrix cache hit ratio when available) and per-source metrics on the analytics page; tests.

## 4. Verification

- [x] 4.1 Run Go tests, frontend build, Playwright tests, and `task pre-push`; confirm no WebSocket message format changes and no regressions in existing dashboard/analytics behavior.
