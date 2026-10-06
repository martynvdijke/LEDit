## Context

LEDit renders every frame through one path: `serveFeed` (`handlers/websocket.go:1050`) resolves sources, renders them with `datasource.RenderThemed`, applies theme/overlay/brightness and panel slicing, and pushes `{format, image, source, next}` JSON over a WebSocket. Three feed flavors exist today:

- `GET /ws/feed` (`HandleWS`, `handlers/websocket.go:703`) — the shared preview, hardcoded 400×400, public, controlled by the global `FeedController` (`handlers/feed_control.go:25`).
- `GET /ws/device/:token` (`HandleDeviceWS`, `handlers/websocket.go:730`) — per-device at its configured width/height/interval, device-token auth, marks `last_seen_at` and increments `frames_served`.
- `GET /ws/device/:id/preview` (`HandleDevicePreviewWS`, `handlers/websocket.go:932`) — admin-session preview keyed by device id, never touches liveness, independent `FeedController`.

Other relevant state: guest tokens with a closed scope set (`guestValidScopes` = pause/next/message/photo, `handlers/guest_remote.go:22`), hashed secrets, expiry/revocation, and a fragment→cookie exchange already used by the guest photo frame (`GET /frame` + `POST /frame/session`, `handlers/frame.go:12`); an in-process fixed-window rate limiter (`checkWindowRateLimit`, `handlers/guest_remote.go:134`) with a 429 helper; a last-known-good frame cache (`defaultLKG`, `handlers/lkg.go:214`) keyed `<type>:<id>@WxH|themeSig`; `/admin/preview` for admin, per-source on-demand PNGs (`handlers/preview.go:50`); and `/api/trmnl/{stats,messages}` as JSON-only public endpoints (`handlers/trmnl.go`).

Constraints: single Go process + SQLite, no cluster state; the public landing (`public-feed-surface`) requires `Cache-Control: no-store` + `Vary: Cookie, Authorization` and minimal anonymous information; `/ws/feed` is intentionally public; no heavyweight frontend framework (vanilla TS + Vite only); the embed must show exactly what the wall shows, not a parallel render pipeline.

Stakeholders: operators who want the wall in OBS or on a website/second screen; e-ink/TRMNL users who need a static image of the current frame; guests/devices that need a scoped, revocable view of one device.

## Goals / Non-Goals

**Goals:**
- One self-contained embed page (`/embed`, `/embed/device/:id`) with transparent background, configurable size, no chrome, no auth prompts, and deterministic push cadence.
- Serve embeds from the existing `serveFeed` pipeline so the pixels match the wall (overlays, incidents, alarms, panel slicing included).
- Public shared embed consistent with the already-public `/ws/feed`; per-device embeds behind a revocable read-only guest token with no secret in query strings.
- A stable `GET /api/frame` PNG snapshot for e-ink/TRMNL/static consumers, with strict `no-store` cache posture and staleness headers.
- Per-IP rate limits on all public embed surfaces and PWA entry points (shortcuts) plus OBS documentation.

**Non-Goals:**
- Native desktop tray apps, Electron wrappers, Chromecast/DLNA/Roku/Apple TV apps. *Why:* each needs per-platform packaging, signing, and platform SDKs (cast APIs, tvOS) to duplicate a surface OBS, browsers, and installed PWAs already provide; if casting is needed later it can build on the same public feed endpoints without changing this contract.
- Embed-side playback controls (pause/skip/next) or guest messaging; the embed is read-only.
- A new frontend framework, bundler, or sandboxed runtime; the widget is vanilla TS compiled by the existing Vite build.
- Per-embed source selection or playlists beyond `device=<id>`; content selection stays server-side (global list / device assignment).
- Changing `/ws/feed` or the admin device preview behavior; both stay byte-stable.
- A public-feed or embed on/off setting in v1 (see D2).

## Decisions

### D1 — Separate `/embed` shell and `/ws/embed` endpoints, not modes of `/` or `/ws/feed`

`GET /` is auth-aware, chrome-heavy (`handlers/handlers.go:120`), and its cache headers exist to prevent public/authenticated mix-ups; the embed needs a fixed minimal shell, a public/device auth split, its own rate-limit keys, and configurable render size. `/ws/feed` stays untouched so its existing consumers (main page, guest mirror work) keep byte-identical behavior.

