# Change Proposal: add-event-driven-switching

## Why

Feed switching is purely time-driven: cron schedules decide *when* sources run, and the rotation cycles them on a timer. The only interrupt is a priority notification — an all-or-nothing override with no notion of "stay on this while something is true". There is no middle tier between scheduled rotation and emergency priority: when Jellyfin starts playing, the wall should hold on now-playing until playback stops; when HomeAssistant reports the garage door open, it should pin that camera/sensor panel. Today the wall can't react to the data it is already displaying.

## What Changes

- Add a **DisplayRule** entity: `name`, `enabled`, target source reference (`source_type`/`source_id`, same shape as matrix bindings), a `condition` JSON blob (`{path, operator, value}` — dot-path resolved with the existing `extractDotPath` semantics, operators `eq/ne/gt/lt/ge/le/contains/exists`), a `check_interval_seconds` (default 30), and optional `cooldown_seconds`.
- Add a **StateProvider capability interface** on datasources (`CurrentState(ctx) (map[string]any, error)`): sources that can report live state (Jellyfin, HomeAssistant, GenericAPI, Pi-hole, system stats) implement it; rules targeting sources without the capability are skipped and logged.
- **Pin-aware feed controller**: `FeedController` gains a pinned-source concept. A background evaluator goroutine checks enabled rules at their intervals; when a condition matches, the controller pins the target source — `serveFeed` renders and holds the pinned source instead of advancing the rotation. When the condition clears, the pin releases and rotation resumes where it left off.
- **Precedence**: notifications > pinned event rule > scheduled rotation. This slots cleanly under the existing notification broadcast and above normal cycling; push-to-display's TTL overrides remain the top tier.
- **Admin UI**: rules CRUD page (name, target source picker, condition builder, interval/cooldown, enabled) plus a dashboard indicator when a rule currently holds the wall.

## Capabilities

### New Capabilities
- `event-driven-switching`: Condition-based display pinning — DisplayRule storage, state evaluation via the StateProvider capability, pin/unpin lifecycle in the feed controller, precedence vs notifications and rotation, and admin management.

### Modified Capabilities
<!-- openspec/specs/ has no archived capabilities yet; no requirement deltas. -->

## Impact

- **New code**: `ent/schema/displayrule.go` (+ generated ent code), `handlers/eventrules.go` (CRUD + evaluator goroutine).
- **Modified code**: `datasource/datasource.go` (StateProvider interface), state implementations in relevant datasources (jellyfin from `more-datasources`, homeassistant, genericapi, systemstats), `handlers/feed_control.go` (pin API on FeedController), `handlers/websocket.go` (serveFeed honors pin each slot boundary), `handlers/server.go` (routes + evaluator lifecycle), admin templates for rules CRUD.
- **Dependencies**: Target sources may not exist yet (e.g. Jellyfin lands with `more-datasources`) — the capability interface means rules degrade gracefully to "skipped" until their target exists.
- **Risk**: evaluator fetch storms — per-rule check intervals with jitter, reusing each datasource's own caching/TTL where present; flapping conditions — cooldown holds the pin (or holds it off) for the configured window; evaluator failure never blocks the feed (rotation continues untouched).
