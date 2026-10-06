# Spec: embed-surfaces

## ADDED Requirements

### Requirement: Embeddable live widget page

The system SHALL serve a self-contained embed page at `GET /embed` and `GET /embed/device/:id` that renders the live wall feed without admin navigation, sidebars, telemetry, playback controls, or login call-to-action. The page SHALL work as an OBS browser source and inside an `<iframe>` and SHALL never redirect the viewer to `/login`.

#### Scenario: Shared embed page loads

- **WHEN** an anonymous client requests `GET /embed`
- **THEN** the response is `200` with an embed shell containing a canvas/video surface and no admin navigation, source queue, device roster, or login call-to-action

#### Scenario: Device embed page loads

- **WHEN** a client requests `GET /embed/device/<id>`
- **THEN** the response is `200` with the same chrome-free shell and no device data rendered server-side

#### Scenario: Invalid token never loops to login

- **WHEN** a device embed page cannot obtain a valid embed token
- **THEN** it shows a static "invalid or expired" state in place and does not redirect to `/login`

### Requirement: Configurable frame presentation

The embed SHALL accept `w` and `h` query parameters for the rendered frame size, clamped to the same 8–1024 bounds as `clampPreviewSize` (`handlers/preview.go:19`), and a `bg` parameter for the page background (`transparent` by default, or a hex color). The frame SHALL be scaled to fit without re-encoding or re-rendering it client-side, preserving crisp pixel-art edges.

#### Scenario: Explicit size applied

- **WHEN** a client requests `/embed?w=256&h=128`
- **THEN** the feed streams frames rendered at 256×128 and the canvas is sized for that aspect

#### Scenario: Sizes clamped

- **WHEN** a client requests a `w` or `h` below 8 or above 1024
- **THEN** the value is clamped to `[8, 1024]` and the feed renders at the clamped size

#### Scenario: Transparent background default

- **WHEN** a client requests `/embed` without `bg`
- **THEN** the page background is transparent and only the frame's own pixels are painted

#### Scenario: Device embed defaults to the device resolution

- **WHEN** a client requests `/embed/device/<id>` without `w`/`h`
- **THEN** frames stream at the device's configured width×height

### Requirement: Same render pipeline as the wall

Embed feed endpoints SHALL produce frames through the same `serveFeed` pipeline and source resolution used by the shared feed (`loadSources`, `handlers/websocket.go:129`) and device feeds (`composeDeviceSources`, `handlers/websocket.go:382`). The client SHALL only decode and draw the received frames. Opening an embed SHALL NOT update a device's `last_seen_at`, increment `frames_served`, or alter any other connection's feed control state.

#### Scenario: Shared embed uses the shared pipeline

- **WHEN** `/ws/embed` streams the shared feed
- **THEN** frames come from `loadSources` rendered through `serveFeed` at the requested size, including overlays, incidents, and panel slicing

#### Scenario: Device embed uses per-device resolution

- **WHEN** `/ws/embed/device/<id>` streams an enabled device
- **THEN** frames come from `composeDeviceSources` with that device's overlay and panel configuration

#### Scenario: No device liveness side effects

- **WHEN** a device embed connects or disconnects
- **THEN** the device's `last_seen_at` and `frames_served` are unchanged

#### Scenario: No client-side re-render

- **WHEN** a frame message arrives at the embed client
- **THEN** the client draws the received image/video without re-rendering the source or running a second rendering pipeline

### Requirement: Deterministic update cadence and reconnect

The embed SHALL render frames as the server pushes them, on the feed's own cadence (global timeout or device `refresh_interval`), with no client polling loop, interval timer, or animation loop. On disconnect the embed SHALL keep the last frame visible and reconnect with bounded exponential backoff. MP4 frames SHALL render in a video element.

#### Scenario: Frames arrive on feed cadence

- **WHEN** the server advances to the next source and pushes a frame
- **THEN** the embed displays it without waiting for a client timer

#### Scenario: Reconnect keeps last frame

- **WHEN** the embed WebSocket closes
- **THEN** the last frame remains visible, a reconnecting state is shown, and reconnection is attempted with bounded backoff

#### Scenario: Video frame plays

- **WHEN** the feed emits an `MP4` frame
- **THEN** the embed plays it in a video element instead of the PNG canvas

### Requirement: Public shared embed access

