package handlers

import (
	"context"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
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
}

func New(driver *sql.Driver) *Server {
	client := ent.NewClient(ent.Driver(driver))
	ctx := context.Background()

	if err := client.Schema.Create(ctx); err != nil {
		slog.Error("Failed to create schema resources", "error", err)
		panic(err)
	}

	// Initialize central logging system (DB-backed, OTEL-ready).
	// This sets slog.SetDefault, so all subsequent slog calls use it.
	logStore, otelExp, logCleanup := logging.InitLogging(client, "warn")

	router := gin.Default()

	srv := &Server{
		Router:       router,
		DB:           client,
		WSHub:        NewWSHub(client),
		Ctx:          ctx,
		LogStore:     logStore,
		OTelExporter: otelExp,
		LogCleanup:   logCleanup,
	}

	srv.setupRoutes()

	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Router.ServeHTTP(w, r)
}

func (s *Server) setupRoutes() {
	tmpl := template.New("")
	filepath.Walk("web/templates", func(path string, info fs.FileInfo, err error) error {
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

	s.Router.Static("/static", "./web/static")
	s.Router.Static("/media", "./web/media")

	s.Router.GET("/", s.IndexHandler)
	s.Router.GET("/ws/feed", s.WSHub.HandleWS)

	api := s.Router.Group("/api")
	{
		api.GET("/feed/current", s.APIFeedStatus)
		api.POST("/feed/next", s.APIFeedNext)
		api.POST("/feed/pause", s.APIFeedPause)
		api.POST("/feed/resume", s.APIFeedResume)
		api.POST("/feed/priority", s.APIFeedPriority)
		api.POST("/webhook/notify", s.APIWebhookNotify)
		api.GET("/notifications", s.APINotificationHistory)
	}

	s.Router.GET("/login", s.LoginPage)
	s.Router.POST("/login", s.LoginAction)
	s.Router.GET("/logout", s.LogoutAction)

	admin := s.Router.Group("/admin")
	admin.Use(AuthMiddleware())
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
		admin.POST("/devices/:id/delete", s.AdminDeviceSettingsDelete)

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

		// Text Slides (Phase 4)
		admin.GET("/textslides/new", s.AdminTextSlideNew)
		admin.POST("/textslides/new", s.AdminTextSlideCreate)
		admin.GET("/textslides/:id/edit", s.AdminTextSlideEdit)
		admin.POST("/textslides/:id/edit", s.AdminTextSlideUpdate)
		admin.POST("/textslides/:id/delete", s.AdminTextSlideDelete)

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

		// Analytics (Phase 10)
		admin.GET("/analytics", s.AdminAnalytics)
	}
}
