## Why

Themable content — themes, visual layouts (`Composition`), playlists, scenes, text slides, and pixel art — is trapped inside the instance that authored it. The only movement path is a full-instance backup (`handlers/backup.go`), which is too coarse to share one theme or pixel artwork and carries the whole configuration surface; there is no discovery mechanism at all. The codebase already has both halves of the answer: a validated, checksum-aware bundle/import pipeline in `handlers/backup.go` (`Bundle`, `ValidateBundleWithDB`, `ImportBundle`, zip handling in `BackupImportHandler`) and a working install-from-URL + catalog flow for plugins (`installPluginFromURL` `handlers/plugins.go:365`, `fetchPluginCatalog` `handlers/plugins.go:462`, `plugin_catalog.html`). Artifact sharing extends those two proven models to the artifacts users actually want to trade.

## What Changes

- **Versioned artifact bundle**: one artifact per bundle, a zip containing `manifest.json` (schema version, `kind`, `name`, `version`, `author`, `description`, `created_with` LEDit version, `requires` refs, payload checksum), `payload.json` (the artifact entity), and an optional `preview.png`. Payload serialization reuses the backup row→map pipeline (`toMapSlice`, `stripSecrets` in `handlers/backup.go`) instead of inventing a parallel model. Kinds in v1: `theme`, `layout` (composition), `playlist`, `scene`, `text-slide`, `pixel-art`; `effect-preset` is reserved as a forward-compatible kind.
- **Export**: a new admin page `/admin/artifacts` lists shareable artifacts with a per-artifact Export action; `GET /admin/api/artifacts/export/:kind/:id` (session auth) downloads the bundle. Built-in themes are not exportable (they exist everywhere). Secrets are stripped on export.
- **Import**: file upload or URL, with a validation/dry-run pass (schema, caps, checksum, PNG decode) and conflict preview, then a transactional create/update. Name collisions return `name_conflict` and offer **rename / skip / replace**; replace updates in place so existing theme assignments survive. Every import is audit-logged like backup exports/imports.
- **Remote catalog**: `ARTIFACT_CATALOG_URL` points at a GitHub-hosted or plain HTTP JSON index listing `{kind, name, version, author, description, bundle_url, preview_url, sha256}`. A new `/admin/artifacts/catalog` page mirrors `plugin_catalog.html` with preview, author, kind, version, and integrity hash, plus one-click install into a chosen name. Fetching is SSRF-hardened: `ARTIFACT_CATALOG_HOSTS` allowlist, http/https only, redirects refused, loopback/private/link-local addresses denied unless explicitly allowlisted, 10 s timeout, 256 KiB index cap, 8 MiB bundle cap — the fetch path modeled on `fetchPluginCatalog` (`handlers/plugins.go:462`) but stricter.
- **Declarative-only and degradation**: no code execution, no script/exec fields, no secret material (pixel-art `api_token` is stripped; secret fields are never persisted). References to datasources, images, or playlists that do not exist on the target instance degrade to warnings in the import result — they never fail the import. Unknown kinds/versions are rejected with stable JSON error codes instead of partial imports.
- **Deep links + API**: `POST /admin/api/artifacts/import` (multipart, raw JSON, or `?url=`) with stable error codes (`invalid_bundle`, `unsupported_kind`, `unsupported_version`, `checksum_mismatch`, `bundle_too_large`, `name_conflict`, `fetch_failed`, `host_not_allowed`, …); a documented `ledit://import?url=…&sha256=…` convention that prefills the import page (the server does not register an OS handler).

## Capabilities

### New Capabilities
- `artifact-sharing`: Versioned single-artifact bundle format, admin export/import with conflict resolution, strict validation, transactional and audited imports, declarative-only enforcement with graceful reference degradation, remote catalog listing/install with SSRF hardening, and the stable API/deep-link surface.

### Modified Capabilities
- None — the behavior is additive. Export/import hooks are added to handlers owned by `theme-designer`, `layout-editor`, and their siblings but no requirement in those specs changes; `config-backup-restore` is explicitly not modified (whole-instance backup stays the migration path, artifact sharing is per-item).

## Impact

- **New server code**: `handlers/artifacts.go` (manifest type, kind registry, zip read/write, checksum, validation, export/import handlers, catalog fetch), `handlers/artifacts_test.go`.
- **New UI**: `web/templates/admin/artifacts.html` (artifact list + export + import + conflict dialog), `web/templates/admin/artifact_catalog.html`, `web/frontend/artifacts.ts` (+ emitted `web/static/assets/artifacts.js`).
- **Modified code**: `handlers/server.go` (routes under the admin group, mirroring the backup route block at `handlers/server.go:975`), `web/templates/admin/sidebar.html` (Artifacts link), optional Export actions on `themes.html`, `layouts.html`, `playlists.html`, `scenes.html`, `pixelarts.html`.
- **API surface**: `GET /admin/artifacts`, `GET /admin/artifacts/catalog`, `GET /admin/api/artifacts/export/:kind/:id`, `POST /admin/api/artifacts/import` — all session-auth admin; no bearer path (bundles are config).
- **Dependencies**: none — stdlib `archive/zip`, `crypto/sha256`, `image/png`, `net/http`; no new module.
- **Risk**: crafted bundles (zip bombs, path traversal) are capped and path-checked; SSRF via catalog/bundle URLs is allowlisted and redirect-refusing; secrets are stripped both ways; name collisions are explicit; reference rot degrades to warnings instead of failures.
