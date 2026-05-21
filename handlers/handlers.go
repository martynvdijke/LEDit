package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/ent/generalsettings"
)

func (s *Server) IndexHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{})
}

func (s *Server) AdminDashboard(c *gin.Context) {
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().WithImages().WithVideos().Only(s.Ctx)

	stats := gin.H{
		"has_settings": err == nil,
	}
	if err == nil {
		sonarrCount, _ := settings.Edges.SonarrOrErr()
		radarrCount, _ := settings.Edges.RadarrOrErr()
		f1Count, _ := settings.Edges.F1OrErr()
		weatherCount, _ := settings.Edges.WeatherOrErr()
		haCount, _ := settings.Edges.HomeAssistantOrErr()
		untappdCount, _ := settings.Edges.UntappdOrErr()
		imageCount, _ := settings.Edges.ImagesOrErr()
		videoCount, _ := settings.Edges.VideosOrErr()
		stats = gin.H{
			"has_settings":  true,
			"settings":      settings,
			"sonarr_count":  len(sonarrCount),
			"radarr_count":  len(radarrCount),
			"f1_count":      len(f1Count),
			"weather_count": len(weatherCount),
			"ha_count":      len(haCount),
			"untappd_count": len(untappdCount),
			"image_count":   len(imageCount),
			"video_count":   len(videoCount),
			"total_sources": len(sonarrCount) + len(radarrCount) + len(f1Count) + len(weatherCount) + len(haCount) + len(untappdCount) + len(imageCount) + len(videoCount),
		}
	}
	c.HTML(http.StatusOK, "dashboard.html", stats)
}

func (s *Server) AdminSettings(c *gin.Context) {
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	c.HTML(http.StatusOK, "settings.html", gin.H{
		"settings": settings,
		"hasSettings": settings != nil,
	})
}

func (s *Server) AdminSettingsSave(c *gin.Context) {
	timeout, _ := strconv.ParseFloat(c.PostForm("timeout"), 64)
	random := c.PostForm("random") == "on"
	width, _ := strconv.Atoi(c.PostForm("width"))
	height, _ := strconv.Atoi(c.PostForm("height"))

	exists, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Exist(s.Ctx)
	if !exists {
		s.DB.GeneralSettings.Create().
			SetTimeout(timeout).
			SetRandom(random).
			SetWidth(width).
			SetHeight(height).
			Save(s.Ctx)
	} else {
		s.DB.GeneralSettings.UpdateOneID(1).
			SetTimeout(timeout).
			SetRandom(random).
			SetWidth(width).
			SetHeight(height).
			Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminSonarrNew(c *gin.Context) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Sonarr",
		"endpoint": "sonarr",
	})
}

func (s *Server) AdminSonarrCreate(c *gin.Context) {
	token := c.PostForm("token")
	url := c.PostForm("url")
	obj := s.DB.Sonarr.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	settings, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if settings != nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddSonarr(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminSonarrEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Sonarr.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Sonarr",
		"endpoint": "sonarr",
		"obj":      obj,
		"edit":     true,
	})
}

func (s *Server) AdminSonarrUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Sonarr.UpdateOneID(id).SetToken(c.PostForm("token")).SetURL(c.PostForm("url")).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminSonarrDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Sonarr.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminRadarrNew(c *gin.Context) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Radarr",
		"endpoint": "radarr",
	})
}

func (s *Server) AdminRadarrCreate(c *gin.Context) {
	token := c.PostForm("token")
	url := c.PostForm("url")
	obj := s.DB.Radarr.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	settings, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if settings != nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddRadarr(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminRadarrEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Radarr.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Radarr",
		"endpoint": "radarr",
		"obj":      obj,
		"edit":     true,
	})
}

func (s *Server) AdminRadarrUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Radarr.UpdateOneID(id).SetToken(c.PostForm("token")).SetURL(c.PostForm("url")).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminRadarrDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Radarr.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}
