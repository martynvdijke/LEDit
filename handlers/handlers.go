package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/generalsettings"
)

func (s *Server) IndexHandler(c *gin.Context) {
	umamiSettings, _ := s.DB.UmamiSettings.Query().Only(s.Ctx)
	umamiEnabled := false
	umamiEndpoint := ""
	umamiWebsiteID := ""
	if umamiSettings != nil && umamiSettings.Enable {
		umamiEnabled = true
		umamiEndpoint = umamiSettings.Endpoint
		umamiWebsiteID = umamiSettings.WebsiteID
	}
	c.HTML(http.StatusOK, "index.html", gin.H{
		"umamiEnabled":   umamiEnabled,
		"umamiEndpoint":  umamiEndpoint,
		"umamiWebsiteID": umamiWebsiteID,
	})
}

func (s *Server) AdminDashboard(c *gin.Context) {
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().WithImages().WithVideos().WithCrypto().WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().WithEmailSettings().WithAiSettings().Only(s.Ctx)

	stats := gin.H{
		"has_settings": err == nil,
	}
	if err == nil {
		sonarrItems, _ := settings.Edges.SonarrOrErr()
		radarrItems, _ := settings.Edges.RadarrOrErr()
		f1Items, _ := settings.Edges.F1OrErr()
		weatherItems, _ := settings.Edges.WeatherOrErr()
		haItems, _ := settings.Edges.HomeAssistantOrErr()
		untappdItems, _ := settings.Edges.UntappdOrErr()
		imageItems, _ := settings.Edges.ImagesOrErr()
		videoItems, _ := settings.Edges.VideosOrErr()
		cryptoItems, _ := settings.Edges.CryptoOrErr()
		rssItems, _ := settings.Edges.RssFeedsOrErr()
		calendarItems, _ := settings.Edges.CalendarsOrErr()
		stockItems, _ := settings.Edges.StocksOrErr()
		textSlideItems, _ := settings.Edges.TextSlidesOrErr()

		type sourceEntry struct {
			ID       int
			Type     string
			Endpoint string
			Token    string
			URL      string
			Name     string
			Path     string
			Content  string
			Color    string
		}
		var sources []sourceEntry
		for _, s := range sonarrItems {
			sources = append(sources, sourceEntry{ID: s.ID, Type: "Sonarr", Endpoint: "sonarr", Token: s.Token, URL: s.URL})
		}
		for _, r := range radarrItems {
			sources = append(sources, sourceEntry{ID: r.ID, Type: "Radarr", Endpoint: "radarr", Token: r.Token, URL: r.URL})
		}
		for _, f := range f1Items {
			sources = append(sources, sourceEntry{ID: f.ID, Type: "F1", Endpoint: "f1", Token: f.Token, URL: f.URL})
		}
		for _, w := range weatherItems {
			sources = append(sources, sourceEntry{ID: w.ID, Type: "Weather", Endpoint: "weather", Token: w.Token, URL: w.URL})
		}
		for _, h := range haItems {
			sources = append(sources, sourceEntry{ID: h.ID, Type: "HomeAssistant", Endpoint: "homeassistant", Token: h.Token, URL: h.URL})
		}
		for _, u := range untappdItems {
			sources = append(sources, sourceEntry{ID: u.ID, Type: "Untappd", Endpoint: "untappd", Token: u.Token, URL: u.URL})
		}
		for _, img := range imageItems {
			sources = append(sources, sourceEntry{ID: img.ID, Type: "Image", Endpoint: "images", Path: img.Path})
		}
		for _, vid := range videoItems {
			sources = append(sources, sourceEntry{ID: vid.ID, Type: "Video", Endpoint: "videos", Path: vid.Path})
		}
		for _, cr := range cryptoItems {
			sources = append(sources, sourceEntry{ID: cr.ID, Type: "Crypto", Endpoint: "crypto", Token: cr.Token, URL: cr.URL})
		}
		for _, rs := range rssItems {
			sources = append(sources, sourceEntry{ID: rs.ID, Type: "RSS Feed", Endpoint: "rssfeed", URL: rs.URL, Name: rs.Name})
		}
		for _, cl := range calendarItems {
			sources = append(sources, sourceEntry{ID: cl.ID, Type: "Calendar", Endpoint: "calendar", URL: cl.URL, Name: cl.Name})
		}
		for _, st := range stockItems {
			sources = append(sources, sourceEntry{ID: st.ID, Type: "Stock", Endpoint: "stock", Token: st.Token, URL: st.URL})
		}
		for _, ts := range textSlideItems {
			sources = append(sources, sourceEntry{ID: ts.ID, Type: "Text Slide", Endpoint: "textslides", Content: ts.Content, Color: ts.Color})
		}

		stats = gin.H{
			"has_settings":    true,
			"settings":        settings,
			"sources":         sources,
			"sonarr_count":    len(sonarrItems),
			"radarr_count":    len(radarrItems),
			"f1_count":        len(f1Items),
			"weather_count":   len(weatherItems),
			"ha_count":        len(haItems),
			"untappd_count":   len(untappdItems),
			"image_count":     len(imageItems),
			"video_count":     len(videoItems),
			"crypto_count":    len(cryptoItems),
			"rssfeed_count":   len(rssItems),
			"calendar_count":  len(calendarItems),
			"stock_count":     len(stockItems),
			"textslide_count": len(textSlideItems),
			"total_sources":   len(sonarrItems) + len(radarrItems) + len(f1Items) + len(weatherItems) + len(haItems) + len(untappdItems) + len(imageItems) + len(videoItems) + len(cryptoItems) + len(rssItems) + len(calendarItems) + len(stockItems) + len(textSlideItems),
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
		"settings":    settings,
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

// ---------------------------------------------------------------------------
// Generic helpers for token/url datasources
// ---------------------------------------------------------------------------

func (s *Server) renderForm(c *gin.Context, dsType, endpoint string, edit bool, obj any) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     dsType,
		"endpoint": endpoint,
		"obj":      obj,
		"edit":     edit,
	})
}

