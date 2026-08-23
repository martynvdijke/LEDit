## 1. Compositing primitives

- [x] 1.1 Add `render/composite.go`: decode helper (PNG bytes → `*image.NRGBA`, composited opaque over black), `BlendFade`, `BlendWipe`, `BlendDissolve` pure functions over progress, and a fixed-seed per-resolution permutation generator; encode helper back to PNG `RenderedImage`
- [x] 1.2 Table-driven unit tests in `render/composite_test.go`: endpoint identity (t=0/t=1), determinism (repeat calls pixel-identical), wipe monotonicity, odd-size canvases, and a benchmark for decode+blend+encode at 64×64 and 128×128

## 2. Settings

- [x] 2.1 Add `transition_style` (string, default `"none"`, enum validator) and `transition_ms` (int, default 500, validator 100–2000) to `ent/schema/generalsettings.go`; run ent codegen and confirm additive migration
- [x] 2.2 Extend the settings admin form + save handler with the two fields (select + number input)

## 3. Feed integration

- [x] 3.1 In `serveFeed`: retain the outgoing frame across loop iterations; at each boundary load the global transition config once per connection; when eligible, generate steps via the chosen blender and send them paced (`transition_ms`/steps bounded sleeps) before starting the hold deadline; label ramp messages with the incoming source name
- [x] 3.2 Implement skip conditions: none-style, missing/stale boundary frames, notification-first precedence, mid-ramp skip abort, mid-ramp pause hand-off; keep LKG/health/analytics paths untouched
- [x] 3.3 Confirm `/ws/feed`, device feeds, and device previews all honor the setting while the TRMNL HTTP path remains unchanged

## 4. Tests

- [x] 4.1 Integration test in `main_test.go`: fake clock-free two-source feed with `fade` asserts ramp frame count and ordering, `none` asserts exact legacy message sequence, and skip-mid-ramp truncates the burst
- [x] 4.2 Handler test for settings validation (bad style, out-of-range ms rejected)

## 5. Docs and validation

- [x] 5.1 README note: styles, duration knob, LED refresh caveat, e-ink unaffected
- [x] 5.2 Run `go build ./... && go test ./...` and `task pre-push` before pushing
