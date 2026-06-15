## 1. Database Schema

- [x] 1.1 Create `ent/schema/umamisettings.go` with fields: `id`, `endpoint` (string), `website_id` (string), `enable` (bool, default false)
- [x] 1.2 Add `umami_settings` edge (`edge.To("umami_settings", UmamiSettings.Type)`) to `ent/schema/generalsettings.go`
- [x] 1.3 Run `go generate ./ent` to regenerate Ent code for both schemas

## 2. Backend Handlers

- [x] 2.1 Add `AdminUmamiSettings` and `AdminUmamiSettingsSave` handler methods in `handlers/handlers.go` following the email/ai/log settings pattern (GET renders form, POST saves)
- [x] 2.2 Register new routes in `handlers/server.go`: `GET /admin/settings/umami` and `POST /admin/settings/umami`
- [x] 2.3 Pass Umami settings data to template context in `IndexHandler` and any handler using `base.html`

## 3. Frontend Templates

- [x] 3.1 Create `web/templates/admin/umami_settings.html` with form fields: Endpoint URL, Website ID, Enable checkbox
- [x] 3.2 Add "Umami Analytics" link to sidebar in `web/templates/admin/sidebar.html` and `web/templates/base.html`
- [x] 3.3 Inject conditional Umami tracking script into `web/templates/base.html` and `web/templates/index.html` using `{{if .umamiEnabled}}` blocks

## 4. Verify

- [x] 4.1 Run `go build ./...` to confirm compilation
- [x] 4.2 Run `task pre-push` to run gofmt, tests, and build
- [x] 4.3 Verify the admin settings page renders and saves correctly