The shared embed (`GET /embed`, `GET /ws/embed`) SHALL be reachable without a session or token, consistent with the already-public `/ws/feed`, and SHALL NOT require an admin session. It SHALL NOT expose the device roster, source queue, notification history, or any controls.

#### Scenario: Anonymous shared embed streams

- **WHEN** an unauthenticated client loads `/embed` and connects to `/ws/embed`
- **THEN** the connection is accepted and streams shared feed frames

#### Scenario: No privileged detail

- **WHEN** the shared embed shell and client render
- **THEN** they contain no device list, queue, notification history, or control affordances

#### Scenario: Device target is not public

- **WHEN** an unauthenticated client requests `/embed/device/<id>` or `/ws/embed/device/<id>` without a valid token or admin session
- **THEN** the embed feed is not authorized (static error page or rejected upgrade) and no device frames are sent

### Requirement: Device embed authorization and scope lifecycle

Device embeds SHALL require either a valid admin session or a guest token carrying the read-only `embed` scope. `embed` SHALL be part of the closed guest scope set and creatable/revocable through the existing guest-remote admin surface, using the existing hashed-token storage, expiry, and revocation semantics. Tokens without the scope SHALL be rejected with `403`, and invalid, revoked, or expired tokens with `401`, without disclosing token metadata.

#### Scenario: Token with embed scope authorized

- **WHEN** a client presents a valid guest token whose scopes include `embed`
- **THEN** the device embed feed is authorized and streams that device's frames

#### Scenario: Token without scope rejected

- **WHEN** a valid guest token that lacks the `embed` scope requests a device embed
- **THEN** the request is rejected with `403 insufficient_scope` and no frames are sent

#### Scenario: Revoked token loses access

- **WHEN** an administrator revokes an embed token
- **THEN** subsequent embed requests with that token are rejected and active frames stop being served on the next request/reconnect

#### Scenario: Admin can create embed tokens

- **WHEN** an administrator creates a guest token with the `embed` scope on the guest-remotes page
- **THEN** the token is persisted with that scope and its secret is shown once

#### Scenario: Existing tokens unaffected

- **WHEN** a guest token created before this change is used
- **THEN** its existing scopes behave exactly as before and it is not granted `embed`

### Requirement: Embed session token exchange

A device embed SHALL accept the guest secret in the URL fragment only; the page SHALL exchange it at `POST /embed/session` for a short-lived HttpOnly cookie scoped to the embed routes. The secret SHALL NOT appear in query strings, request lines, server-rendered HTML, or application logs. Non-browser clients MAY authenticate the feed upgrade with an `X-Guest-Token` header or `Authorization: Bearer`.

#### Scenario: Fragment secret exchanged for a cookie

- **WHEN** a browser opens `/embed/device/<id>#<secret>` and the secret is valid
- **THEN** the page calls `POST /embed/session`, receives an HttpOnly embed cookie, removes the fragment from the visible URL, and connects to the embed feed

#### Scenario: Secret never in the request line

- **WHEN** the session exchange and feed connection happen
- **THEN** no request URL or query string contains the secret

#### Scenario: Header auth for non-browser clients

- **WHEN** a non-browser client upgrades to `/ws/embed/device/<id>` with a valid `X-Guest-Token` or Bearer secret carrying the `embed` scope
- **THEN** the upgrade is authorized without a cookie

#### Scenario: Invalid secret fails closed

- **WHEN** the fragment secret is missing, unknown, revoked, or expired
- **THEN** `POST /embed/session` returns `401` and the page shows the static error state

### Requirement: Embed rate limiting

Public embed surfaces SHALL be rate-limited per client IP with the existing fixed one-minute window: page loads, WebSocket upgrade attempts, and snapshot requests each have a limit. Exceeding a limit SHALL return `429` with `Retry-After` and `Cache-Control: no-store`, and WebSocket limits SHALL be enforced before the upgrade. Limits SHALL apply only to embed and snapshot endpoints and SHALL NOT change the behavior of `/ws/feed`, `/remote`, or admin routes.

#### Scenario: Page rate limit

- **WHEN** a client exceeds the embed page-load limit within a minute
- **THEN** further page requests return `429` with `Retry-After`

#### Scenario: Upgrade rate limit

- **WHEN** a client exceeds the embed WebSocket upgrade limit
- **THEN** the request returns `429` before upgrading and no stream is established

