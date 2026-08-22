## Context

LEDit was originally a Python Django application (in `server-py/`) that served an LED matrix display with datasources like Sonarr, Radarr, F1, Weather, HomeAssistant, Untappd, Images, and Videos. The application was ported to Go using gin (HTTP router), ent (ORM from schema), and gorilla/websocket (real-time feed). The Python code is entirely orphaned — it is not referenced by the Dockerfile, docker-compose.yml, Taskfile.yml, or any Go source. Additionally, `middleware/` and `models/` directories in the Go project are empty, and `client/` contains only a trivial `__init__.py` version file.

## Goals / Non-Goals

**Goals:**
- Confirm all Python Django features have a Go equivalent
- Remove the entire `server-py/` directory (Django app, migrations, static assets, templates, themes, submodels, media)
- Remove orphaned `client/ledit_client/` package
- Remove empty `middleware/` and `models/` Go directories
- Verify Go build/tests pass after cleanup
- Commit and push the cleaned repository

**Non-Goals:**
- No functional changes to the Go application
- No database schema migrations
- No refactoring of Go code structure
- No removal of shared assets like fonts (Go uses its own `fonts/` dir)
- No changes to the frontend (TypeScript/Playwright tests stay)

## Decisions

1. **Full directory removal vs individual file audit**: The Python code is entirely self-contained in `server-py/`. Instead of auditing file-by-file, remove the entire directory after confirming Go equivalents exist for each feature. This is safer (no missed files) and cleaner (atomic removal).

2. **Keep shared font files**: The Python and Go projects each have their own font directories. The Go `fonts/` directory is actively used and referenced. The Python `server-py/fonts/` will be removed with the rest of `server-py/`. No font files are shared across boundaries.

3. **Keep `client/` but clean empty directories**: The `client/` directory contains only a version file. Remove it since it's unused and has no references in the Go codebase. The `middleware/` and `models/` Go directories are empty — remove them as dead scaffold.

4. **Verify with build and tests**: After removal, run `go build ./...` and `go test ./...` to confirm no breakage. The build currently passes; no Python code is imported by Go.

## Risks / Trade-offs

- **[Low Risk] Missed Python reference**: If any config/doc references Python, the stale reference may remain. Mitigation: search all non-Python files for Django/`server-py`/Python references before and after removal.
- **[Low Risk] Font paths in data**: Fonts stored in DB or config might reference old paths. Mitigation: `fonts/` directory is project-level, not Python-specific. Python fonts are separate and unused.
- **[Low Risk] Accidental removal of desired media**: Python `server-py/media/` contains custom images and videos. These were uploaded via Django admin. Mitigation: verify none of these assets are referenced by the Go app's data layer (they are not — Go stores its own media paths).
