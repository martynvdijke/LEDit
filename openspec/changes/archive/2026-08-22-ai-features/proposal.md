# Change Proposal: ai-features

## Why

LEDit already ships a fully-wired `AISettings` entity (provider, API key, model, endpoint) with an admin form and a test-connection button — but nothing in the codebase actually calls an LLM. The wall displays raw data, never insight. This change puts that dormant configuration to work: it lets the wall generate content, not just render it.

## What Changes

1. **AI text slide generation** — the text slide admin form gains a "Generate with AI" button. An admin enters a prompt (or uses a template), the server calls the configured LLM provider, and the response is injected into the slide content field for review before saving. The LLM is told the target is a small LED display (short output, no markdown).

2. **AI news digest** — a new `AI Digest` datasource entity. It fetches headlines from one or more configured RSS/news feeds, asks the LLM for a short digest (2–3 key items, tuned for a 64×64 display), and renders the result on the wall. Digests are cached for a configurable TTL (default 30 min) so the feed loop never hammers the LLM API — the digest only refreshes when the cache expires or is manually refreshed.

3. **Provider-agnostic LLM client** — a small OpenAI-compatible chat-completions client (`/chat/completions` with `Authorization: Bearer <key>`, honoring the configured `endpoint` and `model`). This covers OpenAI, Anthropic-compatible gateways, Ollama, LM Studio, and most self-hosted options already representable in `AISettings`.

4. **Graceful degradation** — any LLM failure (timeout, bad key, provider down) never breaks the feed: the digest falls back to a placeholder render, and the generate button surfaces an error message. The wall keeps cycling.

## Capabilities

### New Capabilities
- `ai-content`: LLM-powered content generation for LEDit — AI-generated text slides and AI news digests, with caching, provider-agnostic client, and graceful degradation.

### Modified Capabilities
- (none — existing capabilities keep their requirements; this is additive)

## Impact

- New `AIDigest` ent entity (name, prompt, sources JSON, TTL, enabled) with edges from `GeneralSettings`, full CRUD (5 admin routes), sidebar link, dashboard count, and feed wiring via `handlers/datasource_registry.go`.
- New `datasource/ai.go` LLM client + `datasource/aidigest.go` datasource implementation.
- Text slide form template + admin handler change (generate button + endpoint).
- No changes to existing datasources, feed message format, or WebSocket protocol.
- Dependency: none new at runtime beyond what `AISettings` already stores (plain HTTP, no Go SDK).