func (s *Server) createTokenURLDS(c *gin.Context, endpoint string) {
	token := c.PostForm("token")
	url := c.PostForm("url")

	var obj any
	switch endpoint {
	case "sonarr":
		obj = s.DB.Sonarr.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "radarr":
		obj = s.DB.Radarr.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "f1":
		obj = s.DB.F1.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "weather":
		obj = s.DB.Weather.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "homeassistant":
		obj = s.DB.HomeAssistant.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "untappd":
		obj = s.DB.Untappd.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "crypto":
		obj = s.DB.Crypto.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	case "stock":
		obj = s.DB.Stock.Create().SetToken(token).SetURL(url).SaveX(s.Ctx)
	}

	addEdge(endpoint, s, obj)
	c.Redirect(http.StatusFound, "/admin/")
}

func addEdge(endpoint string, s *Server, obj any) {
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if err != nil || settings == nil {
		return
	}
	upd := s.DB.GeneralSettings.UpdateOne(settings)
	switch endpoint {
	case "sonarr":
		upd.AddSonarr(obj.(*ent.Sonarr))
	case "radarr":
		upd.AddRadarr(obj.(*ent.Radarr))
	case "f1":
		upd.AddF1(obj.(*ent.F1))
	case "weather":
		upd.AddWeather(obj.(*ent.Weather))
	case "homeassistant":
		upd.AddHomeAssistant(obj.(*ent.HomeAssistant))
	case "untappd":
		upd.AddUntappd(obj.(*ent.Untappd))
	case "crypto":
		upd.AddCrypto(obj.(*ent.Crypto))
	case "stock":
		upd.AddStocks(obj.(*ent.Stock))
	}
	upd.Exec(s.Ctx)
}

