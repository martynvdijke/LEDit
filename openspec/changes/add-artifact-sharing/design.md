## Context

LEDit stores shareable artifacts as Ent rows in one SQLite database: `Theme` (`ent/schema/theme.go`, unique `name`, `built_in` immutable seeds), `Composition` (`ent/schema/composition.go`, the visual layout editor's "layout", `regions` is JSON referencing sources by `source_type`/`source_id`), `Playlist` (`ent/schema/playlist.go`, `items` + `schedule_windows` JSON), `Scene` (`ent/schema/scene.go`, unique `name`, `triggers`/`actions` JSON), `TextSlide` (`ent/schema/textslide.go`, attached to `GeneralSettings` through the `TextSlides` edge by `AdminTextSlideCreate` at `handlers/handlers.go:2195`), and `PixelArt` (`ent/schema/pixelart.go`, `frames`/`bindings` JSON plus `api_url`/`api_token`). All admin CRUD is session-authenticated and wired in `handlers/server.go` (`/admin/themes`, `/admin/layouts`, `/admin/playlists`, `/admin/scenes`, `/admin/pixelarts`, `/admin/textslides`).

Two existing mechanisms frame this design:

- **Backup bundle precedent** (`handlers/backup.go`): a versioned `Bundle` (`handlers/backup.go:35`), row→map marshaling and secret stripping (`toMapSlice` `:119`, `stripSecrets` `:93`), validation/diff/import passes (`ValidateBundleWithDB` `:410`, `DiffBundle` `:483`, `ImportBundle` `:581`), zip safety caps (`maxUncompressedSize`/`maxZipEntries` `:27`, traversal checks in `BackupImportHandler` `:1241`), sha256 media verification (`:1275`), and audit logging through `LogStore.Submit` (`:1166`).
- **Plugin catalog precedent** (`handlers/plugins.go`): `installPluginFromURL` (`:365`) fetches one JSON document with an `http.Client{Timeout: 10s}`, a `PLUGINS_MANIFEST_HOSTS` allowlist (`manifestHostAllowed` `:351`), a 64 KiB body cap, and creates a disabled row — no executable download; `fetchPluginCatalog` (`:462`) fetches a `{"plugins":[…]}` index under `PLUGINS_CATALOG_URL` with a 256 KiB cap; `plugin_catalog.html` renders entries with an Install form posting back to `AdminPluginInstall` (`:430`).

Constraints: single process, SQLite, no new heavyweight dependency; admin-session auth only (as with backup); the server may run in a homelab where catalogs live on LAN hosts, so the fetch policy must be safe by default but explicitly overridable; artifacts are declarative configuration, never code.

Stakeholders: self-hosters sharing themes/pixel art/layouts between instances, community catalog authors hosting a JSON index on GitHub Pages or any static host.

## Goals / Non-Goals

**Goals:**
- One versioned, checksummed, single-artifact bundle format that can be exported from and imported into any LEDit instance.
- One-click discovery of artifacts from a remote catalog with preview, author, kind, version, and integrity hash.
- Strict, transactional, audited imports with an explicit rename/skip/replace answer for name collisions.
- Graceful degradation: missing datasource/playlist/image references warn instead of failing; secrets are stripped in both directions.
- Reuse the backup serialization and the plugin catalog fetch/install model rather than inventing parallel ones.

**Non-Goals:**
- Bulk or pack export (multi-artifact archives): the bundle is single-artifact by design; whole-instance migration remains `config-backup-restore`.
- Cross-instance live sync, versioning history, ratings, comments, or an upload/publish server — LEDit only consumes catalogs.
- Executing, unpacking, or installing any kind of code; no `exec`/`script`/`hook` fields exist in the schema.
- Registering an OS `ledit://` protocol handler from the server binary; the scheme is a documented convention that prefills the import page.
- Transporting secrets (`api_token`), device tokens, or credentials of any kind.
- A new database table: provenance is recorded in the existing audit log.

## Decisions

### D1 — Bundle is a zip with three fixed entries: `manifest.json`, `payload.json`, optional `preview.png`

Import sniffs the PK magic and reads entries the same way `BackupImportHandler` does (`handlers/backup.go:1229`): entry names limited to the allowlist, `filepath.Clean` traversal check, `maxZipEntries`-style count cap, and an uncompressed-total cap. `preview.png` is an optional real PNG (binary), which is why the container is zip rather than a JSON document with base64.

- *Why:* `archive/zip`, the caps, and the inspection pattern already exist in `handlers/backup.go`; a zip keeps the preview binary efficient and the payload diff-able; the standard library is the only dependency.
- *Alternative:* one JSON document with `preview_png_b64` — rejected: bloats the payload, hides binary content, and needs a second reading model.
- *Alternative:* a tarball or custom container — rejected: zip handling is already present and familiar.

### D2 — Manifest is versioned and payloads reuse the backup entity-map shape

```json
{
  "schema": 1,
  "kind": "theme",
  "name": "Midnight",
  "version": "1.0.0",
  "author": "someone",
  "description": "dark palette",
  "created_with": "v0.9.2",
  "requires": [{"kind": "datasource", "type": "weather", "name": "Home"}],
  "payload_sha256": "<hex of payload.json bytes>"
}
```

`payload.json` is `{"kind": "theme", "entity": {…}}`, where `entity` is produced by marshaling the Ent row to a map exactly as `ExportBundle` does (`json.Marshal(row)` → `map[string]any`), then `stripSecrets` (`handlers/backup.go:93`) and deletion of instance-local fields (`id`, `created_at`, `updated_at`). Because `encoding/json` sorts map keys, the marshaled payload is deterministic, so `payload_sha256` is stable across instances and safe to publish in a catalog index. Import maps the same keys back onto Ent create/update builders per kind, reusing the field handling in `importType` (`handlers/backup.go:635`) where it applies — but always through an explicit per-kind importer that creates new rows (new IDs), never the backup id-upsert path.

- *Why:* a single serialization model for the whole app; no per-kind structs to drift from the Ent schema; deterministic bytes make the checksum meaningful.
- *Alternative:* Go structs per artifact — rejected: parallel model that must be updated on every schema change.
- *Alternative:* embed backup's `Bundle` envelope — rejected: the backup envelope is multi-entity and expects same-instance FK IDs; a single-artifact envelope with its own schema version is what sharing needs.

### D3 — Kind registry: `theme`, `layout` (Composition), `playlist`, `scene`, `text-slide`, `pixel-art`, plus a reserved `effect-preset`

A registry maps `kind` to `{export, validate, apply}`. Validation reuses the existing parsers rather than re-implementing them: playlists go through `datasource.ParsePlaylistItems` (`handlers/playlists.go:54`) and `ParseScheduleWindows`/`ValidateWindows` (`handlers/schedule.go:63`,`:78`); layouts through `datasource.ParseRegions` (`datasource/compositor.go:56`) plus the bounds/shape checks of `validateRegionConfig` (`handlers/layout_editor.go:63`); scenes through `parseSceneTriggers`/`parseSceneActions` (`handlers/scenes.go:80`,`:92`); pixel art through `validPixelBindings` (`handlers/pixelart.go:17`) and a JSON object check on `frames`. Themes reuse the `hexColorPattern`/font-size bounds from `ent/schema/theme.go`. Built-in themes (`built_in = true`) are never exported or replaced, matching their immutability in `AdminThemeDelete` (`handlers/theme.go:452`).

`effect-preset` is registered as a known-but-unimplemented kind: today its bundles are rejected with `unsupported_kind` and a message naming the minimum LEDit version, and a future change adds the extractor/importer without touching the envelope. Unknown `schema` values are rejected with `unsupported_version`.

- *Why:* a closed key→logic map is what makes "declarative only" provable — `kind` selects validation code, never a path, URL, or interpreter.
- *Alternative:* free-form `kind` with pass-through payload — rejected: would import arbitrary fields and break "strict validation".
- *Alternative:* store the full renderer code — rejected: artifacts are data.

### D4 — Export normalization: strip secrets and instance-local fields; derive `requires`

`ExportArtifact(kind, id)` loads the row, marshals it like `ExportBundle`, runs `stripSecrets`, deletes `id`/timestamps, and scans the payload for references (playlist `items`, layout `regions`, scene `triggers`/`actions`, pixel-art `bindings`) to populate `requires`. Secrets such as pixel-art `api_token` are removed with an export-side `secret_stripped` note in the response.

- *Why:* an artifact that embeds a token must never leave the instance; deterministic normalization means the same artifact produces the same checksum from any instance.
- *Alternative:* export everything and rely on import to strip — rejected: the secret would still live in a file on disk and in transit.

### D5 — Import is validate-then-write, with a dry-run conflict preview

Validation (no DB writes): body ≤ 8 MiB via `io.LimitReader`; zip PK sniff; entry allowlist + traversal check; uncompressed total ≤ 16 MiB and ≤ 32 entries; `manifest.json` and `payload.json` required; manifest `schema == 1`, known `kind`, non-empty bounded `name`/`version`/`created_with`; `payload_sha256` matches the literal `payload.json` bytes; when the caller supplies an expected whole-file `sha256` (catalog install), verify it against the received bytes; per-kind schema validation (D3); `stripSecrets` with a `secret_stripped` warning if anything was removed; `preview.png`, when present, must decode as PNG within 1 MiB and 1024×1024. A reference scan then resolves `requires` and in-payload refs against the DB once per referenced table, following the `ValidateBundleWithDB` pattern (`handlers/backup.go:410`), and returns `ref_missing` warnings.

- *Why:* every check runs before a transaction, so a rejected bundle cannot leave partial state; reusing the field parsers means validation matches what the editors accept.
- *Alternative:* validate inside the transaction — rejected: wasted writes and mixed failure semantics.
- *Risk:* reference resolution against live tables can go stale if a datasource is deleted mid-import — acceptable, refs are warnings by design.

### D6 — Name collisions answer with `409 name_conflict` and an explicit `on_conflict=rename|skip|replace`

Import defaults to returning `409` with `{error:"name_conflict", details:{kind, name, existing_id}}`. `rename` requires a target `name` and creates a new row (re-validating uniqueness for theme/scene); `skip` performs no write and returns `skipped: true`; `replace` updates the existing row in place, preserving its ID and any dependent rows (theme assignments, GeneralSettings edges). A built-in theme collision can only be renamed or skipped — replacement is refused because built-ins are immutable.

- *Why:* one-click catalog installs must not silently clobber a same-named local artifact; making the answer explicit keeps scripts deterministic.
- *Alternative:* auto-suffix rename (`Name (2)`) — rejected: surprising duplicates, especially for uniquely named themes/scenes.
- *Alternative:* always replace — rejected: silent data loss.

### D7 — Missing references degrade to warnings; secrets never persist; nothing executes

Unresolved refs (datasource `source_type`/`source_id`, scene trigger entity IDs, scene/layout/playlist references, pixel-art bindings) are reported as warnings in the import result and left in the payload verbatim so they can resolve later; they do not fail the import. `stripSecrets` is applied on both export and import, so a hand-edited bundle carrying `api_token` imports without the secret and returns `secret_stripped`. No kind reads files, spawns processes, or evaluates expressions; the only external fetches are the catalog/bundle URLs governed by D8.

- *Why:* foreign database IDs are meaningless on the target; a sharing feature that fails because one datasource is absent is unusable. Warnings keep the operator informed without blocking.
- *Alternative:* strip unresolved refs — rejected: destroys information and breaks layouts whose sources are added later.
- *Alternative:* hard-fail on dangling refs — rejected: contradicts the product goal (see `add-config-backup-restore` which tolerates dangling data with a dry-run listing).

### D8 — Remote fetch is SSRF-hardened: allowlist, no redirects, private-range deny by default, caps

`ARTIFACT_CATALOG_URL` (index) and `ARTIFACT_CATALOG_HOSTS` (comma-separated allowlist) mirror `PLUGINS_CATALOG_URL`/`PLUGINS_MANIFEST_HOSTS` naming. Fetch rules for both the index and a bundle URL: http/https only; if the allowlist is set, the hostname must appear in it; otherwise the host is resolved (`net.Resolver.LookupIPAddr`) and every resolved address must be public (reject loopback, private, link-local, unspecified, multicast); `http.Client` with `Timeout: 10s` and `CheckRedirect` returning `http.ErrUseLastResponse` so redirects are never followed; index body capped at 256 KiB (matching `fetchPluginCatalog` `handlers/plugins.go:483`), bundle body at 8 MiB; catalog `sha256`/`size`, when present, verified against the downloaded bytes. Explicitly allowlisted LAN hosts are permitted so homelab catalogs still work.

- *Why:* a catalog is untrusted input; default-deny private ranges closes the cloud-metadata/link-local SSRF class, and refusing redirects closes bounce-based bypasses of a host check. The plugin catalog's default client follows redirects (`handlers/plugins.go:474`), so this deliberately tightens the model the prompt says to be consistent with.
- *Alternative:* reuse `manifestHostAllowed` semantics (allow all when unset) — rejected: a server with no catalog configured should not be usable to probe its own network.
- *Alternative:* block private ranges even when allowlisted — rejected: breaks the common homelab deployment with a NAS-hosted catalog.
- *Risk:* DNS rebinding between the address check and the request (TOCTOU) — documented residual; a follow-up can pin the checked IP in a custom `DialContext`. Catalog preview images are loaded by the browser from `preview_url`, so the server never fetches them.

### D9 — Catalog index + page mirror the plugin catalog, with integrity and preview columns

Index shape: `{"artifacts":[{"kind","name","version","author","description","bundle_url","preview_url","sha256","size","created_with"}]}`. `GET /admin/artifacts/catalog` fetches and renders it server-side into `artifact_catalog.html`, a table with preview thumbnail (or kind badge), name, author, kind, version, description, sha256 (truncated), and an Install form. Install posts the entry to the HTML wrapper `POST /admin/artifacts/import` (flash + redirect, like `AdminPluginInstall` `handlers/plugins.go:430`), which delegates to the same core as the JSON API `POST /admin/api/artifacts/import`; both re-fetch the bundle under D8, verify the index hash, then follow the normal validation/conflict flow.

- *Why:* two thin wrappers over one `importArtifact` core keep behavior identical for one-click and API use; server-rendered HTML matches every other admin page and needs no new frontend framework.
- *Alternative:* client-side fetch of the catalog — rejected: duplicates the SSRF guards in the browser and leaks the admin session origin policy into a second code path.
- *Alternative:* store catalog entries in a table — rejected: no need to cache untrusted index data; fetch per page view (with failure shown, like `plugin_catalog.html`).

### D10 — Errors and results are a stable JSON contract

Errors: `{"error":"<code>","message":"human text","details":{…}}` with codes `unauthorized`, `invalid_bundle`, `invalid_zip`, `invalid_json`, `missing_manifest`, `missing_payload`, `unsupported_version`, `unsupported_kind`, `bundle_too_large`, `invalid_path`, `checksum_mismatch`, `name_conflict`, `validation_failed`, `secret_not_allowed`, `fetch_failed`, `host_not_allowed`, `import_failed`. Success: `{"imported":true,"kind","name","id","replaced":bool,"skipped":bool,"warnings":[{"code","path","message"}]}`. Warnings never carry secrets or prices and `secret_stripped`/`ref_missing` are warnings, not errors.

- *Why:* deep links, the catalog page, and future CI tooling all need stable machine-readable outcomes; the backup import already returns structured results (`ImportResult` `handlers/backup.go:66`), so this is the same idea at artifact granularity.

### D11 — Deep links are a documented convention that prefills, never auto-imports

`ledit://import?url=<encoded bundle url>&sha256=<hex>` is documented as mapping to `https://<ledit-host>/admin/artifacts?url=…&sha256=…` (the OS/browser resolves the scheme; the server registers nothing). The page prefills the URL field and requires an explicit Import click. Fetch-on-load is forbidden so an untrusted link cannot trigger a server-side request drive-by.

- *Why:* deep links are attacker-controllable; requiring a click keeps the admin in the loop and preserves CSRF posture.
- *Alternative:* server-side `/admin/artifacts/ledit?url=…` auto-redirect — rejected: same reason plus redirect ambiguity.

### D12 — Previews are optional and cheap; import never requires them

Export includes `preview.png` where a renderer already exists cheaply — pixel art (first frame encoded with `image/png` as in the import pipeline, `handlers/media_import.go:565`) and themes (a small color swatch). Other kinds omit previews in v1; the format and UI treat them as optional, and the catalog displays `preview_url` when the index supplies one and a kind badge otherwise. Preview caps: ≤1 MiB, ≤1024×1024, PNG only.

- *Why:* previews materially help discovery but must not block exports or bloat bundles; tying them to existing encoders keeps the implementation bounded.
- *Alternative:* always render previews through the live-feed pipeline (`handlers/preview.go`) — deferred: heavier and per-device dependent.

## Risks / Trade-offs

- [Malicious bundle zip] Zip bomb / traversal / oversized payload → capped entries, total and per-entry sizes (`maxZipEntries`-style, `filepath.Clean` allowlist from `BackupImportHandler`), strict required-entry list; only `manifest.json`/`payload.json`/`preview.png` are ever read.
- [SSRF via catalog or `bundle_url`] → allowlist + default-deny private ranges + redirect refusal + timeouts + caps (D8); residual DNS-rebinding TOCTOU documented, mitigable later with IP-pinned dialing.
- [Secret leakage] → `stripSecrets` on export and import; `api_token` never persisted from a bundle; warning when stripped; bundles are never echoed back in responses.
- [Silent clobbering on install] → default `409 name_conflict` with explicit rename/skip/replace; built-in themes can never be replaced.
- [Reference rot after import] → refs preserved verbatim and surfaced as `ref_missing` warnings; renderers already tolerate unresolved sources.
- [Forward compatibility] → closed registry rejects unknown kinds/versions with stable codes; unknown payload *fields* are dropped with `unknown_field` warnings so a newer minor exporter still imports into an older instance; `schema` major bumps gate incompatible envelope changes.
- [Catalog index staleness/trust] → the index is untrusted presentation data; install re-fetches and verifies `sha256` from the index before import; a bad index can mislabel metadata but cannot bypass validation.
- [Browser fetching remote previews] → previews are loaded directly by the admin browser from `preview_url` (no server-side fetch); acceptable for v1, and a proxy is a possible follow-up if tracking/privacy becomes a concern.
- [Unique-name kinds] → theme/scene imports re-validate uniqueness inside the transaction; a racing create fails the import cleanly with `import_failed` and nothing is committed.

## Migration Plan

1. No database migration: no new tables or columns; the feature is handlers, templates, and one TS entry.
2. Add `handlers/artifacts.go` + tests; wire routes in `handlers/server.go` next to the backup block (`handlers/server.go:975`) and add the sidebar entry.
3. Add `web/templates/admin/artifacts.html`, `artifact_catalog.html`, `web/frontend/artifacts.ts`; build with the existing frontend task so `web/static/assets/artifacts.js` is emitted.
4. Ship `ARTIFACT_CATALOG_URL`/`ARTIFACT_CATALOG_HOSTS` as optional; unset means the catalog page explains how to configure it (same UX as `plugin_catalog.html`).
5. Rollback: remove the routes/templates; exported bundles remain valid files and any imported artifacts remain ordinary rows. No schema or data cleanup needed.

## Open Questions

- Should `effect-preset` ship as an alias for an existing entity (for example a Scene action bundle) in a follow-up, or wait for a dedicated schema? Assumed: wait; the registry slot and clean `unsupported_kind` error are enough for forward compatibility.
- Should the catalog page cache entries briefly to avoid a fetch per view, and should it show a stale indicator? Assumed: no caching in v1; failure is rendered like `plugin_catalog.html`.
- Should replace-on-conflict also merge theme assignments, or only preserve the existing row's assignments? Assumed: preserve the existing row's assignments and leave the imported theme unassigned.
- Should bulk export exist as a multi-bundle zip? Assumed: deferred; `config-backup-restore` covers whole-instance moves and the single-bundle format stays canonical.
- Should `ledit://` also support `ledit://catalog?url=…` to set a catalog on the import page? Assumed: defer; only `ledit://import` is documented in v1.
