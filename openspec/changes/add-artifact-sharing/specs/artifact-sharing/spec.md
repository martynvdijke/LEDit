# Spec: artifact-sharing

## ADDED Requirements

### Requirement: Versioned single-artifact bundle

The system SHALL define one portable bundle format for a single artifact. A bundle SHALL be a zip archive whose entry names are limited to `manifest.json`, `payload.json`, and optional `preview.png`. The manifest SHALL contain `schema` (bundle format version, starting at `1`), `kind`, `name`, `version`, `author`, `description`, `created_with` (the LEDit version that produced it), `requires` (a list of typed references), and `payload_sha256` (hex SHA-256 of the exact `payload.json` bytes). `payload.json` SHALL be a JSON object `{"kind": <kind>, "entity": {…}}` whose `entity` uses the same field names and row-to-map serialization as the config backup pipeline (`handlers/backup.go`), with instance-local fields (`id`, `created_at`, `updated_at`) omitted, so payload bytes are deterministic for a given artifact. The supported kinds SHALL be a closed registry: `theme`, `layout`, `playlist`, `scene`, `text-slide`, `pixel-art`, plus `effect-preset` reserved for a future change. A bundle whose `schema` is newer than the running instance SHALL be rejected with `unsupported_version`, and a bundle whose `kind` is not implemented by the running instance SHALL be rejected with `unsupported_kind`; neither SHALL write anything.

#### Scenario: Export produces a valid bundle

- **WHEN** an administrator exports a custom theme named "Midnight"
- **THEN** the download SHALL be a zip containing `manifest.json` with `schema: 1`, `kind: "theme"`, `name: "Midnight"`, `created_with`, `requires`, and `payload_sha256`, plus `payload.json` whose `entity` contains the theme tokens without an `id`

#### Scenario: Payload checksum is deterministic

- **WHEN** the same artifact is exported twice from the same instance
- **THEN** the `payload.json` bytes SHALL be identical and `payload_sha256` SHALL match the SHA-256 of those bytes

#### Scenario: Preview is optional

- **WHEN** a bundle without `preview.png` is imported
- **THEN** the import SHALL proceed and preview SHALL NOT be required

#### Scenario: Unknown kind is rejected cleanly

- **WHEN** a bundle declares `kind: "effect-preset"` on an instance that has no effect-preset importer
- **THEN** the import SHALL fail with error code `unsupported_kind` and SHALL write nothing

#### Scenario: Newer bundle schema is rejected cleanly

- **WHEN** a bundle declares `schema: 2`
- **THEN** the import SHALL fail with error code `unsupported_version` and SHALL write nothing

### Requirement: Admin artifact export

The system SHALL provide an admin page `GET /admin/artifacts` listing shareable artifacts (custom themes, layouts, playlists, scenes, text slides, pixel arts) with a per-artifact export action, and a session-authenticated endpoint `GET /admin/api/artifacts/export/:kind/:id` returning the bundle as a file download. Export SHALL strip secret fields using the backup `stripSecrets` rules, SHALL omit device/instance tokens, and SHALL NOT export or replace built-in themes. `requires` SHALL be derived from the artifact's references so consumers can see what the artifact expects. A request without a valid admin session SHALL be rejected with `401`.

#### Scenario: Custom theme exports from the artifacts page

- **WHEN** an administrator clicks Export for a custom theme on `/admin/artifacts`
- **THEN** the browser SHALL download a bundle whose manifest kind is `theme` and whose payload contains the theme tokens

#### Scenario: Built-in theme is not exportable

- **WHEN** an administrator attempts to export a theme with `built_in: true`
- **THEN** the export SHALL be refused with a clear message and SHALL NOT produce a bundle

#### Scenario: Secrets are stripped on export

- **WHEN** a pixel art with a configured `api_token` is exported
- **THEN** the payload SHALL NOT contain the token and the export result SHALL report that a secret was stripped

#### Scenario: Unauthenticated export is rejected

- **WHEN** an unauthenticated request calls `GET /admin/api/artifacts/export/:kind/:id`
- **THEN** the system SHALL respond `401` with the stable JSON error body

#### Scenario: Every supported kind can be exported

- **WHEN** an administrator exports a layout, playlist, scene, and text slide that exist
- **THEN** each download SHALL carry the matching kind manifest and a payload that re-imports into another instance

### Requirement: Strict import validation before write

