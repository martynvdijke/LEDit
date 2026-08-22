# Change Proposal: source-health-monitoring

## Why

A datasource that fails silently is invisible: the wall keeps cycling fallback frames, and nobody knows the weather API died two days ago. LEDit has no signal for this — no per-source success/failure tracking, no fleet liveness, no render timing. The admin dashboard shows counts, not health.

This change adds three layers of visibility: per-source health status, device fleet health, and render metrics.

## What Changes

1. **Source health status** — an in-memory health registry keyed by `<type>:<id>` recording last success time, last error, last render duration, consecutive failures, total renders and failures. Updated by the feed loop after every `GetPNG` call and by the preview endpoint. The dashboard shows a per-source status dot (green/red/yellow) and a health summary; the matrix editor's per-cell source selectors mark failing sources with ⚠. Optional `GET /api/health` exports the registry as JSON.

2. **Device fleet health** — the devices page gains liveness: alive/stale/never badges (alive = last_seen within 3× the device's refresh interval), a frames-served counter per device, and last error. Auto-refreshing page.

3. **Render metrics** — per-source render duration EWMA (ms), total frames rendered per source, and MatrixDS composite cache hit/miss counters. Surfaced on dashboard stat cards and the analytics page.

## Impact

- New `handlers/health.go` registry (no new ent entities — in-memory, resets on restart, accepted trade-off).
- Dashboard, devices page, analytics page UI additions (Go templates + small JS for auto-refresh).
- Optional unauthenticated `GET /api/health` (consistent with the existing `/api` read endpoints; exposure documented).
- Matrix editor ⚠ marks depend on the matrix-dashboard-rendering binding options; health tracking itself is independent.
- WebSocket message format unchanged; no datasource behavior changes.
