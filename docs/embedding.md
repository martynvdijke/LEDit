# Embedding the wall

LEDit can serve a chrome-free **embed surface** that shows the live wall (or a
single device) anywhere you can put a browser: OBS Browser Sources, iframes,
digital-signage players or a kiosk browser. The page has no admin navigation,
never redirects to `/login`, and only decodes the frames the server pushes —
there is no polling and no client-side re-encoding.

## Endpoints

| Method | Endpoint | Purpose | Auth |
|--------|----------|---------|------|
| `GET` | `/embed` | Shared wall page (all connected sources) | none |
| `GET` | `/embed/device/:id` | Single-device page | `ledit_embed` cookie or `X-Guest-Token` |
| `POST` | `/embed/session` | Exchange a guest secret for the embed cookie | guest secret in the body |
| `GET` | `/ws/embed` | Shared wall frame stream | none |
| `GET` | `/ws/embed/device/:id` | Device frame stream | `ledit_embed` cookie, `X-Guest-Token` or bearer token |
| `GET` | `/api/frame` | One-shot PNG/WebP snapshot of the current frame | shared: none; device: embed cookie/token or admin session |

All embed and snapshot responses send `Cache-Control: no-store` (pages and
snapshots also send `Vary: Cookie, Authorization`), and the service worker never
caches `/embed`, `/api/frame` or `/ws/` requests.

## OBS Browser Source

Use one of these URLs as a **Browser** source. Set the source's width/height to
match the `w`/`h` values (or leave the page to scale itself with `max-width`).

Shared wall, transparent background, 256×128:

```
https://ledit.example.com/embed?w=256&h=128&bg=transparent
```

A single device (create a guest token with the **embed** scope under
Admin → Guest Remotes first):

```
https://ledit.example.com/embed/device/3#<guest-secret>
```

The `#<guest-secret>` part is the URL fragment. It is sent to the browser only —
never to the server in the request line, never in access logs, and never in
`Referer` headers. The page immediately exchanges it for a short-lived
HttpOnly `ledit_embed` cookie and removes the fragment from the address bar.

### Query parameters

| Parameter | Values | Default | Notes |
|-----------|--------|---------|-------|
| `w` | 8–1024 | 400 (shared), device width | Frame width. Values below 8 map to 64; above 1024 clamp to 1024. |
| `h` | 8–1024 | 400 (shared), device height | Frame height, same clamping. |
| `bg` | `transparent` or `rrggbb` | `transparent` | Page background. Any other value falls back to transparent. |

Device pages default to the device's configured resolution, so
`/embed/device/3#<secret>` needs no size parameters.

## iframe

```html
<iframe
  src="https://ledit.example.com/embed?w=256&h=128&bg=transparent"
  width="256" height="128"
  style="border:0;background:transparent"
  allow="autoplay"
  title="LEDit wall">
</iframe>
```

For a device embed, put the fragment on the `src` exactly as above.

## Behaviour and reconnect

- Frames are pushed by the server at the source's display cadence; the client
  only decodes and draws them. There is no timer or `requestAnimationFrame`
  loop.
- If the connection drops, the last frame stays on screen and the client
  reconnects with bounded exponential backoff (up to 5 attempts, capped at 30 s
  between tries).
- An invalid, revoked or expired device secret shows a static
  “Live view unavailable — this link is invalid or expired.” message with no
  login redirect. Reload the page (or recreate the token) to retry.

## Snapshots

`GET /api/frame` returns the current frame as an image, useful for dashboards,
notifications or wall-clock style refreshes:

| Parameter | Values | Notes |
|-----------|--------|-------|
| `w`, `h` | 8–1024 | Same clamping as the embed page. |
| `format` | `png` (default), `webp` | `webp` re-encodes the frame. |
| `device` | device id | Selects a device frame; requires an embed token or admin session. |

Response headers report provenance: `X-LEDit-Source` (the displayed source) and
`X-LEDit-Frame-Age` (seconds since the frame was streamed) for a live buffered
frame, or `X-LEDit-Stale: 1` with `X-LEDit-Stale-Age` when a last-known-good
render was served. Video-only content returns `415` with a JSON body.

## Security notes

- The guest secret is only ever accepted in the URL **fragment** (device embed
  page) or a POST body (`/embed/session`). It is never accepted from a query
  string or a path segment.
- Embed pages and streams require HTTPS for cross-site use: the embed cookie is
  set `SameSite=None; Secure` on HTTPS, so browsers only accept it over TLS.
- Embed-scoped tokens are read-only and can be revoked at any time under
  Admin → Guest Remotes. Revocation takes effect for new sessions immediately;
  already-open connections end when the page reconnects.
- Embed page loads, WebSocket upgrades and snapshot requests are rate limited
  per client IP (and per token for token clients) with `429` +
  `Retry-After`. The limits are independent of other routes, so a busy embed
  never throttles `/ws/feed`, `/remote` or the admin UI.
