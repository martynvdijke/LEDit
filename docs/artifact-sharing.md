# Sharing artifacts

LEDit can export single artifacts (themes, layouts, playlists, scenes, text
slides and pixel art) as portable **bundles**, and import bundles produced by
another instance. Bundles are plain zip files with a small, versioned envelope —
there is no code execution, no file IO and no template evaluation anywhere in the
path.

Open **Admin → Share Artifacts** (`/admin/artifacts`) to export one artifact or
import a bundle from a file, a URL, or the remote catalog.

## Bundle layout

A bundle is a zip archive containing at most three entries:

| Entry | Required | Description |
|-------|----------|-------------|
| `manifest.json` | yes | Envelope: `schema`, `kind`, `name`, `version`, `author`, `description`, `created_with`, `requires[]`, `payload_sha256` |
| `payload.json` | yes | `{"kind": "<kind>", "entity": {…}}` — the artifact row as a field map |
| `preview.png` | no | Optional preview, valid PNG, ≤ 1 MiB and ≤ 1024×1024 |

`payload_sha256` is the hex SHA-256 of the exact `payload.json` bytes, so a
tampered payload is always detected before anything is written.

```json
{
  "schema": 1,
  "kind": "theme",
  "name": "Neon",
  "version": "1.0.0",
  "author": "martyn",
  "description": "A neon-on-black theme",
  "created_with": "1.1",
  "requires": [{ "kind": "datasource", "type": "weather", "name": "#3" }],
  "payload_sha256": "9f2c…"
}
```

Row fields use the same names as the backup format, with `id`, `created_at` and
`updated_at` omitted so identical artifacts produce identical bytes. Supported
kinds are `theme`, `layout`, `playlist`, `scene`, `text-slide` and `pixel-art`;
`effect-preset` is reserved for a future version and is rejected for now (as is
any `schema` newer than this instance understands).

### Degradation rules

- **Secrets are never exported.** Fields that look like credentials are stripped
  on the way out and again on the way in; the import also reports a
  `secret_stripped` warning.
- **Missing references are not fatal.** A playlist that points at a datasource
  you have not configured imports fine and reports a `ref_missing` warning; fix
  the reference later in the normal editor.
- **Unknown fields are dropped** with an `unknown_field` warning.
- **Executable-looking fields are refused.** Payload keys such as `exec`,
  `script`, `command`, `entrypoint` or `hook` abort the import with
  `validation_failed`.
- **Built-in themes** can be exported only if they are not built in, and can
  never be replaced — rename or skip instead.

## API

All endpoints require an admin session or an admin API token. Errors use a stable
shape: `{"error":"<code>","message":"…","details":{…}}`.

| Method | Endpoint | Purpose |
|--------|----------|---------|
| `GET` | `/admin/api/artifacts/export/:kind/:id` | Download one artifact bundle |
| `POST` | `/admin/api/artifacts/import` | Import a bundle (multipart `file`, raw body, or `url`) |
| `GET` | `/admin/artifacts` | Share page: export list + import form |
| `POST` | `/admin/artifacts/import` | HTML wrapper: import and redirect back |
| `GET` | `/admin/artifacts/catalog` | Remote catalog browsing |
| `POST` | `/admin/artifacts/catalog/install` | Install a catalog bundle |

Import accepts the optional form fields `name`, `on_conflict`
(`rename` | `skip` | `replace`), `sha256` (whole-file digest to verify) and
`dry_run=1`. A dry run validates the bundle, lists conflicts and reference
warnings, and creates nothing. Installing a name that already exists returns
`409 name_conflict` unless you choose a conflict strategy.

Error codes: `unauthorized`, `invalid_bundle`, `invalid_zip`, `invalid_json`,
`missing_manifest`, `missing_payload`, `unsupported_version`, `unsupported_kind`,
`bundle_too_large`, `invalid_path`, `checksum_mismatch`, `name_conflict`,
`validation_failed`, `fetch_failed`, `host_not_allowed`, `import_failed`.

### Deep links

```
ledit://import?url=https://example.com/ledit-theme.zip&sha256=<hex>
```

LEDit does **not** register an OS protocol handler. The convention exists so a
shared link can be turned into
`/admin/artifacts?url=…&sha256=…`, which only **prefills** the import form. The
server fetches nothing until the admin presses Import.

## Remote catalog

Set `ARTIFACT_CATALOG_URL` to a JSON index and LEDit renders
`/admin/artifacts/catalog` with one-click installs:

```json
{
  "artifacts": [
    {
      "kind": "theme",
      "name": "Neon",
      "version": "1.0.0",
      "author": "martyn",
      "description": "A neon-on-black theme",
      "bundle_url": "https://example.com/ledit-theme.zip",
      "preview_url": "https://example.com/neon.png",
      "sha256": "9f2c…",
      "size": 4096,
      "created_with": "1.1"
    }
  ]
}
```

- `sha256` and `size` are verified against the downloaded bytes before the bundle
  is validated.
- `preview_url` is loaded by your browser; the server never fetches previews.
- Installs run the exact same validation, conflict and transaction path as a
  manual upload.

Outbound fetches are hardened:

| Variable | Effect |
|----------|--------|
| `ARTIFACT_CATALOG_URL` | Index URL (unset = catalog disabled) |
| `ARTIFACT_CATALOG_HOSTS` | Optional comma-separated host allowlist |

Only `http`/`https` are permitted, redirects are never followed, the client times
out after 10 s, the index is capped at 256 KiB and bundles at 8 MiB. Without an
allowlist, the host must resolve exclusively to public addresses — loopback,
private, link-local, multicast and unspecified ranges are refused. Set
`ARTIFACT_CATALOG_HOSTS` to allow a LAN mirror.
