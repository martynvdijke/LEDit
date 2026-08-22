## Why

LEDit was originally built as a Python Django application. It has since been fully ported to Go, but the old Python codebase (`server-py/`) remains in the repository. This creates confusion, increases maintenance surface area, and risks accidental use of stale code. Additionally, several directories (`middleware/`, `models/`, `client/`) are effectively empty or orphaned.

## What Changes

- **Audit** the Go port for any missing functionality compared to the Python Django version
- **Verify** all Python-originated features are covered in the Go implementation
- **Remove** the entire `server-py/` directory (Django project with models, views, consumers, migrations, settings, static files, templates, themes, submodels)
- **Remove** the orphaned `client/ledit_client/` Python client package
- **Remove** empty/unused Go directories (`middleware/`, `models/`) that were scaffolded but never populated
- **Remove** any Python-stage orphaned media assets (images, videos) in `server-py/` that are not referenced by Go
- **Remove** `server-py/` references from project documentation if any exist
- **Commit and push** all changes

## Capabilities

### New Capabilities
- `go-port-audit`: Systematic audit comparing Python Django features vs Go implementation
- `python-cleanup`: Safe removal of all orphaned Python Django code and assets

### Modified Capabilities
<!-- No existing spec-level changes -->

## Impact

- **Code removed**: ~30 Python files (models, views, consumers, settings, migrations, submodels, themes, tests) plus Django static assets (admin CSS/JS, debug toolbar assets), media files, and configuration
- **Empty dirs cleaned**: `middleware/`, `models/`, `client/`
- **No runtime impact**: Go binary does not depend on any Python code
- **No data impact**: All data is managed through Go + ent ORM + SQLite
