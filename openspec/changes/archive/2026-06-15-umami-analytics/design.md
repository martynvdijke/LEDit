## Context

LEDit is a Go + Gin web application with Ent ORM-backed SQLite storage. The admin panel has several settings modules (Email, AI, Log) that follow a consistent pattern: an Ent schema → generated CRUD → handler methods → HTML template. The existing analytics page shows in-app stats (display counts, uptime) but has no external web analytics.

We need to add configuration for Umami, a self-hosted privacy analytics platform. The setting will let admins configure their Umami endpoint and website ID, then conditionally inject the Umami tracking script into web pages.

## Goals / Non-Goals

**Goals:**
- Admin can configure Umami analytics via the settings panel (endpoint URL, website ID, enable toggle)
- When enabled, the Umami tracking script is injected into `base.html` and `index.html` (the public-facing templates)
- Follow the same pattern as existing settings (EmailSettings, AISettings, LogSettings) for consistency
- All configuration stored in the database (no env vars needed beyond what's already there)

**Non-Goals:**
- Not adding Umami to the admin panel templates (only public-facing pages)
- Not migrating existing in-app analytics out — external Umami is additive
- Not supporting the cloud-hosted Umami differently from self-hosted (same config fields)

## Decisions

1. **Single-row settings table (like EmailSettings)**: We use a dedicated `UmamiSettings` table (Ent schema) rather than adding fields to `GeneralSettings`. This follows the existing pattern for modular settings and avoids bloating the general settings model.

2. **Enable toggle controls injection**: A boolean `enable` field controls whether the tracking script renders. When disabled, no Umami script is emitted. This lets admins configure the connection details before enabling, or disable temporarily without losing config.

3. **Script injection via template conditional**: The Umami tracking script is conditionally injected using Go template `{{if .umamiEnabled}}` blocks in `base.html` and `index.html`. The handler passes the Umami settings to the template context.

4. **Umami tracking parameters**: Umami's tracking script accepts `data-website-id` and `data-host-url` attributes. We use `endpoint` as the Umami instance URL and `website_id` as the tracking site ID. The script src points to `{endpoint}/script.js`.

5. **No migration from existing analytics**: The existing in-app analytics (`analytics.go`/`analytics.html`) track display events server-side. Umami tracks web traffic client-side. They serve different purposes and coexist.

## Risks / Trade-offs

- **Script blocking / CSP**: If the app has a Content Security Policy, the Umami endpoint must be whitelisted. We'll document this but won't auto-modify CSP headers.
- **Network dependency**: If Umami is unreachable, the tracking script will fail to load. The async/defer script loading ensures it doesn't block page rendering.
- **Self-hosted Umami availability**: The app depends on the operator keeping their Umami instance running. Not our concern — the enable toggle lets them turn it off.
