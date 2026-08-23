## 1. Capability interface and shared helpers

- [x] 1.1 Export `datasource.DotPath(root any, path string) (any, bool)` by promoting `extractDotPath` from `datasource/genericapi.go` (keep the unexported wrapper delegating to it); update genericapi call sites
- [x] 1.2 Add `datasource.StateProvider` interface (`CurrentState(ctx context.Context) (map[string]any, error)`) in `datasource/datasource.go`
- [x] 1.3 Implement `CurrentState` for `GenericAPIDS` (reuse apiGet + cached-body policy), `HomeAssistantDS`, and `SystemStatsDS`; table-driven tests for each state map shape

## 2. Rules storage and evaluation engine

- [x] 2.1 Add `ent/schema/displayrule.go`: `name`, `enabled`, `source_type`, `source_id`, `condition` (text JSON), `check_interval_seconds` (default 30, validator ≥5), `cooldown_seconds` (default 0); GeneralSettings edge; run ent codegen
- [x] 2.2 Add `datasource/eventrule.go` (or `handlers/ruleengine.go`): condition parsing/validation + pure `Evaluate(state map[string]any, cond Condition) bool` covering eq/ne/gt/lt/ge/le/contains/exists with numeric coercion and safe-false semantics; exhaustive unit tests
- [x] 2.3 Build the evaluator goroutine in new `handlers/eventrules.go`: registry of active `*FeedController` (add join/leave calls in `serveFeed`), per-rule nextCheck scheduling with ±10% jitter, 30 s DB reload, pin/unpin fan-out, once-per-start skip logging for non-capable targets; panic-recover + restart via the server run loop
- [x] 2.4 Extend `FeedController` in `handlers/feed_control.go` with pin state (`PinnedKey` + set/clear/get under the existing mutex); extend `Status()` with `pinned_by` when set

## 3. Feed integration

- [x] 3.1 In `serveFeed`, honor the pin at each slot boundary: when `feed.PinnedKey` is set, select that source (resolved from the connection's source list by cacheKey) instead of advancing; keep notifications-first precedence; manual `Next()` clears the pin
- [x] 3.2 Verify pinned slots flow through LKG/health/stale/frame-count paths unchanged (no special-casing) and that pause still pauses while pinned

## 4. Routes and admin UI

- [x] 4.1 CRUD handlers in `handlers/eventrules.go` (list/new/create/edit/update/delete) with condition validation; session-authed routes in `handlers/server.go`; start/stop the evaluator with the server lifecycle
- [x] 4.2 `web/templates/admin/eventrules.html`: rules list + form (target picker from bindingOptions-style data, condition builder, interval/cooldown inputs)
- [x] 4.3 Dashboard badge "held by rule: <name>" driven by `GlobalFeed.Status()["pinned_by"]` (and per-device status where surfaced)

## 5. Tests

- [x] 5.1 Integration test in `main_test.go`: fake StateProvider source toggling true→false asserts pin → hold → release over short intervals; second test asserts notification-during-pin precedence and skip-clears-pin
- [x] 5.2 Evaluator unit tests: jitter bounds, reload picks up disabled rules, non-capable target logged once, upstream failure leaves pin state per cooldown
- [x] 5.3 Handler tests for rules CRUD auth mirroring existing datasource CRUD tests

## 6. Docs and validation

- [x] 6.1 README section: rules model, operator table, precedence chain (notifications > rules > rotation), cooldown semantics
- [x] 6.2 Run `go build ./... && go test ./...` and `task pre-push` before pushing
