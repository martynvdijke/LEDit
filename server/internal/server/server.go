package server

import (
	"context"
	"html/template"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/martynvdijke/ledit/internal/ent"
	"entgo.io/ent/dialect/sql"
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

	admin := s.Router.Group("/admin")
	{
		admin.GET("/", s.AdminDashboard)
		admin.GET("/settings", s.AdminSettings)
		admin.POST("/settings", s.AdminSettingsSave)
		admin.GET("/datasources/sonarr/new", s.AdminSonarrNew)
		admin.POST("/datasources/sonarr/new", s.AdminSonarrCreate)
		admin.GET("/datasources/sonarr/:id/edit", s.AdminSonarrEdit)
		admin.POST("/datasources/sonarr/:id/edit", s.AdminSonarrUpdate)
		admin.POST("/datasources/sonarr/:id/delete", s.AdminSonarrDelete)
		admin.GET("/datasources/radarr/new", s.AdminRadarrNew)
		admin.POST("/datasources/radarr/new", s.AdminRadarrCreate)
		admin.GET("/datasources/radarr/:id/edit", s.AdminRadarrEdit)
		admin.POST("/datasources/radarr/:id/edit", s.AdminRadarrUpdate)
		admin.POST("/datasources/radarr/:id/delete", s.AdminRadarrDelete)
	}
}
