# LEDit — LED Matrix Display Server

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/SQLite3-003B57?style=flat&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/Gin-1.12-00ADD8?style=flat&logo=go" alt="Gin">
  <img src="https://img.shields.io/badge/license-MIT-blue" alt="License">
  <img src="https://img.shields.io/badge/docker-ready-2496ED?style=flat&logo=docker" alt="Docker">
</p>

A self-hosted LED matrix display server that cycles through content from multiple datasources and streams them to LED matrix devices via WebSocket. Supports a wide variety of datasources including media servers, weather, crypto, calendars, RSS feeds, and more.

<p align="center">
  <img src="web/static/brand/ledit-lockup-light.svg" alt="LEDit" width="420">
</p>

The project identity lives in [`brandkit/README.md`](brandkit/README.md), with reusable SVG logo variants and shared brand tokens in [`web/static/brand`](web/static/brand/).

## Features

### Data Sources
- **Sonarr** — Display upcoming TV show downloads and wanted items
- **Radarr** — Display upcoming movie downloads and wanted items
- **F1** — Show Formula 1 race schedules and results
- **Weather** — Current weather conditions and forecasts
- **HomeAssistant** — Home automation sensor data and states
- **Untappd** — Recent beer check-ins and brewery stats
- **Crypto** — Cryptocurrency prices and portfolio values
- **Stocks** — Stock market data and portfolio tracking
- **RSS Feeds** — Custom RSS/Atom feed headlines and content
- **Calendars** — iCal calendar events and schedules
- **Images** — Uploaded custom images for display
- **Videos** — Uploaded custom videos for playback
- **Text Slides** — Custom text messages with color, background, and font size
- **System Stats** — Built-in system statistics (CPU, memory, uptime)

