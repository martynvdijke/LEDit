## 1. Embed Access Primitives

- [ ] 1.1 Add `"embed"` to `guestValidScopes` (`handlers/guest_remote.go:22`) and update the scope-validation error message; add an Embed checkbox to the guest-remotes scope form in `web/templates/admin/guest_remotes.html` (the inline script collects all `input[type=checkbox][value]` scopes, verify the new value is posted)
- [ ] 1.2 Extend `handlers/guest_remote_test.go`: creating a token with the `embed` scope succeeds, unknown scopes still fail, and a legacy token without `embed` is not granted it
- [ ] 1.3 Refactor the guest-secret lookup in `GuestAuthMiddleware` (`handlers/guest_remote.go:73`) into a shared helper so it can be reused with a different cookie name; keep `GuestAuthMiddleware` behavior byte-identical for `/api/guest/*`
- [ ] 1.4 Implement `EmbedAuthMiddleware` in `handlers/embed.go`: accepts the `ledit_embed` HttpOnly cookie, `X-Guest-Token`, or `Authorization: Bearer`; requires the `embed` scope; also accepts a valid admin session; returns `401`/`403` JSON without disclosing token metadata and never redirects
- [ ] 1.5 Implement `GET /embed` and `GET /embed/device/:id` page handlers rendering `web/templates/embed.html` with `Cache-Control: no-store` and `Vary: Cookie, Authorization`, and no server-rendered device/source data
- [ ] 1.6 Implement `POST /embed/session`: read the secret from the JSON/form body, validate it has the `embed` scope and is not revoked/expired, set the `ledit_embed` HttpOnly cookie (short max-age; `SameSite=None; Secure` when the request is HTTPS), and return `401` on failure — mirroring `FrameSession` (`handlers/frame.go:17`)
- [ ] 1.7 Add unit tests for `POST /embed/session`: valid token sets an HttpOnly cookie, missing/unknown/revoked/expired returns `401`, a token lacking `embed` returns `403`, and the secret never appears in the URL

## 2. Embed Feed Endpoints

- [ ] 2.1 Implement `GET /ws/embed` (shared): parse and clamp `w`/`h` via `clampPreviewSize` (`handlers/preview.go:19`, defaults 400×400), enforce the per-IP upgrade rate limit before `upgrader.Upgrade`, then call `serveFeed` with `loadSources` and a fresh per-connection `FeedController`
- [ ] 2.2 Implement `GET /ws/embed/device/:id`: `EmbedAuthMiddleware`, load the enabled device, default size to `device.Width`/`device.Height`, resolve sources with `composeDeviceSources`, and call `serveFeed` with the device overlay/panel settings — without calling `registerDeviceFeed`, updating `last_seen_at`, or incrementing `frames_served` (compare `HandleDevicePreviewWS`, `handlers/websocket.go:932`)
- [ ] 2.3 Add a frame-capture hook to `serveFeed` (`handlers/websocket.go:1050`): on every emitted frame that carries an `image`, copy the final post-slice bytes plus `source`/`next`/format/dimensions into a package-level bounded last-frame registry keyed by target and size (`shared@WxH`, `device:<id>@WxH`); do not store notification frames (no image) and let canonical frames overwrite transition ramps
- [ ] 2.4 Track the current cache key on `FeedController` (set alongside `SetCurrent` in `serveFeed`) so the snapshot fallback path can resolve the current slot and its theme
- [ ] 2.5 Unit tests: anonymous shared upgrade succeeds; device upgrade without token is rejected; device upgrade with an `embed` token succeeds; frames are rendered at the requested clamped size; `last_seen_at`/`frames_served` unchanged after a device embed connect/disconnect; over-limit upgrades return `429` with `Retry-After`

## 3. Embed Frontend