func (s *Server) editTokenURLDS(c *gin.Context, endpoint string) {
	id, _ := strconv.Atoi(c.Param("id"))
	dsType := datasourceTypeName(endpoint)
	var obj any
	var err error
	switch endpoint {
	case "sonarr":
		obj, err = s.DB.Sonarr.Get(s.Ctx, id)
	case "radarr":
		obj, err = s.DB.Radarr.Get(s.Ctx, id)
	case "f1":
		obj, err = s.DB.F1.Get(s.Ctx, id)
	case "weather":
		obj, err = s.DB.Weather.Get(s.Ctx, id)
	case "homeassistant":
		obj, err = s.DB.HomeAssistant.Get(s.Ctx, id)
	case "untappd":
		obj, err = s.DB.Untappd.Get(s.Ctx, id)
	case "crypto":
		obj, err = s.DB.Crypto.Get(s.Ctx, id)
	case "stock":
		obj, err = s.DB.Stock.Get(s.Ctx, id)
	}
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	s.renderForm(c, dsType, endpoint, true, obj)
}

func (s *Server) updateTokenURLDS(c *gin.Context, endpoint string) {
	id, _ := strconv.Atoi(c.Param("id"))
	token := c.PostForm("token")
	url := c.PostForm("url")
	switch endpoint {
	case "sonarr":
		s.DB.Sonarr.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "radarr":
		s.DB.Radarr.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "f1":
		s.DB.F1.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "weather":
		s.DB.Weather.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "homeassistant":
		s.DB.HomeAssistant.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "untappd":
		s.DB.Untappd.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "crypto":
		s.DB.Crypto.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	case "stock":
		s.DB.Stock.UpdateOneID(id).SetToken(token).SetURL(url).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) deleteTokenURLDS(c *gin.Context, endpoint string) {
	id, _ := strconv.Atoi(c.Param("id"))
	switch endpoint {
	case "sonarr":
		s.DB.Sonarr.DeleteOneID(id).Exec(s.Ctx)
	case "radarr":
		s.DB.Radarr.DeleteOneID(id).Exec(s.Ctx)
	case "f1":
		s.DB.F1.DeleteOneID(id).Exec(s.Ctx)
	case "weather":
		s.DB.Weather.DeleteOneID(id).Exec(s.Ctx)
	case "homeassistant":
		s.DB.HomeAssistant.DeleteOneID(id).Exec(s.Ctx)
	case "untappd":
		s.DB.Untappd.DeleteOneID(id).Exec(s.Ctx)
	case "crypto":
		s.DB.Crypto.DeleteOneID(id).Exec(s.Ctx)
	case "stock":
		s.DB.Stock.DeleteOneID(id).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func datasourceTypeName(endpoint string) string {
	names := map[string]string{
		"sonarr":        "Sonarr",
		"radarr":        "Radarr",
		"f1":            "F1",
		"weather":       "Weather",
		"homeassistant": "HomeAssistant",
		"untappd":       "Untappd",
		"crypto":        "Crypto",
		"stock":         "Stock",
	}
	if n, ok := names[endpoint]; ok {
		return n
	}
	return endpoint
}

// ---------------------------------------------------------------------------
// Sonarr
// ---------------------------------------------------------------------------

func (s *Server) AdminSonarrNew(c *gin.Context) {
	s.renderForm(c, "Sonarr", "sonarr", false, nil)
}
func (s *Server) AdminSonarrCreate(c *gin.Context) { s.createTokenURLDS(c, "sonarr") }
func (s *Server) AdminSonarrEdit(c *gin.Context)   { s.editTokenURLDS(c, "sonarr") }
func (s *Server) AdminSonarrUpdate(c *gin.Context) { s.updateTokenURLDS(c, "sonarr") }
func (s *Server) AdminSonarrDelete(c *gin.Context) { s.deleteTokenURLDS(c, "sonarr") }

// ---------------------------------------------------------------------------
// Radarr
// ---------------------------------------------------------------------------

func (s *Server) AdminRadarrNew(c *gin.Context) {
	s.renderForm(c, "Radarr", "radarr", false, nil)
}
func (s *Server) AdminRadarrCreate(c *gin.Context) { s.createTokenURLDS(c, "radarr") }
func (s *Server) AdminRadarrEdit(c *gin.Context)   { s.editTokenURLDS(c, "radarr") }
func (s *Server) AdminRadarrUpdate(c *gin.Context) { s.updateTokenURLDS(c, "radarr") }
func (s *Server) AdminRadarrDelete(c *gin.Context) { s.deleteTokenURLDS(c, "radarr") }

// ---------------------------------------------------------------------------
// F1
// ---------------------------------------------------------------------------

func (s *Server) AdminF1New(c *gin.Context)    { s.renderForm(c, "F1", "f1", false, nil) }
func (s *Server) AdminF1Create(c *gin.Context) { s.createTokenURLDS(c, "f1") }
func (s *Server) AdminF1Edit(c *gin.Context)   { s.editTokenURLDS(c, "f1") }
func (s *Server) AdminF1Update(c *gin.Context) { s.updateTokenURLDS(c, "f1") }
func (s *Server) AdminF1Delete(c *gin.Context) { s.deleteTokenURLDS(c, "f1") }

// ---------------------------------------------------------------------------
// Weather
// ---------------------------------------------------------------------------

func (s *Server) AdminWeatherNew(c *gin.Context)    { s.renderForm(c, "Weather", "weather", false, nil) }
func (s *Server) AdminWeatherCreate(c *gin.Context) { s.createTokenURLDS(c, "weather") }
func (s *Server) AdminWeatherEdit(c *gin.Context)   { s.editTokenURLDS(c, "weather") }
func (s *Server) AdminWeatherUpdate(c *gin.Context) { s.updateTokenURLDS(c, "weather") }
func (s *Server) AdminWeatherDelete(c *gin.Context) { s.deleteTokenURLDS(c, "weather") }

// ---------------------------------------------------------------------------
// HomeAssistant
// ---------------------------------------------------------------------------

func (s *Server) AdminHomeAssistantNew(c *gin.Context) {
	s.renderForm(c, "HomeAssistant", "homeassistant", false, nil)
}
func (s *Server) AdminHomeAssistantCreate(c *gin.Context) { s.createTokenURLDS(c, "homeassistant") }
func (s *Server) AdminHomeAssistantEdit(c *gin.Context)   { s.editTokenURLDS(c, "homeassistant") }
func (s *Server) AdminHomeAssistantUpdate(c *gin.Context) { s.updateTokenURLDS(c, "homeassistant") }
func (s *Server) AdminHomeAssistantDelete(c *gin.Context) { s.deleteTokenURLDS(c, "homeassistant") }

// ---------------------------------------------------------------------------
// Untappd
// ---------------------------------------------------------------------------

func (s *Server) AdminUntappdNew(c *gin.Context)    { s.renderForm(c, "Untappd", "untappd", false, nil) }
func (s *Server) AdminUntappdCreate(c *gin.Context) { s.createTokenURLDS(c, "untappd") }
func (s *Server) AdminUntappdEdit(c *gin.Context)   { s.editTokenURLDS(c, "untappd") }
func (s *Server) AdminUntappdUpdate(c *gin.Context) { s.updateTokenURLDS(c, "untappd") }
func (s *Server) AdminUntappdDelete(c *gin.Context) { s.deleteTokenURLDS(c, "untappd") }

// ---------------------------------------------------------------------------
// Crypto
// ---------------------------------------------------------------------------

func (s *Server) AdminCryptoNew(c *gin.Context)    { s.renderForm(c, "Crypto", "crypto", false, nil) }
func (s *Server) AdminCryptoCreate(c *gin.Context) { s.createTokenURLDS(c, "crypto") }
func (s *Server) AdminCryptoEdit(c *gin.Context)   { s.editTokenURLDS(c, "crypto") }
func (s *Server) AdminCryptoUpdate(c *gin.Context) { s.updateTokenURLDS(c, "crypto") }
func (s *Server) AdminCryptoDelete(c *gin.Context) { s.deleteTokenURLDS(c, "crypto") }

// ---------------------------------------------------------------------------
// Stock
// ---------------------------------------------------------------------------

func (s *Server) AdminStockNew(c *gin.Context)    { s.renderForm(c, "Stock", "stock", false, nil) }
func (s *Server) AdminStockCreate(c *gin.Context) { s.createTokenURLDS(c, "stock") }
func (s *Server) AdminStockEdit(c *gin.Context)   { s.editTokenURLDS(c, "stock") }
func (s *Server) AdminStockUpdate(c *gin.Context) { s.updateTokenURLDS(c, "stock") }
func (s *Server) AdminStockDelete(c *gin.Context) { s.deleteTokenURLDS(c, "stock") }

// ---------------------------------------------------------------------------
// DeviceSettings (Phase 7)
// ---------------------------------------------------------------------------

func (s *Server) AdminDeviceSettingsList(c *gin.Context) {
	settings, err := s.DB.GeneralSettings.Query().WithDeviceSettings().Only(s.Ctx)
	if err != nil {
		c.HTML(http.StatusOK, "devices.html", gin.H{"devices": []any{}})
		return
	}
	devices, _ := settings.Edges.DeviceSettingsOrErr()
	c.HTML(http.StatusOK, "devices.html", gin.H{"devices": devices})
}

func (s *Server) AdminDeviceSettingsNew(c *gin.Context) {
	c.HTML(http.StatusOK, "device_form.html", gin.H{})
}

func (s *Server) AdminDeviceSettingsCreate(c *gin.Context) {
	name := c.PostForm("name")
	ip := c.PostForm("ip")
	port, _ := strconv.Atoi(c.PostForm("port"))
	if port == 0 {
		port = 6270
	}
	username := c.PostForm("username")
	password := c.PostForm("password")
	width, _ := strconv.Atoi(c.PostForm("width"))
	if width == 0 {
		width = 64
	}
	height, _ := strconv.Atoi(c.PostForm("height"))
	if height == 0 {
		height = 64
	}
	enabled := c.PostForm("enabled") == "on"

	obj := s.DB.DeviceSettings.Create().
		SetName(name).SetIP(ip).SetPort(port).
		SetUsername(username).SetPassword(password).
		SetWidth(width).SetHeight(height).SetEnabled(enabled).
		SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddDeviceSettings(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/devices")
}

func (s *Server) AdminDeviceSettingsEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.DeviceSettings.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/devices")
		return
	}
	c.HTML(http.StatusOK, "device_form.html", gin.H{"obj": obj, "edit": true})
}

func (s *Server) AdminDeviceSettingsUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	ip := c.PostForm("ip")
	port, _ := strconv.Atoi(c.PostForm("port"))
	username := c.PostForm("username")
	password := c.PostForm("password")
	width, _ := strconv.Atoi(c.PostForm("width"))
	height, _ := strconv.Atoi(c.PostForm("height"))
	enabled := c.PostForm("enabled") == "on"
	s.DB.DeviceSettings.UpdateOneID(id).
		SetName(name).SetIP(ip).SetPort(port).
		SetUsername(username).SetPassword(password).
		SetWidth(width).SetHeight(height).SetEnabled(enabled).
		Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/devices")
}

func (s *Server) AdminDeviceSettingsDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.DeviceSettings.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/devices")
}

