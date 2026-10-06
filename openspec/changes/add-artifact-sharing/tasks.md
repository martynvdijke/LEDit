## 1. Bundle Format & Validation Core

- [ ] 1.1 Add `handlers/artifacts.go` with the bundle types: `ArtifactManifest` (`schema`, `kind`, `name`, `version`, `author`, `description`, `created_with`, `requires`, `payload_sha256`), `ArtifactPayload` (`kind`, `entity` map), `ArtifactImportResult` (imported/kind/name/id/replaced/skipped/warnings), and the stable error-code constants from design D10
- [ ] 1.2 Implement deterministic payload serialization: marshal the Ent row to a map exactly like `ExportBundle` (`handlers/backup.go:249`), run `stripSecrets` (`handlers/backup.go:93`), delete `id`/`created_at`/`updated_at`, and expose `canonicalPayloadBytes` + `payloadSHA256` helpers (remember `encoding/json` sorts map keys)
- [ ] 1.3 Implement the zip container writer/reader with the fixed entry allowlist (`manifest.json`, `payload.json`, optional `preview.png`), PK sniff, `filepath.Clean` traversal refusal, ≤32 entries, ≤16 MiB uncompressed, ≤8 MiB body, and preview PNG decode cap (≤1 MiB, ≤1024×1024), mirroring the checks in `BackupImportHandler` (`handlers/backup.go:1210`)
- [ ] 1.4 Add the kind registry mapping `theme`/`layout`/`playlist`/`scene`/`text-slide`/`pixel-art` to export/validate/apply, plus the reserved `effect-preset` slot; reject `unsupported_version`, `unsupported_kind`, and executable-looking fields (`exec`, `script`, `command`, `entrypoint`, `hook`) with `validation_failed`
- [ ] 1.5 Wire per-kind validation to the existing parsers: `datasource.ParsePlaylistItems` + `ParseScheduleWindows`/`ValidateWindows` (`handlers/playlists.go:54`, `handlers/schedule.go:63`), `datasource.ParseRegions` + `validateRegionConfig` (`datasource/compositor.go:56`, `handlers/layout_editor.go:63`), `parseSceneTriggers`/`parseSceneActions` (`handlers/scenes.go:80`), `validPixelBindings` (`handlers/pixelart.go:17`), and theme `hexColorPattern` bounds
- [ ] 1.6 Implement the secret/unknown-field/ref behavior: `stripSecrets` with `secret_stripped` warnings, `unknown_field` drop warnings, and a `ref_missing` scan that resolves payload references against the DB once per referenced table (pattern from `ValidateBundleWithDB`, `handlers/backup.go:483`)
- [ ] 1.7 Add `handlers/artifacts_test.go` unit tests: round-trip marshal/checksum determinism, traversal rejection, entry/size caps, unknown kind/version, unknown-field and secret handling

## 2. Artifact Export

- [ ] 2.1 Implement `ExportArtifact(kind string, id int) (bundle, error)` in `handlers/artifacts.go`: load the row, refuse `built_in` themes, normalize the payload (1.2), derive `requires`, and optionally attach a preview (2.3)
- [ ] 2.2 Add the session-auth endpoint `GET /admin/api/artifacts/export/:kind/:id` returning the zip with `Content-Disposition: attachment; filename="ledit-<kind>-<slug>.zip"`, `401` JSON for unauthenticated calls, and stable errors for unknown kinds/ids
- [ ] 2.3 Add optional preview generation for `theme` (color swatch) and `pixel-art` (first frame via `image/png`, following `handlers/media_import.go:565`); omit previews for other kinds in v1
- [ ] 2.4 Add `GET /admin/artifacts` (`web/templates/admin/artifacts.html`) listing shareable artifacts of every kind (custom themes, layouts from `s.DB.Composition`, playlists, scenes, text slides, pixel arts) with Export actions, and render the import card from group 3
- [ ] 2.5 Add the sidebar link to `/admin/artifacts` in `web/templates/admin/sidebar.html` and optional Export actions/links on `themes.html`, `layouts.html`, `playlists.html`, `scenes.html`, and `pixelarts.html`
- [ ] 2.6 Add handler tests: export each kind round-trips through validation, built-in theme refused, `api_token` stripped, unauthenticated export returns `401`

## 3. Artifact Import

