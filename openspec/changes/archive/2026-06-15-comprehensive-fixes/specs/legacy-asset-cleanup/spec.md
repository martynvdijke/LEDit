## ADDED Requirements

### Requirement: Remove Django admin static assets

All unused Django admin static files SHALL be removed from the project.

#### Scenario: Admin CSS files removed

- **WHEN** listing files in `web/static/admin/css/`
- **THEN** ALL files in this directory SHALL be removed (they are unused Django assets)

#### Scenario: Admin JS files removed

- **WHEN** listing files in `web/static/admin/js/`
- **THEN** ALL files in this directory SHALL be removed

#### Scenario: Admin image files removed

- **WHEN** listing files in `web/static/admin/img/`
- **THEN** ALL files in this directory SHALL be removed

### Requirement: Remove debug toolbar static assets

All unused Django debug toolbar static files SHALL be removed from the project.

#### Scenario: Debug toolbar CSS removed

- **WHEN** listing files in `web/static/debug_toolbar/css/`
- **THEN** ALL files in this directory SHALL be removed

#### Scenario: Debug toolbar JS removed

- **WHEN** listing files in `web/static/debug_toolbar/js/`
- **THEN** ALL files in this directory SHALL be removed

### Requirement: No references to removed assets

After cleanup, the codebase SHALL contain no remaining references to the removed static asset paths.

#### Scenario: No import references

- **WHEN** searching the codebase for references to `admin/css/base.css`, `admin/js/core.js`, or other removed asset paths
- **THEN** the search SHALL return no results
