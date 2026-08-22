## 1. LLM Client

- [x] 1.1 Create `datasource/ai.go` with `ChatCompletions(ctx, settings, messages, maxTokens)` calling `{endpoint}/chat/completions` with bearer auth, 30s timeout, JSON-decoding `choices[0].message.content`
- [x] 1.2 Add unit tests for the client with `httptest` (happy path, HTTP error, malformed JSON, timeout)
- [x] 1.3 Add a shared `BuildSlideSystemPrompt()` helper (short LED text, no markdown, max ~2 lines) and unit test

## 2. AI Text Slide Generation

- [x] 2.1 Add `POST /admin/textslides/generate` handler: reads prompt + current slide fields, calls LLM client, returns `{content}` JSON or 422 with message
- [x] 2.2 Add "Generate with AI" button + JS to the text slide admin form (`web/templates/admin/datasource_form.html` textslide variant) that POSTs the form data and fills the content field
- [x] 2.3 Show inline error in the form when generation fails; add a "not configured" message when `AISettings` has no API key
- [x] 2.4 Register the route in `handlers/server.go` admin group
- [x] 2.5 Playwright test: generate button populates content field (mock LLM via test endpoint or skip on missing config)

## 3. AIDigest Entity + CRUD

- [x] 3.1 Add `ent/schema/aidigest.go`: name, prompt, sources (JSON), ttl_minutes, enabled + edge from `GeneralSettings`; run codegen
- [x] 3.2 Register `AIDigest` in `handlers/datasource_registry.go` with full CRUD (5 admin routes), sidebar link, dashboard count
- [x] 3.3 Create the AI digest admin form template (name, prompt, multi-select of RSS/News feeds, TTL, enabled) with live preview wiring
- [x] 3.4 Add `datasource/aidigest.go` implementing `Datasource.GetPNG` with single-flight TTL cache keyed by digest ID
- [x] 3.5 Wire `AIDigest` into `loadSources` in `handlers/websocket.go` (source name `AI: <name>`)
- [x] 3.6 Add manual-refresh button + endpoint on the digest admin form that invalidates the cache
- [x] 3.7 Playwright test: digest CRUD flow (create, edit, disable, delete)

## 4. Digest Rendering + Degradation

- [x] 4.1 Implement digest generation: fetch headlines from referenced RSS/News feeds (reuse existing fetch helpers), call LLM for 2-3 one-line items, render via `render.RenderText`
- [x] 4.2 Fallback: stale-cache-on-failure and placeholder-on-no-cache paths
- [x] 4.3 Unit tests: cached render within TTL, single-flight concurrent calls, stale render on failure, placeholder when no cache
- [x] 4.4 Run `task pre-push` (gofmt, tests, build) and fix any failures