// ---------------------------------------------------------------------------
// Custom Theme (Phase 8)
// ---------------------------------------------------------------------------

func (s *Server) AdminThemeEditor(c *gin.Context) {
	settings, _ := s.DB.GeneralSettings.Query().Only(s.Ctx)
	theme := map[string]any{
		"bg_color":     "#282a36",
		"accent_color": "#50fa7b",
		"text_color":   "#8be9fd",
		"title":        "CUSTOM",
		"font_size":    24,
	}
	if settings != nil {
		c.HTML(http.StatusOK, "theme_editor.html", gin.H{"theme": theme, "has_settings": true})
		return
	}
	c.HTML(http.StatusOK, "theme_editor.html", gin.H{"theme": theme})
}

func (s *Server) AdminThemeSave(c *gin.Context) {
	// Save theme preferences to GeneralSettings as JSON annotation or dedicated fields
	// For now, store in settings annotations
	c.Redirect(http.StatusFound, "/admin/")
}

// ---------------------------------------------------------------------------
// Analytics (Phase 10)
// ---------------------------------------------------------------------------

func (s *Server) AdminAnalytics(c *gin.Context) {
	stats := GetAnalytics()
	c.HTML(http.StatusOK, "analytics.html", gin.H{"stats": stats})
}

// ---------------------------------------------------------------------------
// Umami Analytics Settings
// ---------------------------------------------------------------------------

