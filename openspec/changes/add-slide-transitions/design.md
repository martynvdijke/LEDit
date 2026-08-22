## Context

`serveFeed` (`handlers/websocket.go`) renders each source once per slot through the last-known-good cache, sends one `{format, image, source, next}` WS message, then holds for the slot timeout while polling skip every 50 ms. `RenderedImage` is `{Format string, Data []byte}` — PNG-encoded bytes — so compositing means decode → blend → re-encode. The pixel-maker-animator change is introducing the `Animator` capability and an in-slot re-render tick; transitions ride the same slot-boundary seam but need no new protocol concepts.

## Goals / Non-Goals

**Goals:**
- Optional animated hand-off (fade / wipe / dissolve) between consecutive feed slots, configured globally.
- Device protocol unchanged: transitions are just more normal messages; devices render whatever they receive.
- Deterministic output: identical inputs produce identical transition frames (testable, reproducible).

**Non-Goals:**
- Per-source or per-device transition styles (global setting v1).
- 3D/pixelate/slide-from-offscreen flourishes beyond the three core styles.
- Transitions on the TRMNL/e-ink HTTP path (devices poll static images; out of scope by construction).
- Audio or timing synchronization with animator frame cadence.

## Decisions

### D1 — Composite on the server from decoded PNGs
`render/composite.go` decodes both frames to `*image.NRGBA`, blends per style, re-encodes PNG. At 64×64 this is microseconds; even 128×128 is trivial. Alpha: treat both frames as opaque over black (LED panels have no translucency) — composite onto an opaque canvas first, then blend RGB channels only.
- *Why:* No device firmware changes; works for any pair of sources including image/video slides.
- *Alternative:* Emitting partial-reveal metadata messages for client-side compositing — rejected: requires firmware coordination across every device type.

### D2 — Frame budget derived from duration
Steps = clamp(`transition_ms` / 40, 6, 16); inter-frame pacing = `transition_ms` / steps via a bounded sleep between sends inside the existing slot loop. Default 500 ms → ~12 steps at ~40 ms pacing, which reads as smooth on LED matrices at typical refresh rates without flooding slow serial links.
- *Why:* Ties perceived smoothness to one knob; caps protect low-baud device links from message bursts.

### D3 — Style implementations are pure functions of progress
```go
func BlendFade(prev, next *image.NRGBA, t float64) *image.NRGBA        // per-pixel lerp
func BlendWipe(prev, next *image.NRGBA, t float64) *image.NRGBA       // column sweep, hard edge
func BlendDissolve(prev, next *image.NRGBA, t float64) *image.NRGBA   // deterministic reveal
```
Dissolve uses a permutation seeded per `(width, height)` (Fisher-Yates with a fixed seed, same determinism approach as matrix rain's `(step, x, y)` hashing), revealing `floor(t × N)` pixels in permutation order. Wipe reveals columns `< t × width`. All take progress `t ∈ [0,1]`; nothing samples clocks inside the blender.
- *Why:* Pure functions are unit-testable against golden outputs and inherently deterministic; clock-free design means re-renders (e.g., LKG replays) can't flicker differently.

### D4 — serveFeed retains the outgoing frame and emits the ramp at the boundary
The loop already holds the current `img` per iteration; keep it as `prevImg` into the next iteration. When transitions are enabled and both `prevImg` and the new frame exist, emit steps before holding the new slot. Message shape stays `{format, image, source, next}` with `source` set to the incoming source name for all ramp frames (so UI labels settle immediately). The hold deadline starts after the ramp completes.
- *Why:* Minimal loop surgery; labels stay truthful; slot timing semantics preserved (ramp time comes out of the boundary, not the slot).

### D5 — Skip conditions mirror the proposal exactly
No transition when: style `none`; either frame missing (first-ever slot, previous render failed/stale-fallback path, notification slot); a skip arrives mid-ramp (abort remaining steps, jump to hold); pause engages mid-ramp (finish current step, then honor pause). Notifications checked before starting a ramp, as today.
- *Why:* Transitions must never delay an interrupt or mask a failure fallback.

### D6 — Settings follow the GeneralSettings pattern
`transition_style` (string enum, default `"none"`, validated against the style set) and `transition_ms` (int, default 500, validated 100–2000) on GeneralSettings; additive migration; exposed in the existing settings admin form with a live preview pair of buttons that POSTs two chosen sources to the preview endpoint and plays the ramp in-browser.
- *Why:* One global knob matches how theme/timeout/random already work; preview keeps the spike question ("does it look janky?") answerable without deploying to hardware.

## Risks / Trade-offs

- [Low-refresh devices make fades stutter] → Defaults tuned conservative (12 steps/500 ms); style `none` remains default so nothing changes until opted in; admin preview lets users judge before hardware.
- [Message burst on constrained links] → Step cap (16) + pacing sleep bounds burst size; TRMNL path unaffected (HTTP polling).
- [Decode cost per boundary] → Negligible at matrix resolutions; measured in the composite unit tests to catch regressions.
- [Interaction with future Animator slots] → Ramp endpoint is whatever frame the incoming source rendered at boundary time; when pixel-maker-animator lands, its in-slot ticks naturally continue after the ramp with no further work.

## Migration Plan

Two additive columns via ent codegen/auto-migration; default `none` makes the feature inert. Rollback safe (columns unused by old code).

## Open Questions

- Should wipe direction be configurable (L→R assumed v1)?
- Crossfade easing (linear vs ease-in-out) — start linear, revisit after hardware eyeballing.