- *Why:* A distinct route is the smallest correct diff and gives one chokepoint for embed rate limits and cache headers.
- *Alternative:* `GET /?embed=1` — rejected: inherits sidebar/admin logic and auth-dependent rendering, forcing cache complexity onto the landing page.
- *Alternative:* Parameterize `/ws/feed` with `w`/`h` — rejected: changes an existing public contract and mixes rate-limit/auth semantics on one endpoint.
- *Alternative:* iframe `/` directly — rejected: chrome, no transparency, session-dependent landing.
- *Risk:* code duplication with `HandleWS`/`HandleDevicePreviewWS`; mitigated by having the new handlers call the same `serveFeed` + `loadSources`/`composeDeviceSources` functions with only sizing/auth differences.

### D2 — Public shared embed inherits `/ws/feed` visibility; no new enable toggle in v1

`/ws/feed` was deliberately kept public by `gate-feed-details-behind-auth` (only `GET /` was gated). A public `/embed` renders those exact frames; it adds no content that anonymous clients cannot already stream. Device content stays non-public because `HandleDeviceWS` is token-gated and `HandleDevicePreviewWS` is admin-gated.

- *Why:* introducing a schema-backed toggle whose off-state cannot actually hide `/ws/feed` would be security theater and a migration for no behavioral guarantee.
- *Alternative:* `EmbedSettings.public_enabled` singleton — rejected for v1: schema/migration plus admin UI for a switch that does not close the underlying public WebSocket; if `/ws/feed` is ever gated, the embed inherits that gate by construction.
- *Alternative:* signed URLs for the shared embed — rejected: cryptographic ceremony protecting already-public data; device embeds get the token path where it matters.
- *Note:* the embed page renders no device roster, source queue, notification history, or controls; the `source`/`next` fields already present on `/ws/feed` are not displayed.

### D3 — Device embeds use a new read-only `embed` guest scope, delivered via fragment → `POST /embed/session` → HttpOnly cookie

Browsers cannot set custom headers on WebSocket upgrades, so a device embed authenticates by cookie. The secret is supplied in the URL fragment (`/embed/device/3#<secret>`), which is never sent to the server, and exchanged once at `POST /embed/session` for a short-lived HttpOnly `ledit_embed` cookie, exactly mirroring `/frame` + `/frame/session` (`handlers/frame.go:17`). Non-browser clients (dashboards, scripts) may authenticate the WS upgrade with `X-Guest-Token`/Bearer instead. Admin sessions are also accepted for quick checks from the admin browser.

- *Why:* guest tokens already provide hashed storage, expiry, revocation, scope gating, and the proven fragment→cookie flow; `embed` slots into the closed scope set with no schema migration (`GuestToken.Scopes` is JSON).
- *Alternative:* reuse the device hardware token in the URL — rejected: leaks the long-lived device credential into browser history/logs and the `/ws/device/:token` path also marks the device seen and increments frame counters.
- *Alternative:* admin session cookie only — rejected: OBS does not share the operator's admin browser session and would produce login loops — exactly what the scope forbids.
- *Alternative:* HMAC signed URLs — rejected: a new secret to store/rotate/expire when revocable guest tokens already exist; signed URLs also surface in query strings and logs.
- *Alternative:* `?token=` query — rejected: request lines, referrers, screenshots.
- *Risk:* cross-site cookie rules. The device embed is designed for OBS, same-site pages, and trusted placements; when served over HTTPS the session cookie is set `SameSite=None; Secure` so cross-site iframe embeds can send it, and over plain HTTP it falls back to the default (cross-site embeds over HTTP are rejected by browsers anyway). The docs call this out.

### D4 — One render pipeline; the client draws, never re-renders

Embed connections call the existing `serveFeed` with `loadSources` (shared) or `composeDeviceSources` (device); `w`/`h` select the render dimensions before the pipeline (clamped 8–1024 via `clampPreviewSize`). The client decodes the pushed base64 frame once and draws it to a canvas (PNG) or plays it in a `<video>` (MP4), scaling only via CSS `object-fit: contain` + `image-rendering: pixelated`. Device embeds never touch `last_seen_at` or `frames_served`, and each connection gets its own `FeedController` (like the admin preview) so its state is independent.