### Display & Feed
- **WebSocket Feed** — Real-time streaming of rendered content to LED matrices
- **Feed Control** — Pause, resume, skip via API or WebSocket commands
- **Slide Transitions** — Optional animated hand-off between sources: `fade`, `wipe` (left→right), or `dissolve`; duration configurable 100–2000 ms (default 500 ms) under Admin → Settings. Disabled (`none`) by default. Tuned for LED refresh rates (~12 steps at the default); e-ink/TRMNL devices are unaffected as they poll static images.
- **Priority Messages** — Urgent notifications that interrupt the current feed
- **Notification History** — View past priority notifications
- **Random/Sequential Ordering** — Cycle sources randomly or in order
- **Configurable Display Timeout** — Per-source display duration
- **Multiple Device Support** — Manage multiple LED matrix devices with individual settings (IP, port, resolution)
- **Device Playlists** — Bind a device to a named, ordered playlist of datasources instead of the global rotation (Admin → Playlists, then pick it on the device form's Content section). Playlists play in saved order; Random/shuffle applies to global mode only. Missing or empty playlists fall back to the global list so a wall never goes blank.

### Rendering
- **Custom Font Rendering** — Pixel font (PixelifySans) for crisp LED display text
- **Theme System** — Cyber (dracula-inspired), Default, F1, and Untappd themes
- **Custom Theme Editor** — Configure background color, accent color, text color, title, font size

### Administration
- **Admin Authentication** — Session-based login for admin panel
- **Dashboard** — Overview of all configured datasources and their status
- **Datasource Management** — CRUD for all datasource types
- **Schedule Management** — Cron-based scheduling for automated content cycling
- **Log Viewer** — DB-backed log entries with level/source filtering and search
- **Log Settings** — Configurable verbosity, retention days, OTel export
- **Email Settings** — SMTP configuration for email notifications
- **AI Settings** — LLM provider configuration for AI features
- **Analytics** — Display tracking (displays by source, uptime stats)
- **Umami Analytics** — Optional web analytics integration

### Observability
- **Database-Backed Logging** — All logs stored in SQLite with filtering
- **OpenTelemetry Export** — Forward logs to OTLP-compatible backends
- **Structured Logging** — slog-based logging with source attribution

## Quick Start

### Docker (Recommended)

```bash
docker compose up -d
```

Open **[http://localhost:8080](http://localhost:8080)** in your browser. Navigate to `/admin` and log in with the default credentials (`admin` / `ledit`) to configure datasources.

### Manual Setup

```bash
# Install dependencies
go mod download

# Build
CGO_ENABLED=1 go build -o ledit .

# Run
./ledit
```

## Configuration

LEDit is configured through the admin panel at `/admin/settings`. Key environment variables:

| Variable | Description |
|----------|-------------|
| `DOCKER` | Set to `true` when running in Docker (auto-configured in Dockerfile) |

All other settings (datasources, schedules, devices, themes) are managed through the web UI.

## Frontend Assets

The Go templates use project-owned Vite assets. Install the frontend dependencies with
`npm install`, then run `npm run build` to compile CSS/TypeScript and regenerate the
PWA icons. During frontend development, `npm run dev` starts Vite in watch mode.
`task build` runs the asset build before compiling the Go server; the Dockerfile does
the same in its Node assets stage.

## Project Structure

```
LEDit/
├── main.go                    # Application entry point
├── handlers/
│   ├── server.go              # Route setup & server initialization
│   ├── handlers.go            # CRUD handlers for all datasources
│   ├── feed_control.go        # Feed controller (pause/resume/skip/priority)
│   ├── websocket.go           # WebSocket hub & display loop
│   ├── analytics.go           # Display tracking analytics
│   ├── log_admin.go           # Log viewer & settings admin
│   └── auth.go                # Admin authentication
├── datasource/
│   ├── datasource.go          # Datasource interface & default theme
│   ├── sonarr.go              # Sonarr datasource
│   ├── radarr.go              # Radarr datasource
│   ├── f1.go                  # F1 datasource
│   ├── weather.go             # Weather datasource
│   ├── homeassistant.go       # HomeAssistant datasource
│   ├── untappd.go             # Untappd datasource
│   ├── crypto.go              # Crypto datasource
│   ├── stock.go               # Stock datasource
│   ├── rssfeed.go             # RSS Feed datasource
│   ├── calendar.go            # Calendar (iCal) datasource
│   ├── image.go               # Image datasource
│   ├── video.go               # Video datasource
│   ├── textslide.go           # Text Slide datasource
│   ├── systemstats.go         # System Stats datasource
│   └── api.go                 # External API helpers
├── render/
│   ├── render.go              # Image rendering engine
│   ├── theme.go               # Theme types
│   ├── simplefont.go          # Pixel font renderer
│   └── themes/                # Theme implementations
├── db/
│   └── db.go                  # Database DSN
├── logging/
│   └── ...                    # Structured logging with OTel
├── web/                       # Frontend assets
│   ├── templates/             # Go HTML templates
│   ├── static/                # Static assets (CSS, JS)
│   └── media/                 # Uploaded media files
├── fonts/                     # Pixel font files
├── Dockerfile                 # Multi-stage Docker build
├── docker-compose.yml         # Docker Compose configuration
└── go.mod / go.sum            # Go module dependencies
```

## API Endpoints

### Authentication

Public reads (`/api/trmnl/stats`, `/api/health`) require no credentials.
**Feed status and notification reads require authentication** (valid session cookie or bearer token). **All mutation endpoints require a bearer API token** issued by an authenticated admin. Details require login — unauthenticated visitors to `/` see only basic public info (version, health, login CTA).

1. Log in to the admin UI and open **API tokens** (`/admin/api-tokens`).
2. Create a token — the secret is shown **exactly once** and is stored only as
   a hash. Copy it immediately.
3. Send it on every write request:

```
Authorization: Bearer <secret>
```

Tokens can be revoked or rotated from the same page. Revoked or expired tokens
are rejected with `401 Unauthorized`.

### Feed Control

| Method | Endpoint | Description | Auth |
|--------|----------|-------------|------|
| `GET` | `/api/feed/current` | Current feed status (current source, next source, paused state) | session or bearer token |
| `POST` | `/api/feed/next` | Skip to next source | bearer token |
| `POST` | `/api/feed/pause` | Pause feed cycling | bearer token |
| `POST` | `/api/feed/resume` | Resume feed cycling | bearer token |
| `POST` | `/api/feed/priority` | Send a priority message | bearer token |
| `POST` | `/api/webhook/notify` | Webhook endpoint for external notifications | bearer token |
| `GET` | `/api/notifications` | Notification history | session or bearer token |

### WebSocket

| Endpoint | Description |
|----------|-------------|
| `GET` `/ws/feed` | WebSocket endpoint for receiving rendered display content and sending commands (pause, resume, next) |

### Admin Endpoints

All admin endpoints are under `/admin/` and require authentication. Full CRUD for every datasource type (Sonarr, Radarr, F1, Weather, HomeAssistant, Untappd, Crypto, Stock, RSS Feeds, Calendars, Images, Videos, Text Slides), plus schedules, devices, playlists, theme, settings, logs, analytics, and API token management.

## License

MIT
