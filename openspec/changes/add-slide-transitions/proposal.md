# Change Proposal: add-slide-transitions

## Why

The wall cuts hard between sources: one slot's frame is replaced by the next with no intermediate state, which reads as flicker on LED matrices and makes the wall feel mechanical. The `add-pixel-maker-animator` change is introducing in-slot re-render machinery (the Animator capability and per-frame send loop); the same seam can carry whole-slide transitions at slot boundaries — fade, wipe, dissolve — for a large perceived-quality win at modest cost.

## What Changes

- Add **transition styles** applied between consecutive feed slots: **fade** (crossfade previous → next), **wipe** (column sweep revealing the next frame), **dissolve** (deterministic pseudo-random pixel reveal, same `(step, x, y)` determinism approach as matrix rain).
- **Global settings**: `transition_style` (`none` | `fade` | `wipe` | `dissolve`, default `none`) and `transition_ms` (default 500) on GeneralSettings, exposed in the existing settings admin form.
- **Slot-boundary compositing in `serveFeed`**: when a transition is configured, the server renders N intermediate composite frames (previous frame × next frame blended by progress) and sends them as normal WS messages before holding the next slot. Message shape unchanged; devices simply receive more frames.
- **Render support**: new `render/composite.go` with progress-parameterized blend functions over `*render.RenderedImage`; deterministic per step index so re-renders are reproducible and cheap.
- Transitions are skipped when: style is `none`, the next source failed to render (fallback path), a notification interrupts, or the slot was skipped mid-transition (skip completes immediately).

## Capabilities

### New Capabilities
- `slide-transitions`: Animated hand-off between feed slots — composite blending styles, settings, slot-boundary send behavior, and interaction with skip/pause/notifications.

### Modified Capabilities
<!-- openspec/specs/ has no archived capabilities yet; no requirement deltas. -->

## Impact

- **New code**: `render/composite.go` (blend primitives + per-style frame generators), unit tests.
- **Modified code**: `ent/schema/generalsettings.go` (`transition_style`, `transition_ms` — additive migration), `handlers/websocket.go` (slot boundary emits transition frames; needs the outgoing frame retained across iterations), settings admin form + handler for the two new fields.
- **Protocol**: unchanged message shape; more messages during boundaries. E-ink/TRMNL devices poll HTTP separately and are unaffected.
- **Dependencies**: none hard — works on any pair of `RenderedImage`s today; composes naturally with pixel-maker-animator's animated slots later (transition samples the animator's current frame as its endpoint).
- **Risk**: low device refresh rates can make fast fades look stuttery — default duration 500 ms with modest step counts (~8–12 frames); compositing cost at 64×64 is trivial; transitions bypass the LKG cache by construction (they compose two already-cached frames) so cache semantics are untouched.
