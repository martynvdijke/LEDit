## Why

TRMNL e-ink displays are ideal for always-on monitoring dashboards, and LEDit already produces exactly the kind of data worth showing — system stats (CPU, memory, load) and display analytics (frames shown per source, uptime). Mirroring the TRMNL plugin pattern used by sandwitches (Liquid templates + polling JSON endpoint), LEDit can serve a TRMNL plugin that turns any e-ink display into a live LEDit stats screen with no extra hardware or accounts.

## What Changes

- Add a public read-only JSON polling endpoint `GET /api/trmnl/stats` returning LEDit system stats (CPU cores, memory allocation, load average, Go/OS info) plus display analytics (total displays, uptime, per-source display counts, recent events)
- Reuse the stat collection logic from the System Stats datasource so values stay consistent between the LED matrix feed and the TRMNL endpoint
- Add a `trmnl/` directory with the TRMNL plugin assets, following the sandwitches convention:
  - `settings.yml` — plugin metadata, polling URL, author bio custom field, refresh interval
  - `full.liquid` — full-screen stats layout
  - `half_horizontal.liquid` / `half_vertical.liquid` — half-screen layouts
  - `quadrant.liquid` — quadrant layout
- No authentication required for the stats endpoint (read-only, no secrets), consistent with how the System Stats datasource content is already public within the display feed

## Capabilities

### New Capabilities
- `trmnl-stats-api`: Public JSON endpoint(s) that expose LEDit system stats and display analytics in a shape suitable for TRMNL polling
- `trmnl-eink-templates`: The `trmnl/` plugin assets — `settings.yml` plus Liquid templates for full, half, and quadrant TRMNL layouts

### Modified Capabilities
- *(none)*

## Impact

- **Go handlers**: New `handlers/trmnl.go` with the stats handler; route registered in `handlers/server.go` under the existing public API group (`/api/...`)
- **Datasource**: Refactor `datasource/systemstats.go` so the stat collection (memory, load) is reusable by the TRMNL handler
- **Assets**: New `trmnl/` directory (`settings.yml`, `*.liquid` templates) — same layout as `sandwitches/trmnl/`
- **Database**: None
- **Dependencies**: None — system stats use `runtime` and `/proc/loadavg`, already in use