- *Why:* guarantees the widget shows exactly the wall content — overlays, incidents, alarms, panel slicing — and avoids a whole class of divergence and CPU bugs.
- *Alternative:* a second lightweight renderer — rejected: duplicated rendering logic and visual drift.
- *Alternative:* re-encode/resize client-side — rejected: destroys pixel-art crispness and costs CPU; the server already renders at the requested size.
- *Risk:* sizing mismatch with `/ws/feed` defaults; shared embed defaults to 400×400 so the common case matches today's preview exactly, and device embeds default to the device's configured resolution.

### D5 — Cadence is server-driven; the client has no clock

The server pushes a frame when `serveFeed` produces one (global timeout or device `refresh_interval`); the embed draws on message and has no polling loop, no `setInterval`, and no `requestAnimationFrame` loop. On close it keeps the last frame and reconnects with bounded exponential backoff (max 5 attempts), identical in spirit to `web/frontend/feed.ts:91`.

- *Why:* "deterministic update cadence" means the wall's cadence; a client timer would lag or double-render, and a rAF loop burns CPU in OBS.
- *Alternative:* poll `GET /api/frame` periodically — preserved for non-WS consumers (e-ink/TRMNL) but rejected for the live embed: it re-renders or re-serves per poll and adds latency.

### D6 — `GET /api/frame` prefers the exact last-sent frame, with a same-pipeline fallback

The server keeps a tiny bounded registry of the most recent frame actually written to a feed connection, keyed by target and render size (`shared@WxH`, `device:<id>@WxH`), storing the final PNG bytes plus `source`, `next`, format, and timestamp. `serveFeed` updates it on every frame that carries an image (canonical, incident, alarm/scene, and animated re-renders; transition ramps are overwritten by the canonical frame that follows; notification frames have no image and do not update it). `GET /api/frame` matches target and exact `w`/`h`:

