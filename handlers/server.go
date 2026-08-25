package handlers

import (
	"context"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent"
	"ledit/logging"
)

type Server struct {
	Router       *gin.Engine
	DB           *ent.Client
	WSHub        *WSHub
	Ctx          context.Context
	LogStore     *logging.LogStore
	OTelExporter *logging.OTelExporter
	LogCleanup   *logging.LogCleanup
	Telemetry    *logging.Telemetry
}

func New(driver *sql.Driver, telemetry *logging.Telemetry) *Server {
	client := ent.NewClient(ent.Driver(driver))
	ctx := context.Background()

	if err := client.Schema.Create(ctx); err != nil {
		slog.Error("Failed to create schema resources", "error", err)
		panic(err)
	}

	// Bootstrap admin credentials on first run.
	initAdminSettings(client, ctx)

	// Seed user table from legacy admin if needed.
	seedUsers(client, ctx)

	// Backfill tokens for any legacy device rows that lack one.
	backfillDeviceTokens(client, ctx)

	// Initialize central logging system (DB-backed, OTEL-ready).
	// This sets slog.SetDefault, so all subsequent slog calls use it.
	logStore, otelExp, logCleanup := logging.InitLogging(client, "warn")

	router := gin.Default()

	// OpenTelemetry request tracing — creates spans for every HTTP request,
	// propagates trace context from incoming traceparent headers.
	if telemetry != nil && telemetry.IsEnabled() {
		router.Use(otelgin.Middleware("ledit"))
		router.Use(metricsMiddleware())
	}

	srv := &Server{
		Router:       router,
		DB:           client,
		WSHub:        NewWSHub(client),
		Ctx:          ctx,
		LogStore:     logStore,
		OTelExporter: otelExp,
		LogCleanup:   logCleanup,
		Telemetry:    telemetry,
	}

	srv.setupRoutes()
	StartEventRuleEngine(client)
	StartGreetingWatcher(ctx, client, defaultHAFetcher(srv), srv)
	InitOutbound(srv)

	mqttCtrl = StartMQTT(srv)
	SetGlobalMqttCtrl(mqttCtrl)
	tgBot = StartTelegram(srv)
	InitChartRecording(client)
	_ = PurgeOldSamples(ctx, client)
	StartChartPurgeLoop(ctx, client)
	StartTimelapseWriter(client)

	return srv
}

// metricsMiddleware records HTTP request count and duration via OTel metrics.
func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		logging.RecordHTTPRequest(
			c.Request.Context(),
			c.Request.Method,
			c.FullPath(),
			c.Writer.Status(),
			duration,
		)
	}
}

func initAdminSettings(client *ent.Client, ctx context.Context) {
	disableAuth := os.Getenv("LEDIT_AUTH_DISABLE")
	if disableAuth == "true" || disableAuth == "1" {
		slog.Warn("Authentication is DISABLED by LEDIT_AUTH_DISABLE env var")
		authEnabled = false
		return
	}

	count, err := client.AdminSettings.Query().Count(ctx)
	if err != nil || count > 0 {
		authEnabled = true
		return
	}

	password := os.Getenv("LEDIT_ADMIN_PASSWORD")
	if password == "" {
		password = "ledit"
		slog.Warn("Using default admin password — set LEDIT_ADMIN_PASSWORD env var for security")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("Failed to hash admin password", "error", err)
		panic(err)
	}

	if _, err := client.AdminSettings.Create().SetUsername("admin").SetPasswordHash(string(hash)).Save(ctx); err != nil {
		slog.Error("Failed to create initial admin settings", "error", err)
		panic(err)
	}
	slog.Info("Initial admin credentials created")
	authEnabled = true
}

// backfillDeviceTokens assigns a token to any device row that lacks one
// (legacy rows created before per-device tokens were introduced).
func backfillDeviceTokens(client *ent.Client, ctx context.Context) {
	devices, err := client.DeviceSettings.Query().All(ctx)
	if err != nil {
		slog.Warn("Failed to query devices for token backfill", "error", err)
		return
	}
	for _, d := range devices {
		if d.Token == "" {
			if err := client.DeviceSettings.UpdateOne(d).SetToken(generateDeviceToken()).Exec(ctx); err != nil {
				slog.Warn("Failed to backfill device token", "id", d.ID, "error", err)
			}
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Router.ServeHTTP(w, r)
}

// templatesDir locates the web/templates directory regardless of the current
// working directory (tests run from the package dir, the binary from the repo
// root). It walks upward from cwd until web/templates is found.
func templatesDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "web/templates"
	}
	for {
		candidate := filepath.Join(dir, "web", "templates")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "web/templates"
		}
		dir = parent
	}
}