#### Scenario: Snapshot rate limit

- **WHEN** a client exceeds the snapshot request limit
- **THEN** further `GET /api/frame` requests return `429` with `Retry-After`

#### Scenario: Token clients limited separately

- **WHEN** a valid embed token is used
- **THEN** its requests are limited under token-scoped keys rather than sharing the anonymous IP bucket

### Requirement: Cache poisoning protection

Every embed and snapshot HTTP response SHALL set `Cache-Control: no-store`; page and snapshot responses SHALL additionally set `Vary: Cookie, Authorization`. Embed session responses SHALL not be cacheable. The service worker SHALL NOT cache embed or snapshot responses.

#### Scenario: Headers present

- **WHEN** `GET /embed`, `GET /embed/device/<id>`, or `GET /api/frame` responds
- **THEN** the response carries `Cache-Control: no-store` and, for pages and snapshots, `Vary: Cookie, Authorization`

#### Scenario: Anonymous response never served to a token client

- **WHEN** an intermediary has a cached anonymous response for an embed or snapshot URL
- **THEN** `Vary: Cookie, Authorization` prevents it from satisfying a token-authenticated request

#### Scenario: Service worker bypasses live paths

- **WHEN** the registered PWA service worker intercepts a request for `/embed`, `/ws/`, or `/api/frame`
- **THEN** it does not serve or store it from CacheStorage

### Requirement: Frame snapshot endpoint

The system SHALL expose `GET /api/frame` returning the current wall frame as `image/png`, or `image/webp` when `format=webp`, with optional `w`/`h` sizing (clamped as above) and an optional `device` selector. It SHALL default to `Cache-Control: no-store`, report the frame source, and report staleness when the frame is not fresh. Shared-feed snapshots SHALL be public; device snapshots SHALL require an `embed` token or admin session. When a live connection has emitted a frame for the matching target and size, the endpoint SHALL return that exact PNG; otherwise it SHALL render the current slot through the same feed render path and cache keys. No source configuration, credentials, or device roster SHALL appear in the response or headers.

#### Scenario: Public shared snapshot

- **WHEN** an anonymous client requests `GET /api/frame`
- **THEN** the response is `200` with `Content-Type: image/png`, `Cache-Control: no-store`, and a frame of the shared feed

#### Scenario: Device snapshot requires authorization

- **WHEN** an anonymous client requests `GET /api/frame?device=<id>`
- **THEN** the response is `401` and no device frame is returned

#### Scenario: Device snapshot with embed token

- **WHEN** a client with an `embed`-scoped token requests `GET /api/frame?device=<id>`
- **THEN** the device's current frame is returned as a PNG

#### Scenario: Exact frame when live

- **WHEN** the feed has just emitted a frame for the requested target and size
- **THEN** the endpoint returns those exact PNG bytes and an age/source header reflecting that emission

#### Scenario: Fallback render when idle

- **WHEN** no frame is buffered for the requested target and size (server restart or no live connection)
- **THEN** the endpoint renders the current slot through the same pipeline and cache keys and reports staleness if the last-known-good frame was used

#### Scenario: WebP output

- **WHEN** the request includes `format=webp`
- **THEN** the response is `200` with `Content-Type: image/webp`

#### Scenario: Video-only content

- **WHEN** the current wall content has no PNG representation (an MP4 video frame)
- **THEN** the endpoint returns `415` with a JSON error instead of a misleading image

#### Scenario: Not a duplicate of existing renders

- **WHEN** an operator wants an admin per-source render
- **THEN** `/admin/preview` continues to serve that purpose unchanged, and `/api/trmnl/*` continues to serve JSON only

### Requirement: PWA widget readiness and documentation

Both PWA manifests SHALL declare `shortcuts` linking to the embed view, and operator documentation SHALL describe OBS browser-source and iframe usage, including transparent background, sizing, and device token URLs.

#### Scenario: Manifest shortcuts

- **WHEN** `web/static/pwa/manifest.json` and `web/static/pwa/remote-manifest.json` are fetched
- **THEN** each is valid JSON containing at least one shortcut whose URL targets `/embed`

#### Scenario: OBS documentation

- **WHEN** an operator reads the embedding documentation
- **THEN** it shows the OBS browser-source URL, transparent-background and size examples, iframe markup, and how to build a device embed token URL
