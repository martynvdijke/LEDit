# Authelia OIDC SSO

LEDit can delegate browser sign-in to an [Authelia](https://www.authelia.com/)
OpenID Connect provider while keeping password login as a fallback. This page
covers the server-side configuration, the Authelia client registration, the
reverse-proxy exceptions the flow needs, how to verify it, and how to roll it
back.

The integration is implemented in `handlers/oidc.go` and reuses the existing
`session` cookie, so OIDC sessions and password sessions behave identically for
every route, middleware, and API token check.

## Configuration

| Variable | Meaning |
| --- | --- |
| `OIDC_ENABLED` | `true`/`1` turns the routes on. Anything else returns `404 oidc_disabled`. |
| `OIDC_ISSUER_URL` | Issuer URL. Discovery is performed live at login (`<issuer>/.well-known/openid-configuration`). |
| `OIDC_CLIENT_ID` | Client id registered in Authelia (default `ledit`). |
| `OIDC_CLIENT_SECRET_FILE` | Preferred secret source: path to a file containing the secret. |
| `OIDC_CLIENT_SECRET` | Fallback for local development only. Never commit a value. |
| `OIDC_REDIRECT_URL` | Must match the Authelia client exactly, e.g. `https://ledit.vandijke.xyz/api/auth/oidc/callback`. Also decides whether cookies are set `Secure` (https → yes). |
| `OIDC_SCOPES` | Space or comma separated, default `openid email profile groups`. |

`docker-compose.yml` and `.env.example` already wire these up; enable by setting
`OIDC_ENABLED=true` and pointing `OIDC_CLIENT_SECRET_FILE` at a mounted secret.

Routes:

- `GET /api/auth/oidc/login` — sets short-lived `oidc_state`, `oidc_nonce`, and
  `oidc_verifier` cookies, then redirects to the provider with PKCE (S256).
- `GET /api/auth/oidc/callback` — validates state/nonce/PKCE, exchanges the
  code, verifies the ID token through the provider JWKS, links or provisions the
  user, and issues the normal `session` cookie before redirecting to `/admin/`.
- `GET /api/auth/oidc/logout` — clears the local session, then redirects to
  `<issuer>/logout` with a post-logout redirect back to the LEDit base URL.

## Register the Authelia client

Add this to Authelia's `identity_providers.oidc.clients` and restart Authelia
(replace the secret and hostnames as needed):

```yaml
identity_providers:
  oidc:
    clients:
      - client_id: ledit
        client_name: LEDit
        client_secret: '$plaintext$REPLACE_WITH_A_GENERATED_SECRET'
        public: false
        authorization_policy: one_factor
        consent_mode: explicit
        token_endpoint_auth_method: client_secret_post
        redirect_uris:
          - https://ledit.vandijke.xyz/api/auth/oidc/callback
        scopes:
          - openid
          - email
          - profile
          - groups
        grant_types:
          - authorization_code
        response_types:
          - code
        enforce_pkce: true
        pkce_challenge_method: S256
```

Notes:

- `client_secret_post` matches LEDit's default token request style;
  `client_secret_basic` also works (the token endpoint accepts both).
- `consent_mode: explicit` keeps a visible consent step. Set `implicit` to skip
  it for a single-user homelab.
- Put the secret in the file referenced by `OIDC_CLIENT_SECRET_FILE` (for
  example a Docker secret) and never in `.env`, compose files, or git.

### Groups become roles

The ID token must carry the `groups` claim. LEDit grants the `admin` role when
`groups` contains `admins`, otherwise the session is a `viewer`. The role is
re-synced from `groups` on every login, so removing the group in Authelia takes
effect at the next sign-in.

New users are auto-provisioned on first login: username derived from the email
local part (made unique against existing accounts), no usable password, and
`auth_method=oidc`. Existing users are matched primarily by the `oidc_sub`
subject, falling back to a verified email match — in that case the existing
username and password hash are preserved, so password login keeps working for
the same account. Logins without a verified email address are rejected.

## Reverse proxy exceptions

Forward-auth in front of LEDit breaks the callback: the browser reaches
`/api/auth/oidc/callback` without an Authelia session yet, so the proxy would
redirect instead of letting LEDit finish the flow. Give the LEDit proxy host its
own access list with **no** forward-auth (LEDit enforces its own authentication),
or at minimum bypass these paths:

- `/api/auth/oidc/login`
- `/api/auth/oidc/callback`
- `/api/auth/oidc/logout`

In Nginx Proxy Manager create (or edit) the proxy host for
`ledit.vandijke.xyz`, set **Access List → Publicly Accessible**, and make sure
the host forwards to the LEDit container. Public reads such as `/`,
`/api/health`, and `/api/trmnl/stats` stay anonymous by design; anything
authenticated is still checked by LEDit's own middleware.

## Verify the deployment

In-process coverage already exercises the whole flow against a stub provider
(`handlers/oidc_stub_test.go`, `handlers/oidc_link_test.go`,
`handlers/oidc_flow_test.go`): discovery-driven redirect with PKCE, happy-path
token exchange, unverified email / wrong nonce / failed exchange rejection,
group-driven role sync on re-login, session equivalence with password login,
viewer inability to mutate, logout clearing the session, and
`OIDC_ENABLED=false` rollback behaviour.

To verify a real deployment:

1. **Public reads stay anonymous** — expect `200` from each:

   ```sh
   curl -s -o /dev/null -w '%{http_code}\n' https://ledit.vandijke.xyz/
   curl -s -o /dev/null -w '%{http_code}\n' https://ledit.vandijke.xyz/api/health
   curl -s -o /dev/null -w '%{http_code}\n' https://ledit.vandijke.xyz/api/trmnl/stats
   ```

2. **Authentication is enforced** — the web routes redirect and the API answers
   `401` with a `WWW-Authenticate` challenge:

   ```sh
   curl -s -o /dev/null -w '%{http_code}\n' -X POST https://ledit.vandijke.xyz/api/feed/pause
   ```

3. **Browser login** — open `https://ledit.vandijke.xyz/login`, confirm the
   **Login with Authelia** button appears, sign in, and check you land on
   `/admin/` as an admin (or as a viewer when your Authelia account is not in
   `admins`).

4. **Session still works everywhere** — reload the admin UI, then confirm API
   access with the same session cookie works and that a viewer session is
   refused (`403 insufficient_role`) on a mutation such as
   `POST /api/devices/<id>/brightness`.

5. **API tokens unaffected** — `GET /api/devices` with
   `Authorization: Bearer <token>` still returns `200` (tokens are managed at
   `/admin/api-tokens`).

6. **Logout** — use logout in the UI: the local session is cleared and you are
   redirected to Authelia's logout, after which `/admin/` requires a fresh
   sign-in.

## Rollback

Password login stays enabled while OIDC is on, so rolling back is a config
change, not a code change:

1. Set `OIDC_ENABLED=false` (or remove it) and restart LEDit. The
   `/api/auth/oidc/*` routes then return `404 oidc_disabled`, the login page
   stops showing the Authelia button, and password login, sessions, and API
   tokens keep working unchanged.
2. Optionally remove the client from Authelia and the `OIDC_*` variables from the
   deployment.
3. Users provisioned through OIDC remain in the database. They have no usable
   password (`auth_method=oidc`), so they cannot sign in unless an admin sets one
   at `/admin/users` — do that before disabling OIDC if those accounts must keep
   access.
