# Design: ai-features

## Context

`AISettings` (ent schema: `provider`, `api_key`, `model`, `endpoint`) is fully configured in the admin UI but never used. Text slides (`TextSlideDS`) are static content strings rendered via `render.RenderText`. The feed loop (`serveFeed`) calls `GetPNG` per source per cycle; rendering must stay cheap and never block on slow upstreams.

## Goals / Non-Goals

**Goals**
- Make the configured LLM actually do something: generate slide content on demand, and produce periodic news digests for the wall.
- Provider-agnostic: work with any OpenAI-compatible chat-completions endpoint.
- Never let LLM latency or failure affect the feed loop: digests cached, errors degrade to placeholders.

**Non-Goals**
- No autonomous "AI controls the wall" agent, no streaming tokens, no image generation, no fine-tuning.
- No natural-language command parsing of `/api/display` text in this change.

## Decisions

### 1. OpenAI-compatible chat-completions client
New `datasource/ai.go` with a single `ChatCompletions(ctx, settings, messages, maxTokens)` helper:
- URL: `strings.TrimSuffix(settings.Endpoint, "/") + "/chat/completions"` (endpoint already includes `/v1` when needed by the provider).
- Auth: `Authorization: Bearer <api_key>`.
- Model from `AISettings.Model`; JSON-decodes `choices[0].message.content`.
- Timeout ~30s; returns typed errors so callers can degrade.
- **Alternative considered**: official OpenAI Go SDK. Rejected — adds a dependency for what is one POST; many self-hosted providers only expose the OpenAI-compatible shape anyway, and we already have a generic endpoint field.

### 2. AI text slide generation (on-demand, human-in-the-loop)
`POST /admin/textslides/generate` (admin auth): body has the prompt (or template id) + current slide fields.
- Handler builds a system prompt: "You write short text for a small LED matrix display (max ~2 lines, ~28 chars/line). No markdown, no quotes around the whole message."
- Returns JSON `{content: "..."}`; the form JS fills the content field. **Nothing is saved until the user submits the slide form** — the AI is a suggestion, the admin stays in control.
- **Alternative considered**: auto-saving generated slides to the DB. Rejected — silent writes to the wall violate least-surprise; review-then-save is one extra click.

### 3. AI digest datasource with TTL caching
New ent entity `AIDigest`:
| field | type | notes |
|---|---|---|
| name | string | display name |
| prompt | string | optional user overrides for the base digest prompt |
| sources | string | JSON list of RSS/news feed names (admin picks from existing `RssFeed` + `NewsFeed` entities) |
| ttl_minutes | int | default 30 |
| enabled | bool | default true |

- `AIDigestDS` implements `Datasource.GetPNG`. First call builds a cached digest: fetch headlines from the referenced feeds (reusing `rssfeed.go`/`news.go` fetch helpers), prompt the LLM for "2-3 key items, each on one short line", then render via `render.RenderText` (or `RenderDict`).
- Digest stored in an in-memory TTL cache keyed by digest ID; `GetPNG` returns the cached render while fresh, re-generates only on expiry or when `Cache-Control`-style manual refresh is triggered via the admin form. In-flight generation is single-flight (one mutex per digest) so a busy feed never stacks concurrent LLM calls.
- On LLM/feed fetch failure: return the fallback placeholder render (consistent with all other datasources) and keep the stale cache if one exists — **stale beats blank**.

### 4. Graceful degradation everywhere
Every LLM call goes through the same error path: log via `slog` (with the `source` attribute convention), return a typed error, let the caller decide (placeholder vs. keep-stale). The generate endpoint returns `422` with a message string that the form renders inline.

## Risks / Trade-offs

- [LLM provider down / rate limited] → digest keeps serving stale cached render; generate button shows inline error; feed never blocks.
- [Prompt injection from RSS content] → digest prompt instructs the model to treat feed content as untrusted data, never as instructions; user-provided prompt is admin-only so trust boundary is the admin.
- [Cost creep from frequent digests] → TTL cache (default 30 min) bounds LLM calls; manual refresh is explicit.
- [OpenAI-compatible shape varies by provider] → keep the client minimal (chat/completions, bearer token); `endpoint` field is already free-form so users can point at compatible gateways; test-connection endpoint already exists in `AISettings` admin.

## Migration Plan

Additive ent migration (`Schema.Create` path): new `AIDigest` table + edge from `GeneralSettings`. No changes to existing tables, message formats, or protocols. Rollback = drop the new table and the generate endpoint.

## Open Questions

- Should the digest render multi-page (rotating lines) when the digest exceeds the panel? (v1: single screen, truncated; multi-page can follow the marquee work in render-upgrades.)
- Do we want named prompt templates (e.g., "morning briefing", "sports recap") beyond a free-text prompt field? (v1: free text + one default.)
