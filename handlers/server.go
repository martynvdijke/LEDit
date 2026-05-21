package handlers

import (
	"context"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"ledit/ent"
)

type Server struct {
	Router *gin.Engine
	DB     *ent.Client
	WSHub  *WSHub
	Ctx    context.Context
}

func New(driver *sql.Driver) *Server {
	client := ent.NewClient(ent.Driver(driver))
	ctx := context.Background()

	if err := client.Schema.Create(ctx); err != nil {
		log.Fatalf("Failed to create schema resources: %v", err)
	}

	router := gin.Default()

	srv := &Server{
		Router: router,
		DB:     client,
		WSHub:  NewWSHub(client),
		Ctx:    ctx,
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

	admin := s.Router.Group("/admin")
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
	}
}