The import endpoint SHALL validate a bundle completely before any database write: request body ≤ 8 MiB (streamed through a limited reader), zip PK sniff, entry names restricted to the bundle allowlist with path traversal refused, at most 32 entries and 16 MiB total uncompressed, both `manifest.json` and `payload.json` present, manifest `schema` and `kind` checks, `payload_sha256` equal to the SHA-256 of the received `payload.json` bytes, optional whole-file `sha256` supplied by a catalog or caller matching the received bundle bytes, per-kind schema and value validation reusing the existing entity parsers (playlist items/schedule windows, layout regions, scene triggers/actions, pixel-art bindings/frames, theme colors/font bounds), and, when `preview.png` is present, a valid PNG no larger than 1 MiB and 1024×1024. On any failure the system SHALL return a stable error code and SHALL NOT create or modify any artifact. When `dry_run=true`, the endpoint SHALL return validation results, conflicts, and reference warnings without writing.

#### Scenario: Tampered payload is rejected

- **WHEN** a bundle's `payload.json` is edited after export so its bytes no longer match `payload_sha256`
- **THEN** the import SHALL fail with `checksum_mismatch` and no artifact SHALL be created

#### Scenario: Path traversal is rejected

- **WHEN** a bundle contains an entry named `../../etc/passwd`
- **THEN** the import SHALL fail with `invalid_path` and no artifact SHALL be created

#### Scenario: Oversized bundle is rejected

- **WHEN** a bundle exceeds the entry count, total uncompressed size, or body size caps
- **THEN** the import SHALL fail with `bundle_too_large` and no artifact SHALL be created

#### Scenario: Invalid entity data is rejected

- **WHEN** a playlist payload contains malformed `items` JSON or an invalid `schedule_windows` entry
- **THEN** the import SHALL fail with `validation_failed` and no playlist SHALL be created

#### Scenario: Dry run reports without writing

- **WHEN** an administrator submits a valid bundle with `dry_run=true`
- **THEN** the response SHALL include the would-be action, any `name_conflict`, and `ref_missing` warnings, and the database SHALL be unchanged

### Requirement: Name collision handling

When an imported artifact's name matches an existing artifact of the same kind, the import SHALL default to returning HTTP `409` with error code `name_conflict` and details identifying the existing row. The caller SHALL be able to resolve the collision with `on_conflict=rename`, `skip`, or `replace`: `rename` creates a new artifact under a caller-supplied name (re-validating uniqueness where the kind requires it), `skip` writes nothing and reports `skipped`, and `replace` updates the existing row in place while preserving its ID and dependent rows (for example theme assignments). A built-in theme collision SHALL only be resolvable by `rename` or `skip`; replacement SHALL be refused.

#### Scenario: Default collision answer

- **WHEN** a bundle named "Midnight" is imported into an instance that already has a theme named "Midnight" and no `on_conflict` is supplied
- **THEN** the system SHALL respond `409` with `name_conflict` and SHALL NOT modify the existing theme

#### Scenario: Rename creates a new artifact

- **WHEN** the same import is retried with `on_conflict=rename` and `name=Midnight 2`
- **THEN** a new theme named "Midnight 2" SHALL be created and the existing "Midnight" SHALL be unchanged

#### Scenario: Skip writes nothing

- **WHEN** the same import is retried with `on_conflict=skip`
- **THEN** the response SHALL report `skipped` and the database SHALL be unchanged

#### Scenario: Replace preserves dependents

- **WHEN** the same import is retried with `on_conflict=replace` and the existing theme is assigned to devices
- **THEN** the existing row SHALL be updated with the payload's tokens, its ID SHALL be unchanged, and its assignments SHALL remain

#### Scenario: Built-in theme cannot be replaced

- **WHEN** a bundle's name collides with a built-in theme and `on_conflict=replace` is requested
- **THEN** the import SHALL be refused and the built-in theme SHALL be unchanged

### Requirement: Transactional and audited imports

A successful import SHALL be written in a single database transaction that also performs the same `GeneralSettings` edge attachment the corresponding admin create does for playlists, layouts, and text slides. If any step fails after validation, the transaction SHALL roll back so that no partial artifact (or edge change) is persisted, and the response SHALL use error code `import_failed`. After a successful commit the system SHALL record an audit entry identifying the actor, kind, name, conflict strategy, and source (upload, URL, or catalog), consistent with the backup export/import audit (`handlers/backup.go`).

#### Scenario: Failure rolls back completely

- **WHEN** an import fails while creating the artifact row
- **THEN** no artifact and no `GeneralSettings` edge change SHALL remain in the database

#### Scenario: Imported artifact behaves like a created one

