## ADDED Requirements

### Requirement: Umami tracking script injected into public pages
The system SHALL conditionally inject the Umami analytics tracking script into public-facing HTML pages when Umami is configured and enabled.

#### Scenario: Script injected when enabled
- **WHEN** Umami settings have `enable` set to `true`
- **AND** a user visits the home page (`/`) or any page using `base.html`
- **THEN** the HTML SHALL include a `<script>` tag with `src` pointing to `{endpoint}/script.js`
- **AND** the script tag SHALL include `data-website-id` attribute set to the configured `website_id`
- **AND** the script tag SHALL include `data-host-url` attribute set to the configured `endpoint`
- **AND** the script tag SHALL use `async` and `defer` attributes

#### Scenario: Script not injected when disabled
- **WHEN** Umami settings have `enable` set to `false`
- **OR** no Umami settings exist
- **THEN** the HTML SHALL NOT include the Umami tracking script

#### Scenario: Script tag uses correct Umami format
- **WHEN** the tracking script is injected
- **THEN** the resulting HTML SHALL contain: `<script async defer src="{{.UmamiEndpoint}}/script.js" data-website-id="{{.WebsiteID}}" data-host-url="{{.UmamiEndpoint}}"></script>`

### Requirement: Umami tracking scope limited to public pages
The tracking script SHALL only be injected into public-facing pages (home/index page and `base.html`-rendered pages), not into admin panel pages.

#### Scenario: Admin pages excluded
- **WHEN** an admin visits any `/admin/*` page
- **THEN** the Umami tracking script SHALL NOT be injected