func (s *Server) AdminUmamiSettings(c *gin.Context) {
	settings, err := s.DB.UmamiSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	c.HTML(http.StatusOK, "umami_settings.html", gin.H{
		"settings":    settings,
		"hasSettings": settings != nil,
	})
}

func (s *Server) AdminUmamiSettingsSave(c *gin.Context) {
	endpoint := c.PostForm("endpoint")
	websiteID := c.PostForm("website_id")
	enable := c.PostForm("enable") == "on"

	exists, _ := s.DB.UmamiSettings.Query().Exist(s.Ctx)
	if !exists {
		_, err := s.DB.UmamiSettings.Create().
			SetEndpoint(endpoint).
			SetWebsiteID(websiteID).
			SetEnable(enable).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to create umami settings", "error", err)
		}
	} else {
		_, err := s.DB.UmamiSettings.Update().
			SetEndpoint(endpoint).
			SetWebsiteID(websiteID).
			SetEnable(enable).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to update umami settings", "error", err)
		}
	}

	c.Redirect(http.StatusFound, "/admin/settings/umami")
}

// ---------------------------------------------------------------------------
// Schedules
// ---------------------------------------------------------------------------

func (s *Server) AdminScheduleList(c *gin.Context) {
	settings, err := s.DB.GeneralSettings.Query().WithSchedules().Only(s.Ctx)
	if err != nil {
		c.HTML(http.StatusOK, "schedules.html", gin.H{"schedules": []any{}})
		return
	}
	schedules, _ := settings.Edges.SchedulesOrErr()
	c.HTML(http.StatusOK, "schedules.html", gin.H{"schedules": schedules})
}