- **WHEN** a playlist is imported successfully
- **THEN** it SHALL appear in `/admin/playlists`, resolve in the feed like a locally created playlist, and be attached to `GeneralSettings` as `AdminPlaylistCreate` does

#### Scenario: Import is audit-logged

- **WHEN** an import completes
- **THEN** an audit log entry SHALL record actor, kind, name, conflict strategy, and source

### Requirement: Declarative-only imports with graceful degradation

Artifacts SHALL be data only. No kind SHALL execute code, read or write files outside the database, spawn processes, or evaluate expressions; payload fields naming executable behavior (`exec`, `script`, `command`, `entrypoint`, `hook`) SHALL be rejected with `validation_failed`. Secret fields SHALL never be persisted from a bundle: they SHALL be stripped using the backup `stripSecrets` rules and reported as `secret_stripped` warnings. References in a payload (datasource `source_type`/`source_id`, scene trigger entity IDs, playlist/layout/scene references, pixel-art bindings) that do not resolve on the target instance SHALL NOT fail the import; they SHALL be preserved verbatim and reported as `ref_missing` warnings.

#### Scenario: Secret material in a bundle is never persisted

- **WHEN** a hand-edited pixel-art bundle contains a non-empty `api_token`
- **THEN** the imported artifact SHALL NOT store the token and the response SHALL include a `secret_stripped` warning

#### Scenario: Missing datasource reference degrades to a warning

- **WHEN** a playlist payload references `{"source_type":"weather","source_id":42}` and the target instance has no such weather source
- **THEN** the playlist SHALL import successfully, the reference SHALL remain in `items`, and the response SHALL include a `ref_missing` warning

#### Scenario: Executable fields are rejected

- **WHEN** a payload contains a field such as `script`, `exec`, or `hook`
- **THEN** the import SHALL fail with `validation_failed` and nothing SHALL be written

#### Scenario: Unknown payload fields degrade forward-compatibly

- **WHEN** a payload carries a field not recognized by the running instance's kind schema
- **THEN** the field SHALL be dropped with an `unknown_field` warning and the rest of the artifact SHALL import

### Requirement: Remote catalog fetch hardening

The system SHALL fetch a remote catalog index from the URL configured in `ARTIFACT_CATALOG_URL` and SHALL apply the same policy to every bundle URL it fetches (catalog installs and URL imports). Fetches SHALL allow only `http` and `https` schemes; when `ARTIFACT_CATALOG_HOSTS` is set, the host SHALL be in that comma-separated allowlist; when it is not set, the host SHALL resolve only to public addresses, with loopback, private, link-local, unspecified, and multicast addresses denied. Redirects SHALL NOT be followed, the client SHALL time out at 10 seconds, the index body SHALL be capped at 256 KiB, and a bundle body SHALL be capped at 8 MiB. When a catalog entry supplies `sha256` or `size`, the downloaded bundle SHALL be verified against them before validation proceeds.

#### Scenario: Configured catalog is fetched and listed

- **WHEN** `ARTIFACT_CATALOG_URL` is set and returns a valid index
- **THEN** the catalog page SHALL list the entries and the server SHALL have applied the scheme, allowlist, redirect, timeout, and size policy

#### Scenario: Redirects are refused

- **WHEN** a catalog or bundle URL responds with a redirect
- **THEN** the fetch SHALL fail with `fetch_failed` and the redirect target SHALL NOT be contacted

#### Scenario: Private address is denied unless allowlisted

- **WHEN** no `ARTIFACT_CATALOG_HOSTS` is configured and a URL resolves to a loopback or private address
- **THEN** the fetch SHALL fail with `host_not_allowed` and no request SHALL be sent to that address

#### Scenario: Explicit allowlist permits a LAN catalog

- **WHEN** `ARTIFACT_CATALOG_HOSTS` includes the hostname of a LAN catalog
- **THEN** the fetch SHALL be permitted to that host only

#### Scenario: Catalog integrity hash is enforced

- **WHEN** a catalog entry declares a `sha256` that does not match the downloaded bundle
- **THEN** the install SHALL fail with `checksum_mismatch` and SHALL NOT import or persist anything

### Requirement: Catalog discovery and one-click install

The system SHALL provide an admin page `GET /admin/artifacts/catalog` that renders the remote index entries with at minimum the artifact kind, name, version, author, description, an integrity hash, and a preview image when the entry supplies a `preview_url`, with a one-click Install action. Installation SHALL run the same validation, conflict, transaction, and audit path as file import, SHALL install under a caller-chosen name where supplied, and SHALL be admin-session authenticated. A missing `ARTIFACT_CATALOG_URL` SHALL render guidance instead of an error, and a malformed or unreachable index SHALL render a non-fatal error message.

