## 1. Shared System Stats Helper

- [x] 1.1 Extract the stat collection in `datasource/systemstats.go` into an exported helper (e.g., `func SystemStats() map[string]string`) returning CPU cores, Go version, OS, memory, and load — reusing the existing `memString`/`loadString` logic
- [x] 1.2 Update `SystemStatsDS.GetPNG()` to use the extracted helper so LED feed output is unchanged
- [x] 1.3 Run `go build ./...` and existing tests to confirm the refactor is behavior-preserving

## 2. TRMNL Stats API

- [x] 2.1 Create `handlers/trmnl.go` with an `APITrmnlStats` handler that returns a JSON document with top-level `system` (from the shared helper) and `analytics` (from `GetAnalytics()`) objects
- [x] 2.2 Register `GET /api/trmnl/stats` in the public API group in `handlers/server.go` (no auth middleware)
- [x] 2.3 Add a handler test asserting `GET /api/trmnl/stats` returns 200 with JSON containing the `system` (cpu_cores, go_version, os, memory, load) and `analytics` (total_displays, uptime, by_source, recent) fields

## 3. TRMNL Plugin Assets

- [x] 3.1 Create `trmnl/settings.yml` — `strategy: polling`, polling URL pointing at `<instance>/api/trmnl/stats`, `refresh_interval: 1440`, `framework_version: 2.3.7`, author bio custom field, and `url` custom field for the LEDit instance address
- [x] 3.2 Create `trmnl/full.liquid` — full-screen layout showing system stats prominently, analytics counts, and `title_bar` footer
- [x] 3.3 Create `trmnl/half_horizontal.liquid` — compact half-screen horizontal layout
- [x] 3.4 Create `trmnl/half_vertical.liquid` — compact half-screen vertical layout
- [x] 3.5 Create `trmnl/quadrant.liquid` — quadrant-sized summary layout

## 4. Verification

- [x] 4.1 Run `task pre-push` (gofmt, tests, build) and confirm everything passes