- [ ] 3.1 Add `web/templates/embed.html`: standalone chrome-free shell (no sidebar/base template), canvas for PNG plus a `<video>` for MP4, transparent page background, `image-rendering: pixelated`, and a `<script type="module" src="/static/assets/embed.js">` tag
- [ ] 3.2 Implement `web/frontend/embed.ts`: read `w`/`h`/`bg`/device from the page's `data-*` attributes; for device pages read the token from the URL fragment, `POST /embed/session`, remove the fragment, then open `/ws/embed/device/:id`; otherwise open `/ws/embed`; draw PNG frames to the canvas (decode once, no per-frame re-encode), play MP4 in the video element, keep the last frame on disconnect, reconnect with bounded exponential backoff (max 5), and show a static "invalid or expired" state on auth failure — no timers, no rAF loop, no login redirect
- [ ] 3.3 Add the `embed` entry to `vite.config.ts` and run `task assets`; verify `web/static/assets/embed.js` is emitted and `/static/assets/embed.js` is served
- [ ] 3.4 Add `tests/embed.spec.ts` (Playwright): shared `/embed` renders a frame from a seeded source and contains no admin nav/login link; `/embed?w=256&h=128` sizes the canvas; `bg=transparent` leaves the page background transparent; a device embed with a valid `embed` token renders, an invalid token shows the static error with no `/login` navigation, and a revoked token stops streaming

## 4. Frame Snapshot Endpoint

- [ ] 4.1 Implement `handlers/frame_snapshot.go` `GET /api/frame`: resolve target (`shared` default, `device=<id>`), clamp `w`/`h` (defaults 400×400 shared, device resolution for a device target), and write `Cache-Control: no-store` + `Vary: Cookie, Authorization`
- [ ] 4.2 Implement lookup order: (1) exact buffered frame at target+size → return the stored bytes with `X-LEDit-Source` and `X-LEDit-Frame-Age`; (2) fallback render of the current slot through `datasource.RenderThemed` + `defaultLKG.GetPNG` with the feed's cache-key/theme logic and send-time overlay (`applyOverlay`), setting `X-LEDit-Stale`/`X-LEDit-Stale-Age` when the LKG frame is served; (3) `415` JSON when the current content is video-only
- [ ] 4.3 Reuse `writeImageResponse` (`handlers/preview.go:339`) for `format=png|webp`, and enforce the `embed:frame` rate limit (per IP for shared, per token for device) with `429` + `Retry-After`
- [ ] 4.4 Unit tests: anonymous shared `GET /api/frame` returns `200 image/png` with no-store + Vary; `?device=<id>` anonymous returns `401`; with an `embed` token returns the device frame; `format=webp` returns `image/webp`; a seeded buffered frame is returned byte-identically with the age header; a fallback render works with no buffered frame; a video-current response is `415`; over-limit returns `429`

## 5. PWA & Documentation

- [ ] 5.1 Add `shortcuts` to `web/static/pwa/manifest.json` (embed entry) and `web/static/pwa/remote-manifest.json` (embed and send-photo entries) using existing maskable icons; validate both files parse as JSON
- [ ] 5.2 Update `web/static/pwa/sw.js`: bump `CACHE_NAME`, and bypass CacheStorage for `/embed`, `/api/frame`, and `/ws/` requests while keeping the remote shell's network-first behavior
- [ ] 5.3 Write `docs/embedding.md`: OBS Browser Source URL examples (shared and device token URL with `#` fragment), transparent background and size options, iframe snippet, reconnect/refresh guidance, and the security/cookie notes (no secret in query strings; HTTPS for cross-site embeds)
- [ ] 5.4 Link the new doc from `docs/index.md` and update `README.md` (public endpoints list includes `/embed`, `/ws/embed`, and `/api/frame`; OBS pointer)

## 6. Wiring & Verification

- [ ] 6.1 Wire all new routes in `handlers/server.go`: `GET /embed`, `GET /embed/device/:id`, `POST /embed/session`, `GET /ws/embed`, `GET /ws/embed/device/:id`, `GET /api/frame`; confirm no schema migration is required and `/ws/feed`, `/ws/device/:token`, and `/ws/device/:token/preview` are untouched
- [ ] 6.2 Run `go test ./...`, `task assets`, and `npx playwright test tests/embed.spec.ts`; fix failures
- [ ] 6.3 Run `task pre-push` (gofmt, tests, build) and fix failures
- [ ] 6.4 Run `openspec validate add-embed-surfaces --strict` and confirm it passes