func (s *Server) setupRoutes() {
	tmpl := template.New("").Funcs(template.FuncMap{
		"isSelected": func(selected []string, name string) bool {
			for _, s := range selected {
				if s == name {
					return true
				}
			}
			return false
		},
		"index": func(item any, key any) any {
			if item == nil || key == nil {
				return ""
			}
			rv := reflect.ValueOf(item)
			if rv.Kind() == reflect.Ptr {
				if rv.IsNil() {
					return ""
				}
				rv = rv.Elem()
			}
			switch rv.Kind() {
			case reflect.Map:
				kv := reflect.ValueOf(key)
				// try direct map lookup; if key type mismatch, try string conversion
				mv := rv.MapIndex(kv)
				if mv.IsValid() {
					return mv.Interface()
				}
				// fallback: if key is int but map expects int, already handled; try string-key lookup for map[string]*
				if kv.Kind() == reflect.String {
					// already tried
				}
				return ""
			case reflect.Array, reflect.Slice, reflect.String:
				if idx, ok := key.(int); ok {
					if idx >= 0 && idx < rv.Len() {
						return rv.Index(idx).Interface()
					}
					return ""
				}
				if idx64, ok := key.(int64); ok {
					i := int(idx64)
					if i >= 0 && i < rv.Len() {
						return rv.Index(i).Interface()
					}
					return ""
				}
				return ""
			case reflect.Struct:
				// safe fallback: don't panic, return empty for index on struct (playlist_form error handling)
				return ""
			default:
				return ""
			}
		},
	})
	filepath.Walk(templatesDir(), func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		_, err = tmpl.ParseFiles(path)
		return err
	})
	s.Router.SetHTMLTemplate(tmpl)

	s.Router.Use(s.EInkMiddleware())
	s.Router.Use(s.SetupMiddleware())

	s.Router.Static("/static", "./web/static")
	s.Router.GET("/media/*filepath", func(c *gin.Context) {
		rel := c.Param("filepath")
		// Timelapse path is admin-only.
		if strings.HasPrefix(rel, "/timelapse") {
			if authEnabled {
				token := ""
				if t, err := c.Cookie("session"); err == nil {
					token = t
				}
				authMu.Lock()
				_, valid := sessions[token]
				authMu.Unlock()
				if !valid {
					// Also accept bearer token via IsAuthenticated when Server available
					if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
						c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
						return
					}
				}
			}
		}
		c.File(filepath.Join("./web/media", filepath.FromSlash(rel)))
	})

	s.Router.GET("/metrics", s.MetricsHandler)
	s.Router.GET("/", s.IndexHandler)
	s.Router.GET("/ws/feed", s.WSHub.HandleWS)
	s.Router.GET("/ws/device/:token", s.WSHub.HandleDeviceWS)
	// Device-accurate preview: admin session (not the device token), never
	// touches last_seen_at. AuthMiddleware redirects to /login on failure.
	// NOTE: the param must stay named ":token" — gin rejects a different
	// wildcard name at the same tree position as /ws/device/:token.
	s.Router.GET("/ws/device/:token/preview", AuthMiddleware(), s.WSHub.HandleDevicePreviewWS)
	s.Router.GET("/eink/toggle", s.AdminEInkToggleFeed)

	api := s.Router.Group("/api")
	{
		// Public only: health and TRMNL polling stay unauthenticated.
		api.GET("/trmnl/stats", s.APITrmnlStats)
		api.GET("/health", s.APIHealth)

		// Authenticated reads: feed and notifications require session or bearer token.
		authReads := api.Group("")
		authReads.Use(s.RequireViewer())
		{
			authReads.GET("/feed/current", s.APIFeedStatus)
			authReads.GET("/analytics/weights", s.APIAnalyticsWeights)
			authReads.GET("/notifications", s.APINotificationHistory)
			authReads.GET("/playlists/resolve", s.HandlePlaylistResolve)
			authReads.GET("/timelapse/frames", s.APITimelapseFrames)
			authReads.POST("/timelapse/export", s.APITimelapseExport)
		}

		// Mutations: every write requires admin role.
		apiMut := api.Group("")
		apiMut.Use(s.RequireAdmin())
		{
			apiMut.POST("/feed/next", s.APIFeedNext)
			apiMut.POST("/feed/pause", s.APIFeedPause)
			apiMut.POST("/feed/resume", s.APIFeedResume)
		}
		// Webhook routes: machine integrations authenticate via webhook key
		// (X-API-Key header or ?token=), not admin sessions.
		api.POST("/feed/priority", s.WebhookAuthMiddleware(), s.APIFeedPriority)
		api.POST("/webhook/notify", s.WebhookAuthMiddleware(), s.APIWebhookNotify)
		// Test-only helpers (enabled when LEDIT_AUTH_DISABLE=true for Playwright).
		if os.Getenv("LEDIT_AUTH_DISABLE") == "true" || os.Getenv("LEDIT_AUTH_DISABLE") == "1" {
			api.POST("/test/seed-timelapse", s.TestSeedTimelapse)
			api.POST("/test/enable-auth", s.TestEnableAuth)
		}
		api.GET("/display", s.WebhookAuthMiddleware(), s.APIDisplay)
		// Pixel art import (admin only)
		apiPixel := api.Group("/pixelart")
		apiPixel.Use(s.RequireAdmin())
		{
			apiPixel.POST("/import", s.PixelArtImport)
			apiPixel.POST("/import/preview", s.PixelArtImportPreview)
			apiPixel.POST("/generate", s.PixelArtGenerate)
			apiPixel.POST("/:id/refine", s.PixelArtRefine)
			apiPixel.POST("/:id/publish", s.PixelArtPublish)
		}
	}

	s.Router.GET("/setup", s.SetupPage)
	s.Router.POST("/setup", s.SetupAction)

	s.Router.GET("/login", s.LoginPage)
	s.Router.POST("/login", s.LoginAction)
	s.Router.GET("/logout", s.LogoutAction)

	// Password recovery (public: no auth required)
	s.Router.GET("/forgot-password", s.ForgotPasswordPage)
	s.Router.POST("/forgot-password", s.ForgotPasswordAction)
	s.Router.GET("/reset-password", s.ResetPasswordPage)
	s.Router.POST("/reset-password", s.ResetPasswordAction)

	admin := s.Router.Group("/admin")
	admin.Use(AuthMiddleware(), FlashMiddleware(), s.AdminRoleMiddleware())
	{
		admin.GET("/", s.AdminDashboard)
		admin.GET("/settings", s.AdminSettings)
		admin.POST("/settings", s.AdminSettingsSave)
		admin.GET("/notifications", s.AdminNotifications)

		// Sonarr
		admin.GET("/datasources/sonarr/new", s.AdminSonarrNew)
		admin.POST("/datasources/sonarr/new", s.AdminSonarrCreate)
		admin.GET("/datasources/sonarr/:id/edit", s.AdminSonarrEdit)
		admin.POST("/datasources/sonarr/:id/edit", s.AdminSonarrUpdate)
		admin.POST("/datasources/sonarr/:id/delete", s.AdminSonarrDelete)

		// Radarr
		admin.GET("/datasources/radarr/new", s.AdminRadarrNew)
		admin.POST("/datasources/radarr/new", s.AdminRadarrCreate)
		admin.GET("/datasources/radarr/:id/edit", s.AdminRadarrEdit)
		admin.POST("/datasources/radarr/:id/edit", s.AdminRadarrUpdate)
		admin.POST("/datasources/radarr/:id/delete", s.AdminRadarrDelete)

		// F1
		admin.GET("/datasources/f1/new", s.AdminF1New)
		admin.POST("/datasources/f1/new", s.AdminF1Create)
		admin.GET("/datasources/f1/:id/edit", s.AdminF1Edit)
		admin.POST("/datasources/f1/:id/edit", s.AdminF1Update)
		admin.POST("/datasources/f1/:id/delete", s.AdminF1Delete)

		// Weather
		admin.GET("/datasources/weather/new", s.AdminWeatherNew)
		admin.POST("/datasources/weather/new", s.AdminWeatherCreate)
		admin.GET("/datasources/weather/:id/edit", s.AdminWeatherEdit)
		admin.POST("/datasources/weather/:id/edit", s.AdminWeatherUpdate)
		admin.POST("/datasources/weather/:id/delete", s.AdminWeatherDelete)

		// HomeAssistant
		admin.GET("/datasources/homeassistant/new", s.AdminHomeAssistantNew)
		admin.POST("/datasources/homeassistant/new", s.AdminHomeAssistantCreate)
		admin.GET("/datasources/homeassistant/:id/edit", s.AdminHomeAssistantEdit)
		admin.POST("/datasources/homeassistant/:id/edit", s.AdminHomeAssistantUpdate)
		admin.POST("/datasources/homeassistant/:id/delete", s.AdminHomeAssistantDelete)

		// Untappd
		admin.GET("/datasources/untappd/new", s.AdminUntappdNew)
		admin.POST("/datasources/untappd/new", s.AdminUntappdCreate)
		admin.GET("/datasources/untappd/:id/edit", s.AdminUntappdEdit)
		admin.POST("/datasources/untappd/:id/edit", s.AdminUntappdUpdate)
		admin.POST("/datasources/untappd/:id/delete", s.AdminUntappdDelete)

		// Images
		admin.GET("/datasources/images/new", s.AdminImageNew)
		admin.POST("/datasources/images/new", s.AdminImageCreate)
		admin.GET("/datasources/images/:id/edit", s.AdminImageEdit)
		admin.POST("/datasources/images/:id/edit", s.AdminImageUpdate)
		admin.POST("/datasources/images/:id/delete", s.AdminImageDelete)

		// Videos
		admin.GET("/datasources/videos/new", s.AdminVideoNew)
		admin.POST("/datasources/videos/new", s.AdminVideoCreate)
		admin.GET("/datasources/videos/:id/edit", s.AdminVideoEdit)
		admin.POST("/datasources/videos/:id/edit", s.AdminVideoUpdate)
		admin.POST("/datasources/videos/:id/delete", s.AdminVideoDelete)

		// Crypto
		admin.GET("/datasources/crypto/new", s.AdminCryptoNew)
		admin.POST("/datasources/crypto/new", s.AdminCryptoCreate)
		admin.GET("/datasources/crypto/:id/edit", s.AdminCryptoEdit)
		admin.POST("/datasources/crypto/:id/edit", s.AdminCryptoUpdate)
		admin.POST("/datasources/crypto/:id/delete", s.AdminCryptoDelete)

		// Schedules
		admin.GET("/schedules", s.AdminScheduleList)
		admin.GET("/schedules/new", s.AdminScheduleNew)
		admin.POST("/schedules/new", s.AdminScheduleCreate)
		admin.GET("/schedules/:id/edit", s.AdminScheduleEdit)
		admin.POST("/schedules/:id/edit", s.AdminScheduleUpdate)
		admin.POST("/schedules/:id/delete", s.AdminScheduleDelete)

		// Devices (Phase 7)
		admin.GET("/devices", s.AdminDeviceSettingsList)
		admin.GET("/devices/new", s.AdminDeviceSettingsNew)
		admin.POST("/devices/new", s.AdminDeviceSettingsCreate)
		admin.GET("/devices/:id/edit", s.AdminDeviceSettingsEdit)
		admin.POST("/devices/:id/edit", s.AdminDeviceSettingsUpdate)
		admin.GET("/devices/:id/preview", s.AdminDevicePreview)
		admin.POST("/devices/:id/delete", s.AdminDeviceSettingsDelete)

		// Groups
		admin.GET("/groups", s.AdminGroupList)
		admin.GET("/groups/new", s.AdminGroupNew)
		admin.POST("/groups/new", s.AdminGroupCreate)
		admin.GET("/groups/:id", s.AdminGroupDetail)
		admin.POST("/groups/:id", s.AdminGroupUpdate)
		admin.GET("/api/groups", s.APIGroupList)
		admin.POST("/api/groups", s.APIGroupCreate)
		admin.DELETE("/api/groups/:id", s.APIGroupDelete)
		admin.POST("/api/groups/:id/members", s.APIGroupAddMember)
		admin.DELETE("/api/groups/:id/members/:deviceId", s.APIGroupRemoveMember)
		admin.POST("/api/groups/:id/feed/pause", s.APIGroupFeedPause)
		admin.POST("/api/groups/:id/feed/resume", s.APIGroupFeedResume)
		admin.POST("/api/groups/:id/feed/next", s.APIGroupFeedNext)
		admin.POST("/api/groups/:id/feed/priority", s.APIGroupFeedPriority)

		// Theme (Phase 8)
		admin.GET("/theme", s.AdminThemeEditor)
		admin.POST("/theme", s.AdminThemeSave)

		// Stock
		admin.GET("/datasources/stock/new", s.AdminStockNew)
		admin.POST("/datasources/stock/new", s.AdminStockCreate)
		admin.GET("/datasources/stock/:id/edit", s.AdminStockEdit)
		admin.POST("/datasources/stock/:id/edit", s.AdminStockUpdate)
		admin.POST("/datasources/stock/:id/delete", s.AdminStockDelete)

		// RSS Feed
		admin.GET("/datasources/rssfeed/new", s.AdminRssFeedNew)
		admin.POST("/datasources/rssfeed/new", s.AdminRssFeedCreate)
		admin.GET("/datasources/rssfeed/:id/edit", s.AdminRssFeedEdit)
		admin.POST("/datasources/rssfeed/:id/edit", s.AdminRssFeedUpdate)
		admin.POST("/datasources/rssfeed/:id/delete", s.AdminRssFeedDelete)

		// Calendar
		admin.GET("/datasources/calendar/new", s.AdminCalendarNew)
		admin.POST("/datasources/calendar/new", s.AdminCalendarCreate)
		admin.GET("/datasources/calendar/:id/edit", s.AdminCalendarEdit)
		admin.POST("/datasources/calendar/:id/edit", s.AdminCalendarUpdate)
		admin.POST("/datasources/calendar/:id/delete", s.AdminCalendarDelete)

		// Google Calendar
		admin.GET("/datasources/googlecalendar/new", s.AdminGoogleCalendarNew)
		admin.POST("/datasources/googlecalendar/new", s.AdminGoogleCalendarCreate)
		admin.GET("/datasources/googlecalendar/:id/edit", s.AdminGoogleCalendarEdit)
		admin.POST("/datasources/googlecalendar/:id/edit", s.AdminGoogleCalendarUpdate)
		admin.POST("/datasources/googlecalendar/:id/delete", s.AdminGoogleCalendarDelete)

		// News
		admin.GET("/datasources/newsfeed/new", s.AdminNewsFeedNew)
		admin.POST("/datasources/newsfeed/new", s.AdminNewsFeedCreate)
		admin.GET("/datasources/newsfeed/:id/edit", s.AdminNewsFeedEdit)
		admin.POST("/datasources/newsfeed/:id/edit", s.AdminNewsFeedUpdate)
		admin.POST("/datasources/newsfeed/:id/delete", s.AdminNewsFeedDelete)

		// Custom API
		admin.GET("/datasources/genericapi/new", s.AdminGenericAPINew)
		admin.POST("/datasources/genericapi/new", s.AdminGenericAPICreate)
		admin.GET("/datasources/genericapi/:id/edit", s.AdminGenericAPIEdit)
		admin.POST("/datasources/genericapi/:id/edit", s.AdminGenericAPIUpdate)
		admin.POST("/datasources/genericapi/:id/delete", s.AdminGenericAPIDelete)

		// Transit
		admin.GET("/datasources/transit/new", func(c *gin.Context) { s.renderForm(c, "Transit", "transit", false, nil) })
		admin.POST("/datasources/transit/new", func(c *gin.Context) { s.createTokenURLDS(c, "transit") })
		admin.GET("/datasources/transit/:id/edit", func(c *gin.Context) { s.editTokenURLDS(c, "transit") })
		admin.POST("/datasources/transit/:id/edit", func(c *gin.Context) { s.updateTokenURLDS(c, "transit") })
		admin.POST("/datasources/transit/:id/delete", func(c *gin.Context) { s.deleteTokenURLDS(c, "transit") })

		// Uptime
		admin.GET("/datasources/uptime/new", func(c *gin.Context) {
			s.renderPage(c, 200, "datasource_form.html", gin.H{"type": "Uptime", "endpoint": "uptime", "has_config": true})
		})
		admin.POST("/datasources/uptime/new", func(c *gin.Context) { s.createFieldDS(c, "uptime") })
		admin.GET("/datasources/uptime/:id/edit", func(c *gin.Context) { s.editFieldDS(c, "uptime", nil) })
		admin.POST("/datasources/uptime/:id/edit", func(c *gin.Context) { s.updateFieldDS(c, "uptime") })
		admin.POST("/datasources/uptime/:id/delete", func(c *gin.Context) { s.deleteFieldDS(c, "uptime") })

		// Pi-hole
		admin.GET("/datasources/pihole/new", func(c *gin.Context) { s.renderForm(c, "Pi-hole", "pihole", false, nil) })
		admin.POST("/datasources/pihole/new", func(c *gin.Context) { s.createTokenURLDS(c, "pihole") })
		admin.GET("/datasources/pihole/:id/edit", func(c *gin.Context) { s.editTokenURLDS(c, "pihole") })
		admin.POST("/datasources/pihole/:id/edit", func(c *gin.Context) { s.updateTokenURLDS(c, "pihole") })
		admin.POST("/datasources/pihole/:id/delete", func(c *gin.Context) { s.deleteTokenURLDS(c, "pihole") })

		// GitHub
		admin.GET("/datasources/github/new", func(c *gin.Context) { s.renderForm(c, "GitHub", "github", false, nil) })
		admin.POST("/datasources/github/new", func(c *gin.Context) { s.createTokenURLDS(c, "github") })
		admin.GET("/datasources/github/:id/edit", func(c *gin.Context) { s.editTokenURLDS(c, "github") })
		admin.POST("/datasources/github/:id/edit", func(c *gin.Context) { s.updateTokenURLDS(c, "github") })
		admin.POST("/datasources/github/:id/delete", func(c *gin.Context) { s.deleteTokenURLDS(c, "github") })

		// Sports
		admin.GET("/datasources/sports/new", func(c *gin.Context) { s.renderForm(c, "Sports", "sports", false, nil) })
		admin.POST("/datasources/sports/new", func(c *gin.Context) { s.createTokenURLDS(c, "sports") })
		admin.GET("/datasources/sports/:id/edit", func(c *gin.Context) { s.editTokenURLDS(c, "sports") })
		admin.POST("/datasources/sports/:id/edit", func(c *gin.Context) { s.updateTokenURLDS(c, "sports") })
		admin.POST("/datasources/sports/:id/delete", func(c *gin.Context) { s.deleteTokenURLDS(c, "sports") })

		// Sun/Moon
		admin.GET("/datasources/sunmoon/new", func(c *gin.Context) { s.renderForm(c, "Sun/Moon", "sunmoon", false, nil) })
		admin.POST("/datasources/sunmoon/new", func(c *gin.Context) { s.createTokenURLDS(c, "sunmoon") })
		admin.GET("/datasources/sunmoon/:id/edit", func(c *gin.Context) { s.editTokenURLDS(c, "sunmoon") })
		admin.POST("/datasources/sunmoon/:id/edit", func(c *gin.Context) { s.updateTokenURLDS(c, "sunmoon") })
		admin.POST("/datasources/sunmoon/:id/delete", func(c *gin.Context) { s.deleteTokenURLDS(c, "sunmoon") })

		// Jellyfin
		admin.GET("/datasources/jellyfin/new", func(c *gin.Context) { s.renderForm(c, "Jellyfin", "jellyfin", false, nil) })
		admin.POST("/datasources/jellyfin/new", func(c *gin.Context) { s.createTokenURLDS(c, "jellyfin") })
		admin.GET("/datasources/jellyfin/:id/edit", func(c *gin.Context) { s.editTokenURLDS(c, "jellyfin") })
		admin.POST("/datasources/jellyfin/:id/edit", func(c *gin.Context) { s.updateTokenURLDS(c, "jellyfin") })
		admin.POST("/datasources/jellyfin/:id/delete", func(c *gin.Context) { s.deleteTokenURLDS(c, "jellyfin") })
		admin.POST("/datasources/genericapi/test", s.AdminGenericAPITest)

		// QR Code
		admin.GET("/qrcodes", s.AdminQrcodeList)
		admin.GET("/qrcodes/new", s.AdminQrcodeNew)
		admin.POST("/qrcodes/new", s.AdminQrcodeCreate)
		admin.GET("/qrcodes/:id/edit", s.AdminQrcodeEdit)
		admin.POST("/qrcodes/:id/edit", s.AdminQrcodeUpdate)
		admin.POST("/qrcodes/:id/delete", s.AdminQrcodeDelete)
		// JSON API (session auth)
		admin.GET("/api/qrcodes", s.APIQrcodeList)
		admin.GET("/api/qrcodes/:id", s.APIQrcodeGet)
		admin.POST("/api/qrcodes", s.APIQrcodeCreate)
		admin.PUT("/api/qrcodes/:id", s.APIQrcodeUpdate)
		admin.DELETE("/api/qrcodes/:id", s.APIQrcodeDelete)

		// Pixel Art
		admin.GET("/pixelarts", s.PixelArtList)
		admin.GET("/pixelarts/new", s.PixelArtNew)
		admin.POST("/pixelarts/new", s.PixelArtCreate)
		admin.GET("/pixelarts/:id/edit", s.PixelArtEdit)
		admin.POST("/pixelarts/:id/edit", s.PixelArtUpdate)
		admin.POST("/pixelarts/:id/delete", s.PixelArtDelete)
		admin.POST("/pixelarts/preview", s.PixelArtPreview)
		admin.POST("/pixelarts/import", s.PixelArtImport)
		admin.POST("/pixelarts/import/preview", s.PixelArtImportPreview)

		// Playlists
		admin.GET("/playlists", s.AdminPlaylistList)
		admin.GET("/playlists/new", s.AdminPlaylistNew)
		admin.POST("/playlists/new", s.AdminPlaylistCreate)
		admin.GET("/playlists/:id/edit", s.AdminPlaylistEdit)
		admin.POST("/playlists/:id/edit", s.AdminPlaylistUpdate)
		admin.POST("/playlists/:id/delete", s.AdminPlaylistDelete)

		// Event rules
		admin.GET("/eventrules", s.AdminEventRuleList)
		admin.GET("/eventrules/new", s.AdminEventRuleNew)
		admin.POST("/eventrules/new", s.AdminEventRuleCreate)
		admin.GET("/eventrules/:id/edit", s.AdminEventRuleEdit)
		admin.POST("/eventrules/:id/edit", s.AdminEventRuleUpdate)
		admin.POST("/eventrules/:id/delete", s.AdminEventRuleDelete)

		// Greetings
		admin.GET("/greetings", s.AdminGreetings)
		admin.GET("/api/greetings", s.APIGreetingList)
		admin.POST("/api/greetings", s.APIGreetingCreate)
		admin.PUT("/api/greetings/:id", s.APIGreetingUpdate)
		admin.DELETE("/api/greetings/:id", s.APIGreetingDelete)
		admin.POST("/api/greetings/:id/test", s.APIGreetingTest)

		// Matrix layouts
		admin.GET("/matrixlayouts", s.AdminMatrixLayoutList)
		admin.GET("/matrixlayouts/new", s.AdminMatrixLayoutNew)
		admin.POST("/matrixlayouts/new", s.AdminMatrixLayoutCreate)
		admin.GET("/matrixlayouts/:id/edit", s.AdminMatrixLayoutEdit)
		admin.POST("/matrixlayouts/:id/edit", s.AdminMatrixLayoutUpdate)
		admin.POST("/matrixlayouts/:id/delete", s.AdminMatrixLayoutDelete)

		// On-demand previews (live previews + PNG template export)
		admin.GET("/preview", s.AdminPreview)
		admin.POST("/preview/datasource", s.AdminPreviewDatasource)
		admin.POST("/preview/matrix", s.AdminPreviewMatrix)

		// Text Slides (Phase 4)
		admin.GET("/textslides/new", s.AdminTextSlideNew)
		admin.POST("/textslides/new", s.AdminTextSlideCreate)
		admin.GET("/textslides/:id/edit", s.AdminTextSlideEdit)
		admin.POST("/textslides/:id/edit", s.AdminTextSlideUpdate)
		admin.POST("/textslides/:id/delete", s.AdminTextSlideDelete)

		// Countdowns (ambience modes)
		admin.GET("/countdowns/new", s.AdminCountdownNew)
		admin.POST("/countdowns/new", s.AdminCountdownCreate)
		admin.GET("/countdowns/:id/edit", s.AdminCountdownEdit)
		admin.POST("/countdowns/:id/edit", s.AdminCountdownUpdate)
		admin.POST("/countdowns/:id/delete", s.AdminCountdownDelete)

		// AI Digests (ai-features)
		admin.GET("/aidigests/new", s.AdminAIDigestNew)
		admin.POST("/aidigests/new", s.AdminAIDigestCreate)
		admin.GET("/aidigests/:id/edit", s.AdminAIDigestEdit)
		admin.POST("/aidigests/:id/edit", s.AdminAIDigestUpdate)
		admin.POST("/aidigests/:id/delete", s.AdminAIDigestDelete)
		admin.POST("/aidigests/:id/refresh", s.AdminAIDigestRefresh)

		// AI slide generation (human-in-the-loop, nothing saved)
		admin.POST("/textslides/generate", s.AdminTextSlideGenerate)

		// Log Viewer (Phase 11)
		admin.GET("/logs", s.AdminLogs)
		admin.GET("/api/logs", s.AdminLogsAPI)

		// Log Settings
		admin.GET("/settings/logs", s.AdminLogSettings)
		admin.POST("/settings/logs", s.AdminLogSettingsSave)

		// Email Settings
		admin.GET("/settings/email", s.AdminEmailSettings)
		admin.POST("/settings/email", s.AdminEmailSettingsSave)

		// AI Settings
		admin.GET("/settings/ai", s.AdminAISettings)
		admin.POST("/settings/ai", s.AdminAISettingsSave)
		admin.POST("/settings/ai/test", s.AdminAISettingsTestConnection)

		// Umami Analytics Settings
		admin.GET("/settings/umami", s.AdminUmamiSettings)
		admin.POST("/settings/umami", s.AdminUmamiSettingsSave)

		// Alert Settings
		admin.GET("/settings/alerts", s.AdminAlertSettings)
		admin.POST("/settings/alerts", s.AdminAlertSettingsSave)
		admin.POST("/settings/alerts/test", s.AdminAlertSettingsTest)

		// Webhook/MQTT/Telegram Settings
		admin.GET("/webhook", s.AdminWebhookSettingsGET)
		admin.POST("/webhook", s.AdminWebhookSettingsPOST)
		admin.GET("/mqtt", s.AdminMQTTSettingsGET)
		admin.POST("/mqtt", s.AdminMQTTSettingsPOST)
		admin.GET("/telegram", s.AdminTelegramSettingsGET)
		admin.POST("/telegram", s.AdminTelegramSettingsPOST)

		// Password Change
		admin.GET("/password", s.AdminPasswordChange)
		admin.POST("/password", s.AdminPasswordChangeSave)

		// E-Ink Mode
		admin.GET("/eink/toggle", s.AdminEInkToggle)
		admin.POST("/eink/refresh", s.AdminEInkRefresh)

		// Analytics (Phase 10)
		admin.GET("/analytics", s.AdminAnalytics)

		// Timelapse gallery + API
		admin.GET("/timelapse", s.TimelapseGallery)
		admin.GET("/api/timelapse/frames", s.APITimelapseFrames)
		admin.POST("/api/timelapse/export", s.APITimelapseExport)

		// API tokens (secure-api-mutations): owner-only lifecycle management.
		admin.GET("/api-tokens", s.AdminAPITokens)
		admin.POST("/api-tokens", s.AdminAPITokenCreate)
		admin.POST("/api-tokens/:id/revoke", s.AdminAPITokenRevoke)
		admin.POST("/api-tokens/:id/rotate", s.AdminAPITokenRotate)

		// Datasource Plugins
		admin.GET("/plugins", s.AdminPlugins)
		admin.GET("/plugins/new", s.AdminPluginNew)
		admin.POST("/plugins/new", s.AdminPluginCreate)
		admin.GET("/plugins/:id/edit", s.AdminPluginEdit)
		admin.POST("/plugins/:id/edit", s.AdminPluginUpdate)
		admin.POST("/plugins/:id/delete", s.AdminPluginDelete)
		admin.GET("/api/plugins", s.APIPluginList)
		admin.GET("/api/plugins/:id", s.APIPluginGet)
		admin.POST("/api/plugins", s.APIPluginCreate)
		admin.PUT("/api/plugins/:id", s.APIPluginUpdate)
		admin.DELETE("/api/plugins/:id", s.APIPluginDelete)
		admin.GET("/api/plugins/:id/health", s.APIPluginHealth)

		// Outbound events
		admin.GET("/events", s.AdminEventsPage)
		admin.GET("/api/outbound/webhooks", s.OutboundWebhooksList)
		admin.POST("/api/outbound/webhooks", s.OutboundWebhooksCreate)
		admin.DELETE("/api/outbound/webhooks/:id", s.OutboundWebhooksDelete)
		admin.POST("/api/outbound/webhooks/:id/test", s.OutboundWebhooksTest)
		admin.GET("/api/outbound/settings", s.OutboundSettingsGet)
		admin.PUT("/api/outbound/settings", s.OutboundSettingsPut)

		// Backup & Restore
		admin.GET("/backup", s.AdminBackupPage)
		admin.GET("/api/backup/export", s.BackupExportHandler)
		admin.POST("/api/backup/import", s.BackupImportHandler)

		// Users management (admin-only)
		admin.GET("/users", s.AdminUsersPage)
		admin.GET("/api/users", s.APIUsersList)
		admin.POST("/api/users", s.APIUsersCreate)
		admin.DELETE("/api/users/:id", s.APIUsersDelete)
		admin.POST("/api/users/:id/role", s.APIUsersChangeRole)
		admin.POST("/api/users/:id/password", s.APIUsersResetPassword)
	}
}
