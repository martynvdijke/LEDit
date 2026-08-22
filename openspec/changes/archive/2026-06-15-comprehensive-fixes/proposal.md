## Why

LEDit has accumulated significant technical debt, security gaps, and usability issues across its codebase. A comprehensive audit revealed: no auth hardening (hardcoded credentials, disabled by default), ~500 lines of repetitive CRUD handlers for 8 datasource types, unused Django admin static assets (~40 files), missing theme editor save implementation, in-memory-only notification storage that loses data on restart, duplicate sidebar code maintained in 3 places, no responsive/mobile layout, no form feedback for users, and incomplete dashboard coverage. These issues compound maintenance cost, block feature development, and degrade the operator experience. Fixing them now establishes a solid foundation for future work.

## What Changes

- **Remove legacy Django admin assets** — Delete unused `web/static/admin/` CSS/JS/images and `web/static/debug_toolbar/` assets (~40 files) that are relics of a past Django migration
- **Harden authentication** — Replace hardcoded `admin`/`ledit` credentials with bcrypt-hashed, configurable credentials stored in DB settings; add env-var-based initial setup; enable auth by default for all deployments
- **Consolidate datasource CRUD handlers** — Replace 40 nearly identical handler functions (~500 lines) with a generic, reflection-driven or registry-based pattern; eliminate giant switch statements in `addEdge`, `createTokenURLDS`, `editTokenURLDS`, `updateTokenURLDS`, `deleteTokenURLDS`
- **Consolidate sidebar** — Make `base.html` and `index.html` use the existing `sidebar.html` template partial; add active page highlighting; make sidebar collapsible on mobile
- **Fix theme editor** — Implement actual persistence in `AdminThemeSave` handler (currently redirects without saving)
- **Persist notifications** — Migrate notification history from in-memory slice to a new `Notification` Ent schema backed by SQLite
- **Add form feedback** — Implement flash message system for redirects after form submissions (success/error toasts)
- **Complete dashboard stat coverage** — Add missing stat cards for RSS, Calendar, Stock, and Text Slides datasource types
- **Add input validation** — Validate all form inputs (required fields, URL formats, number ranges) across all handlers
- **Fix schedule naming** — Rename `cron` field to `time_range` in schedule form to match its actual behavior
- **Harden WebSocket security** — Replace `CheckOrigin: return true` with configurable origin checking
- **Fix file upload safety** — Generate unique filenames for uploaded images/videos to prevent collisions
- **Fix error handling gaps** — Surface errors from DB operations in `updateTokenURLDS`, `deleteTokenURLDS`, and other silent-failure paths

## Capabilities

### New Capabilities

- `auth-hardening`: Configurable admin credentials with bcrypt hashing, session management improvements, env-var-based initial password setup, auth enabled by default
- `crud-consolidation`: Generic datasource CRUD handler infrastructure eliminating per-type repetition
- `sidebar-consolidation`: Unified sidebar template partial used across all pages with active state and responsive collapse
- `theme-editor-persistence`: Functional custom theme editor that actually saves color/font preferences to the database
- `notification-persistence`: Database-backed notification storage replacing in-memory-only history
- `form-feedback`: Flash message system showing success/error toast notifications after admin form submissions
- `dashboard-completeness`: Full coverage of all 14 datasource types in dashboard stat cards and source table
- `input-validation`: Server-side validation for all form inputs with user-facing error messages
- `schedule-naming-clarity`: Renamed form fields and data model to accurately reflect time-range behavior
- `security-hardening`: WebSocket origin validation, unique file upload names, proper error propagation
- `legacy-asset-cleanup`: Removal of unused Django admin and debug toolbar static assets

### Modified Capabilities

- *(none — no existing specs to modify)*

## Impact

- **Removed code**: ~40 unused Django/debug toolbar static files deleted from `web/static/admin/` and `web/static/debug_toolbar/`
- **New Go dependencies**: `golang.org/x/crypto` (bcrypt) for password hashing; `github.com/google/uuid` or crypto/rand for unique filenames
- **New Ent schemas**: `Notification` table for persisted notifications; migration of `GeneralSettings` to include admin credentials (hashed)
- **Refactored code**: `handlers/handlers.go` — ~500 lines of repetitive switch-case CRUD → ~100 lines of generic handler logic; `handlers/auth.go` — session management enhanced
- **New handlers**: Flash message middleware; maybe a `AdminThemeSave` implementation
- **New templates**: Potentially a toast/notification partial for flash messages
- **Modified handlers**: All datasource form handlers gain validation; `AdminThemeSave` becomes functional
- **CheckOrigin change**: `websocket.go` — `CheckOrigin` changed from permissive `return true` to verified origin matching against configured device URLs
- **Naming change**: Schedule field `cron` renamed to `time_range` in schema, template, and handlers (requires DB migration)
