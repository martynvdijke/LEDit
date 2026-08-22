## 1. LKG Cache Implementation

- [x] 1.1 Implement `handlers/lkg.go`: `LKGCache` with mutex-guarded map + LRU list, `max` capacity (default 256), config-signature mismatch handling
- [x] 1.2 Implement `GetPNG(key, configSig, get)` wrapper returning `(img, stale bool, err)` per design semantics
- [x] 1.3 Implement `Stats()` (hits, misses, stale serves) and eviction logging (slog.Debug)

## 2. Feed Loop Integration

- [x] 2.1 Add `cacheKey` field to `sourceWithName` (websocket.go) and populate it in `loadSources` for every source type
- [x] 2.2 Wrap the `sw.Source.GetPNG` call in `serveFeed` with the LKG wrapper; on stale, add `stale`/`stale_age` to the message JSON
- [x] 2.3 Verify successful path emits identical messages to before (no stale fields, same format)

## 3. Preview Integration

- [x] 3.1 Wrap `AdminPreview` and matrix preview render calls with the LKG wrapper; on stale, set `X-LEDit-Stale: 1` + `X-LEDit-Stale-Age` headers
- [x] 3.2 Confirm preview error path is unchanged when no cache entry exists

## 4. Unit Tests

- [x] 4.1 Cache tests: store/retrieve, per-resolution separation, LRU eviction at capacity, config-signature invalidation
- [x] 4.2 Wrapper tests: success caches + returns live, failure-with-cache returns stale, failure-without-cache returns error
- [x] 4.3 Feed integration test: failing source yields stale frame with flag; fresh render has no flag
- [x] 4.4 Preview integration test: stale headers present on cached serve, absent on live

## 5. Verification

- [x] 5.1 Run `task pre-push` (gofmt, tests, build) and fix failures
- [x] 5.2 Confirm no changes to datasource implementations, ent schema, or protocol for live frames