func (s *Server) AdminScheduleNew(c *gin.Context) {
	c.HTML(http.StatusOK, "schedule_form.html", gin.H{})
}

func (s *Server) AdminScheduleCreate(c *gin.Context) {
	name := c.PostForm("name")
	cron := c.PostForm("cron")
	enabled := c.PostForm("enabled") == "on"
	obj := s.DB.Schedule.Create().SetName(name).SetCron(cron).SetEnabled(enabled).SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddSchedules(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/schedules")
}

func (s *Server) AdminScheduleEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Schedule.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/schedules")
		return
	}
	c.HTML(http.StatusOK, "schedule_form.html", gin.H{"obj": obj, "edit": true})
}

func (s *Server) AdminScheduleUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	cron := c.PostForm("cron")
	enabled := c.PostForm("enabled") == "on"
	s.DB.Schedule.UpdateOneID(id).SetName(name).SetCron(cron).SetEnabled(enabled).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/schedules")
}

func (s *Server) AdminScheduleDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Schedule.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/schedules")
}

// ---------------------------------------------------------------------------
// Image (file upload)
// ---------------------------------------------------------------------------

func (s *Server) AdminImageNew(c *gin.Context) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":       "Image",
		"endpoint":   "images",
		"is_media":   true,
		"extensions": ".png,.jpg,.jpeg,.gif",
	})
}

func (s *Server) AdminImageCreate(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, "File upload required")
		return
	}
	path := filepath.Join("web", "media", "custom_images", file.Filename)
	if err := c.SaveUploadedFile(file, path); err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Failed to save file: %v", err))
		return
	}
	obj := s.DB.Image.Create().SetPath(path).SaveX(s.Ctx)
	settings, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if settings != nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddImages(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminImageEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Image.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":       "Image",
		"endpoint":   "images",
		"obj":        obj,
		"edit":       true,
		"is_media":   true,
		"extensions": ".png,.jpg,.jpeg,.gif",
	})
}

func (s *Server) AdminImageUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	file, err := c.FormFile("file")
	if err == nil {
		path := filepath.Join("web", "media", "custom_images", file.Filename)
		if err := c.SaveUploadedFile(file, path); err == nil {
			s.DB.Image.UpdateOneID(id).SetPath(path).Exec(s.Ctx)
		}
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminImageDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Image.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

// ---------------------------------------------------------------------------
// Video (file upload)
// ---------------------------------------------------------------------------

func (s *Server) AdminVideoNew(c *gin.Context) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":       "Video",
		"endpoint":   "videos",
		"is_media":   true,
		"extensions": ".mp4,.webm,.avi",
	})
}