- hit → return the stored bytes byte-identically, with `X-LEDit-Source` and `X-LEDit-Frame-Age`;
- miss (no connection, restart, different size) → resolve the current slot (`GlobalFeed` current cache key; device's composed sources) and render through the same `datasource.RenderThemed` + `defaultLKG` + overlay code as `serveFeed`, honoring LKG staleness (`X-LEDit-Stale`, `X-LEDit-Stale-Age` like `AdminPreview`, `handlers/preview.go:134`);
- current content is video-only (MP4) → `415` with a JSON error, since neither e-ink nor image consumers can use it.

`format=webp` reuses `writeImageResponse` (`handlers/preview.go:339`). Default size: 400×400 shared, device resolution for `device=<id>`. Auth: shared public; `device=` requires an `embed` guest token or admin session. The handler is registered at `GET /api/frame` and is the only change to the public `/api` allowlist.

- *Why:* exact bytes satisfy "same frames"; the fallback keeps the endpoint useful when nothing is connected (e.g. TRMNL polling overnight), and the headers tell consumers how fresh the image is.
- *Alternative:* always re-render — rejected: defeats exactness and does redundant work for the common live case.
- *Alternative:* buffered-only with 503 when empty — rejected: brittle for e-ink pollers.
- *Alternative:* extend `/admin/preview` or `/api/trmnl/stats` — rejected: the former is per-source and admin-only, the latter is JSON; neither is the current wall frame.

### D7 — Rate limits reuse the in-process fixed-window limiter, keyed per IP or per token

Public surfaces: `embed:page:<ip>` (page loads), `embed:ws:<ip>` (upgrade attempts, checked before `upgrader.Upgrade`), `embed:frame:<ip>` (snapshots). Token surfaces: the same keys with `:tok:<id>` and higher caps. Exceeding returns `429` + `Retry-After` + `Cache-Control: no-store` through the existing `abortGuestRateLimited` (`handlers/guest_remote.go:163`).

- *Why:* the limiter already exists, is namespaced, and matches the single-instance constraint documented at `handlers/guest_remote.go:132`.
- *Alternative:* `golang.org/x/time/rate` or a shared store — rejected: a second rate-limit style (and multi-instance state) for no gain.

### D8 — Cache posture: `no-store` everywhere, `Vary` on pages and snapshots, and a service-worker bypass

`GET /embed`, `GET /embed/device/:id`, `POST /embed/session`, and `GET /api/frame` set `Cache-Control: no-store`; the page and snapshot responses also set `Vary: Cookie, Authorization` so a shared cache can never serve an authenticated device response to an anonymous request (the `public-feed-surface` rule). `sw.js` (registered only by `/remote`, `web/templates/remote.html:117`) currently caches every GET network-first; it gains a bypass for `/embed`, `/api/frame`, and `/ws/` and a bumped `CACHE_NAME`.

- *Why:* embeds mix public and token-authenticated responses on the same URL shapes; `no-store` + `Vary` prevents poisoning and stale frames served from CacheStorage.
- *Alternative:* cache the shared embed shell — rejected: the shell is tiny; caching only creates staleness after deploy.

### D9 — PWA shortcuts and OBS docs; native apps are explicit non-goals

Both manifests gain `shortcuts` (`/embed` on both, plus `/frame` on the remote manifest). `docs/embedding.md` documents OBS Browser Source setup (URL, width/height, transparent background, "shutdown when not visible" guidance), iframe snippets, and per-device token URLs, linked from `docs/index.md` and `README.md`.

- *Why:* shortcuts make the embed reachable from installed PWAs; docs make OBS adoption self-serve.
- *Alternative:* a generated per-device manifest — rejected: complexity for a shortcut list; device URLs are documented, not manifested.

## Risks / Trade-offs

- [Public embed widens frame reachability] → same content as the already-public `/ws/feed`; no roster/queue/controls UI; per-IP rate limits; documented.
- [Device token leakage] → fragment-only (never in HTTP request lines), exchanged for an HttpOnly cookie; header auth for server-side clients; no query-string tokens.
- [Cross-site iframe cookie rejection] → `SameSite=None; Secure` cookie when HTTPS, plain-HTTP fallback, documented host-relative/OBS guidance, header auth for non-browser renderers.
- [CPU from many simultaneous embeds] → per-IP limits cap connections; LKG cache shares expensive renders by source+size; frames are pushed only at rotation boundaries.
- [Snapshot diverges from the wall] → last-sent buffer preferred; fallback uses the same functions and cache keys; `X-LEDit-Frame-Age`/`X-LEDit-Stale` communicate freshness.
- [Proxy cache poisoning] → `no-store` + `Vary: Cookie, Authorization` on every embed/snapshot/session response; WebSockets are not cacheable; SW bypass.
- [Video frames in e-ink consumers] → `GET /api/frame` returns `415` for video-only content rather than pretending; documented.
- [OBS transparency expectations] → the frame's own background stays as rendered; `bg` only controls the page letterbox; documented in `docs/embedding.md`.

## Migration Plan

1. Add `embed` to `guestValidScopes` and the guest-remotes admin form; existing token rows are untouched (`Scopes` is a JSON column, no migration).
2. Add `handlers/embed.go`: embed pages, `POST /embed/session`, embed auth middleware, shared/device embed feed handlers, rate limits.
3. Add the `serveFeed` last-frame capture hook + buffer and `handlers/frame_snapshot.go`; wire `GET /api/frame`.
4. Add `web/templates/embed.html`, `web/frontend/embed.ts`, and the `embed` Vite entry; run `task assets`.
5. Update `manifest.json`, `remote-manifest.json`, and `sw.js`.
6. Write `docs/embedding.md`, link it from `docs/index.md` and `README.md`.
7. Rollback: remove the new routes and entry points; the only persisted artifact is optional `embed` scope strings in existing guest token rows, which older code ignores.

## Open Questions

- Should the shared embed get a kill switch if `/ws/feed` is ever gated? Assumed the embed inherits whatever gate `/ws/feed` has; a dedicated setting is deferred.
- Should `/api/frame` apply an e-ink/high-contrast post-process (`?eink=1`)? Assumed no — e-ink mode is a page presentation mode (`eink.css`, `ledit_eink` cookie), not a render transform.
- Should MP4-capable snapshots return the last PNG instead of `415`? Assumed `415` with a clear error is more honest; revisit if e-ink consumers need a still.
- Should embeds be capped by concurrent connections per IP in addition to the fixed-window rate? Assumed the rate limit plus long-lived temporal locality is sufficient for v1.
- Should shortcut labels be localized? Assumed English only, matching the rest of the manifests.