#### Scenario: Catalog page shows metadata

- **WHEN** the configured index contains an entry with kind, name, version, author, description, preview URL, and sha256
- **THEN** the page SHALL display all of those fields and an Install control

#### Scenario: One-click install succeeds

- **WHEN** an administrator clicks Install for a valid entry and no name collision exists
- **THEN** the bundle SHALL be fetched, verified, validated, imported, and the page SHALL report success with a link to the artifact

#### Scenario: Install collision defers to the conflict flow

- **WHEN** an administrator installs a catalog entry whose name already exists locally
- **THEN** the system SHALL present the rename/skip/replace choice and SHALL NOT write until one is chosen

#### Scenario: Unconfigured catalog renders guidance

- **WHEN** `ARTIFACT_CATALOG_URL` is not set
- **THEN** the catalog page SHALL explain how to configure it and SHALL NOT error

#### Scenario: Broken catalog degrades safely

- **WHEN** the index URL is unreachable or returns malformed JSON
- **THEN** the page SHALL show an error message and the rest of the admin SHALL remain functional

### Requirement: Stable API and deep-link convention

The system SHALL expose `POST /admin/api/artifacts/import` accepting a bundle as a multipart file, raw request body, or `url` parameter plus optional `name`, `on_conflict`, `sha256`, and `dry_run`, and SHALL return a stable JSON contract: errors as `{"error": "<code>", "message": "...", "details": {...}}` using the documented codes (`unauthorized`, `invalid_bundle`, `invalid_zip`, `invalid_json`, `missing_manifest`, `missing_payload`, `unsupported_version`, `unsupported_kind`, `bundle_too_large`, `invalid_path`, `checksum_mismatch`, `name_conflict`, `validation_failed`, `fetch_failed`, `host_not_allowed`, `import_failed`), and successes as `{"imported": true, "kind", "name", "id", "replaced", "skipped", "warnings": [...]}`. The system SHALL document a `ledit://import?url=<bundle-url>&sha256=<hex>` deep-link convention that maps to the admin import page with those values prefilled; the page SHALL fetch nothing until the administrator explicitly confirms, and the server SHALL NOT register an operating-system protocol handler. All import/export/catalog endpoints SHALL require a valid admin session.

#### Scenario: API import via uploaded file

- **WHEN** an authenticated admin posts a bundle file to `POST /admin/api/artifacts/import`
- **THEN** the response SHALL be the success contract with `imported: true` and any warnings

#### Scenario: API import via URL

- **WHEN** an authenticated admin posts `url=<https bundle>` to the import endpoint
- **THEN** the server SHALL fetch the bundle under the catalog fetch policy and import it, or return a stable fetch error code

#### Scenario: Stable error body

- **WHEN** an import fails validation
- **THEN** the response SHALL contain a machine-readable `error` code and a human-readable `message` without leaking secrets or absolute server paths

#### Scenario: Deep link prefills without fetching

- **WHEN** an administrator opens the import page with `?url=…&sha256=…` (for example via a documented `ledit://import` mapping)
- **THEN** the form SHALL be prefilled with those values and SHALL NOT contact the URL until the administrator confirms the import

#### Scenario: Unauthenticated API is rejected

- **WHEN** an unauthenticated caller posts to the import API
- **THEN** the system SHALL return `401` with `{"error": "unauthorized"}`

### Requirement: Shared-artifact UI integration

The admin SHALL surface artifact sharing without disturbing existing editors: a sidebar entry to `/admin/artifacts` and a catalog entry for `/admin/artifacts/catalog`, and the existing artifact list pages (themes, layouts, playlists, scenes, pixel arts) SHALL gain an Export action or link that targets the same bundle export endpoint. The artifacts page SHALL combine the shareable-artifact list, the import form (file picker or URL with a dry-run preview and conflict controls), and the catalog link.

#### Scenario: Artifacts page lists shareable kinds

- **WHEN** an administrator opens `/admin/artifacts`
- **THEN** the page SHALL list custom themes, layouts, playlists, scenes, text slides, and pixel arts with Export actions and an import form

#### Scenario: Existing editors still work

- **WHEN** an administrator uses the theme editor, layout editor, playlist form, scene form, or pixel-art editor after this change
- **THEN** their existing create/edit/delete behavior SHALL be unchanged

#### Scenario: Sidebar discovers sharing

- **WHEN** an administrator views the admin sidebar
- **THEN** a link to Artifacts and its catalog SHALL be present alongside the existing artifact sections