func (s *Server) AdminVideoCreate(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, "File upload required")
		return
	}
	path := filepath.Join("web", "media", "custom_videos", file.Filename)
	if err := c.SaveUploadedFile(file, path); err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Failed to save file: %v", err))
		return
	}
	obj := s.DB.Video.Create().SetPath(path).SaveX(s.Ctx)
	settings, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if settings != nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddVideos(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminVideoEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Video.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":       "Video",
		"endpoint":   "videos",
		"obj":        obj,
		"edit":       true,
		"is_media":   true,
		"extensions": ".mp4,.webm,.avi",
	})
}

func (s *Server) AdminVideoUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	file, err := c.FormFile("file")
	if err == nil {
		path := filepath.Join("web", "media", "custom_videos", file.Filename)
		if err := c.SaveUploadedFile(file, path); err == nil {
			s.DB.Video.UpdateOneID(id).SetPath(path).Exec(s.Ctx)
		}
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminVideoDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Video.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

// ---------------------------------------------------------------------------
// RSS Feed
// ---------------------------------------------------------------------------

func (s *Server) AdminRssFeedNew(c *gin.Context) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "RSS Feed",
		"endpoint": "rssfeed",
		"has_name": true,
	})
}

func (s *Server) AdminRssFeedCreate(c *gin.Context) {
	url := c.PostForm("url")
	name := c.PostForm("name")
	obj := s.DB.RssFeed.Create().SetURL(url).SetName(name).SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddRssFeeds(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminRssFeedEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.RssFeed.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "RSS Feed",
		"endpoint": "rssfeed",
		"obj":      obj,
		"edit":     true,
		"has_name": true,
	})
}

func (s *Server) AdminRssFeedUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	url := c.PostForm("url")
	name := c.PostForm("name")
	s.DB.RssFeed.UpdateOneID(id).SetURL(url).SetName(name).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminRssFeedDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.RssFeed.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

// ---------------------------------------------------------------------------
// Calendar
// ---------------------------------------------------------------------------

func (s *Server) AdminCalendarNew(c *gin.Context) {
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Calendar",
		"endpoint": "calendar",
		"has_name": true,
	})
}

func (s *Server) AdminCalendarCreate(c *gin.Context) {
	url := c.PostForm("url")
	name := c.PostForm("name")
	obj := s.DB.Calendar.Create().SetURL(url).SetName(name).SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddCalendars(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminCalendarEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Calendar.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Calendar",
		"endpoint": "calendar",
		"obj":      obj,
		"edit":     true,
		"has_name": true,
	})
}

func (s *Server) AdminCalendarUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	url := c.PostForm("url")
	name := c.PostForm("name")
	s.DB.Calendar.UpdateOneID(id).SetURL(url).SetName(name).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminCalendarDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Calendar.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminTextSlideNew(c *gin.Context) {
	c.HTML(http.StatusOK, "textslide_form.html", gin.H{})
}

func (s *Server) AdminTextSlideCreate(c *gin.Context) {
	content := c.PostForm("content")
	color := c.PostForm("color")
	bgColor := c.PostForm("bg_color")
	fontSize, _ := strconv.Atoi(c.DefaultPostForm("font_size", "32"))
	obj := s.DB.TextSlide.Create().SetContent(content).SetColor(color).SetBgColor(bgColor).SetFontSize(fontSize).SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddTextSlides(obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminTextSlideEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.TextSlide.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "textslide_form.html", gin.H{
		"obj":  obj,
		"edit": true,
	})
}

func (s *Server) AdminTextSlideUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	content := c.PostForm("content")
	color := c.PostForm("color")
	bgColor := c.PostForm("bg_color")
	fontSize, _ := strconv.Atoi(c.DefaultPostForm("font_size", "32"))
	s.DB.TextSlide.UpdateOneID(id).SetContent(content).SetColor(color).SetBgColor(bgColor).SetFontSize(fontSize).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminTextSlideDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.TextSlide.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}
