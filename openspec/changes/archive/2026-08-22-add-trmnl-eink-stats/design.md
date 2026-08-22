## Context

LEDit is a Go/Gin server that renders datasource content and streams frames to LED matrices. It already collects the two data families this TRMNL plugin needs:

- **System stats** (`datasource/systemstats.go`): CPU cores, Go version, OS, memory allocation (`runtime.ReadMemStats`), and load average (`/proc/loadavg`). Currently formatted into a PNG dict for the LED feed via `RenderDict`.
- **Display analytics** (`handlers/analytics.go`): in-memory `displayEvent` log with `TrackDisplay(source, duration)` and `GetAnalytics()` returning total displays, uptime, per-source counts, and recent events.

The reference implementation is the sandwitches project's `trmnl/` directory: a TRMNL polling plugin consisting of `settings.yml` (plugin metadata + polling URL + custom fields) and Liquid templates (`full`, `half_horizontal`, `half_vertical`, `quadrant`). TRMNL devices poll the configured URL(s), receive JSON, and render the Liquid template client-side in the TRMNL app/device. The templates and settings are uploaded to trmnl.app by the user; the self-hosted app only serves the JSON.

## Goals / Non-Goals

**Goals:**
- Expose a single public JSON endpoint with LEDit system stats + display analytics
- Ship `trmnl/` plugin assets (settings.yml + 4 Liquid templates) mirroring the sandwitches convention
- Reuse existing stat/analytics collection so values are consistent with the LED feed

**Non-Goals:**
- No authentication/token mechanism for the endpoint (read-only, no secrets; token auth is future work)
- No server-side Liquid rendering or serving of `.liquid` files — templates are uploaded to trmnl.app by the user
- No changes to the existing browser e-ink mode (`e-ink-mode` archived change) — separate concern
- No database changes, no new dependencies, no new datasource types

## Decisions

### 1. Single combined endpoint vs. multiple polling URLs
**Decision:** One endpoint, `GET /api/trmnl/stats`, returning a combined JSON document:
```json
{
  "system": {
    "cpu_cores": 8,
    "go_version": "go1.26",
    "os": "linux/amd64",
    "memory": "123/456 MB",
    "load": "0.5 0.6 0.7"
  },
  "analytics": {
    "total_displays": 123,
    "uptime": "1h2m3s",
    "by_source": {"weather": 40, "crypto": 83},
    "recent": [{"source": "weather", "time": "...", "duration": 10}]
  }
}
```
**Rationale:** sandwitches used two URLs (`recipe-of-the-day` + `users`) because its template cross-references them via `IDX_0`/`IDX_1`. LEDit has no cross-referencing need — one poll, one document. Templates access nested fields via `IDX_0.system.cpu_cores`, which TRMNL Liquid supports.
**Alternative considered:** two endpoints (`/api/trmnl/system`, `/api/trmnl/analytics`) to mirror sandwitches exactly — rejected, adds a second poll and device-side refresh cost for no benefit.

### 2. Reuse system stats collection
**Decision:** Extract the stat collection in `datasource/systemstats.go` into an exported helper (e.g., `func SystemStats() map[string]string`) used by both `SystemStatsDS.GetPNG()` and the new TRMNL handler.
**Rationale:** `memString`/`loadString` are currently unexported package funcs. Reusing them guarantees the TRMNL display and the LED matrix feed never drift on format (e.g., "123/456 MB").
**Alternative considered:** duplicate the logic in `handlers/trmnl.go` — rejected, creates two sources of truth.

### 3. Public unauthenticated endpoint
**Decision:** Register `GET /api/trmnl/stats` in the existing public API group (`handlers/server.go`, alongside `/api/feed/current`, etc.), no auth middleware.
**Rationale:** The System Stats content is already rendered into the publicly streamed LED feed; CPU/memory/load counts are low-sensitivity. TRMNL polling cannot carry admin session cookies, so requiring auth would force a token mechanism (future work, fits the settings.yml custom-field pattern).
**Risk mitigation:** endpoint is strictly read-only and returns no tokens, API keys, or device credentials.

### 4. TRMNL template conventions
**Decision:** Templates follow the sandwitches conventions: `title_bar` footer with LEDit icon + "LEDit stats" title, TRMNL layout classes (`layout layout--col gap`, `grid grid--cols-2`, `item/meta/content`, `value value--large/small`, `image image-dither`), and `IDX_0`-prefixed field access. `settings.yml` pins `framework_version: 2.3.7` (same as sandwitches) and includes an instance URL custom field so users point the plugin at their own LEDit host.
**Rationale:** matching sandwitches' proven structure minimizes template authoring risk and keeps the two projects consistent.

## Risks / Trade-offs

- **[Risk] Public endpoint exposed to the internet** → Endpoint is read-only, returns no secrets; a custom-field API key can be layered on later without breaking the endpoint contract.
- **[Risk] Load average unavailable on non-Linux** → Same behavior as the existing datasource: returns `"--"`; the datasource and endpoint stay consistent by construction (shared helper).
- **[Risk] Analytics is in-memory and resets on restart** → Mirrors existing analytics behavior; acceptable for a dashboard display. Not part of this change to persist.
- **[Risk] Liquid template drift across TRMNL framework versions** → Pin `framework_version` in `settings.yml`; keep templates to stable TRMNL layout classes used by sandwitches.

## Migration Plan

1. Deploy code changes (route + handler + shared stats helper).
2. `trmnl/` assets are committed with the change; no runtime migration.
3. Users create a TRMNL plugin: upload `settings.yml` (set instance URL to their LEDit host), then the four Liquid templates.
4. **Rollback:** remove the route registration and the `trmnl/` directory; LED matrix feed is unaffected since the helper extraction is behavior-preserving.

## Open Questions

- None blocking. Optional future work: token-protected variant of the endpoint if a user exposes LEDit publicly and wants access control.
