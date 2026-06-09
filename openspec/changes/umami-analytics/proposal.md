## Why

The project has in-app analytics (display counts, uptime) but no external web analytics. Adding Umami — a privacy-focused, self-hosted analytics platform — provides insight into real user traffic, page views, and visitor behavior without sending data to third parties. A new admin setting lets operators configure the connection to their self-hosted Umami instance.

## What Changes

- New Ent schema and DB table for `UmamiSettings` (endpoint URL, website ID, optional shared/tracking script URL)
- New admin settings page at `/admin/settings/umami` to configure Umami connection
- New handler endpoints for Umami settings CRUD (GET form, POST save)
- Umami tracking script injected into `base.html` and `index.html` when enabled
- Sidebar navigation updated with link to Umami settings
- The existing in-app analytics page is untouched (external Umami supplements it)

## Capabilities

### New Capabilities
- `umami-settings`: Admin CRUD for self-hosted Umami analytics configuration (endpoint, website ID, enable toggle)
- `umami-tracking`: Conditional injection of Umami tracking script into web page templates based on settings

### Modified Capabilities
- *(none — no existing spec is changing)*

## Impact

- **Schema**: New `UmamiSettings` Ent schema with fields: `endpoint` (string), `website_id` (string), `enable` (bool). Edge added to `GeneralSettings`.
- **Backend**: New handler functions for settings form and save. Conditional script injection in templates.
- **Frontend**: New HTML template for Umami settings form. Umami script tag conditionally rendered in `base.html` and `index.html`.
- **Database**: New migration for `umami_settings` table (auto-handled by Ent).
- **Config**: No new environment variables — all config is stored in DB.