- [ ] 3.1 Implement the pre-write validation pipeline from design D5 (parse/caps/schema/checksum/per-kind validation/preview check) returning a structured result with `errors`, `warnings`, and conflict info; keep every check outside the transaction
- [ ] 3.2 Implement `on_conflict` handling: default `409` `name_conflict` with existing-row details; `rename` (caller-supplied name, uniqueness re-checked for theme/scene), `skip`, and `replace` (update in place, preserving ID and dependents); built-in theme may never be replaced
- [ ] 3.3 Implement the transactional per-kind importer with `s.DB.Tx`: create new rows, replicate the `GeneralSettings` edge attachment done by `AdminPlaylistCreate` (`handlers/playlists.go:100`), `AdminLayoutCreate` (`handlers/layout_editor.go:266`), and `AdminTextSlideCreate` (`handlers/handlers.go:2206`), and roll back completely on any failure (`import_failed`)
- [ ] 3.4 Add the JSON endpoint `POST /admin/api/artifacts/import` accepting multipart file, raw body, or `url`, with `name`, `on_conflict`, `sha256`, and `dry_run` query/form parameters, returning the stable success/error contract from design D10
- [ ] 3.5 Add the HTML wrapper `POST /admin/artifacts/import` (flash + redirect, modeled on `AdminPluginInstall` `handlers/plugins.go:430`) delegating to the same core importer for page and catalog forms
- [ ] 3.6 Record an audit entry via `s.LogStore.Submit` (actor, kind, name, conflict strategy, source) after commit, mirroring the backup audit at `handlers/backup.go:1164`
- [ ] 3.7 Add handler tests: payload tamper → `checksum_mismatch`, traversal → `invalid_path`, caps → `bundle_too_large`, invalid entity → `validation_failed`, full conflict matrix, rollback leaves no row/edge change, imported playlist/layout/text-slide edge attachment, dry-run writes nothing, audit entry written

## 4. Remote Catalog & Fetch Hardening

- [ ] 4.1 Implement `fetchArtifactURL(ctx, rawURL, maxBytes)` in `handlers/artifacts.go`: http/https only, `ARTIFACT_CATALOG_HOSTS` allowlist, public-address check via `net.Resolver` when unset (deny loopback/private/link-local/unspecified/multicast), `http.Client{Timeout: 10s, CheckRedirect: refuse}`, and a limited reader for the body
- [ ] 4.2 Implement `fetchArtifactCatalog(ctx)` parsing `{"artifacts":[…]}` with `kind`, `name`, `version`, `author`, `description`, `bundle_url`, `preview_url`, `sha256`, `size`, `created_with`; cap the index at 256 KiB and surface failures as non-fatal page errors
- [ ] 4.3 Add `GET /admin/artifacts/catalog` rendering `web/templates/admin/artifact_catalog.html` (modeled on `plugin_catalog.html`) with preview thumbnail or kind badge, name, author, kind, version, description, truncated integrity hash, and an Install form; handle unset/error/empty states
- [ ] 4.4 Wire one-click install: post `bundle_url` + `sha256` to the HTML import wrapper, re-fetch under 4.1, verify the index `sha256`/`size`, then run the normal validation/conflict flow; on conflict, route to the artifacts page with the conflict choice
- [ ] 4.5 Add tests using `httptest` servers: unsupported scheme, allowlisted host, private-address denial, redirect refusal (assert the target is never hit), index/body caps, malformed index, catalog hash mismatch, install success and conflict

## 5. Deep Links, API Contract & UI Integration

- [ ] 5.1 Centralize the stable JSON error helper and codes from design D10 and assert in tests that messages never contain secrets or absolute filesystem paths
- [ ] 5.2 Make the artifacts page read `url`, `sha256`, `name`, and `on_conflict` query parameters to prefill the import form, and ensure it performs no fetch until the administrator confirms
- [ ] 5.3 Document the `ledit://import?url=…&sha256=…` convention (page help plus README/docs section) as a mapping the OS/browser resolves to the admin import URL, and note that no OS handler is registered by the server
- [ ] 5.4 Add `web/frontend/artifacts.ts` for the dry-run preview, conflict radio (rename/skip/replace), import result/warnings, and catalog install UX; ensure `npm run build` (Taskfile `assets`) emits `web/static/assets/artifacts.js`
- [ ] 5.5 Add handler/UI tests: query-param prefill without server fetch, unauthenticated endpoints return `401 {"error":"unauthorized"}`, dry-run response shape, success response shape with warnings

## 6. Docs & Verification

- [ ] 6.1 Document the feature: `README.md`/docs for bundle layout, manifest fields, catalog index schema, `ARTIFACT_CATALOG_URL` and `ARTIFACT_CATALOG_HOSTS`, integrity hashes, and degradation rules (no secrets, refs warn)
- [ ] 6.2 Add Playwright coverage in `tests/artifacts.spec.ts`: export download from the artifacts page, import round-trip into a seeded instance, name-conflict rename/skip/replace flow, catalog page with a routed/stub index and install
- [ ] 6.3 Run `task pre-push` (gofmt, Go tests, Python tests, build) and `task test:e2e`; fix failures
- [ ] 6.4 Run `openspec validate add-artifact-sharing --strict` and confirm it passes
