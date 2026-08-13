package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/generalsettings"
)

// dsEntry defines CRUD operations for a token/URL datasource type.
type dsEntry struct {
	TypeName string
	Create   func(*ent.Client, context.Context, string, string) (any, error)
	Get      func(*ent.Client, context.Context, int) (any, error)
	Update   func(*ent.Client, context.Context, int, string, string) error
	Delete   func(*ent.Client, context.Context, int) error
	AddEdge  func(*ent.GeneralSettingsUpdateOne, any) *ent.GeneralSettingsUpdateOne
	// CreateFields/UpdateFields are optional field-based variants used by
	// datasources with extra form fields (display name, config JSON). When
	// nil, the plain token/url path is used. Fields map keys: token, url,
	// name, config.
	CreateFields func(*ent.Client, context.Context, map[string]string) (any, error)
	UpdateFields func(*ent.Client, context.Context, int, map[string]string) error
}

var dsRegistry map[string]*dsEntry

func init() {
	dsRegistry = map[string]*dsEntry{
		"sonarr": {
			TypeName: "Sonarr",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.Sonarr.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.Sonarr.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.Sonarr.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.Sonarr.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddSonarr(obj.(*ent.Sonarr))
			},
		},
		"radarr": {
			TypeName: "Radarr",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.Radarr.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.Radarr.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.Radarr.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.Radarr.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddRadarr(obj.(*ent.Radarr))
			},
		},
		"f1": {
			TypeName: "F1",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.F1.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.F1.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.F1.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.F1.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddF1(obj.(*ent.F1))
			},
		},
		"weather": {
			TypeName: "Weather",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.Weather.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.Weather.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.Weather.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.Weather.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddWeather(obj.(*ent.Weather))
			},
		},
		"homeassistant": {
			TypeName: "HomeAssistant",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.HomeAssistant.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.HomeAssistant.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.HomeAssistant.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error {
				return db.HomeAssistant.DeleteOneID(id).Exec(ctx)
			},
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddHomeAssistant(obj.(*ent.HomeAssistant))
			},
		},
		"untappd": {
			TypeName: "Untappd",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.Untappd.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.Untappd.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.Untappd.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.Untappd.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddUntappd(obj.(*ent.Untappd))
			},
		},
		"crypto": {
			TypeName: "Crypto",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.Crypto.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.Crypto.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.Crypto.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.Crypto.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddCrypto(obj.(*ent.Crypto))
			},
		},
		"stock": {
			TypeName: "Stock",
			Create: func(db *ent.Client, ctx context.Context, token, url string) (any, error) {
				return db.Stock.Create().SetToken(token).SetURL(url).Save(ctx)
			},
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) { return db.Stock.Get(ctx, id) },
			Update: func(db *ent.Client, ctx context.Context, id int, token, url string) error {
				return db.Stock.UpdateOneID(id).SetToken(token).SetURL(url).Exec(ctx)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error { return db.Stock.DeleteOneID(id).Exec(ctx) },
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddStocks(obj.(*ent.Stock))
			},
		},
		"googlecalendar": {
			TypeName: "Google Calendar",
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) {
				return db.GoogleCalendar.Get(ctx, id)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error {
				return db.GoogleCalendar.DeleteOneID(id).Exec(ctx)
			},
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddGoogleCalendars(obj.(*ent.GoogleCalendar))
			},
			CreateFields: func(db *ent.Client, ctx context.Context, f map[string]string) (any, error) {
				return db.GoogleCalendar.Create().SetURL(f["url"]).SetName(f["name"]).Save(ctx)
			},
			UpdateFields: func(db *ent.Client, ctx context.Context, id int, f map[string]string) error {
				return db.GoogleCalendar.UpdateOneID(id).SetURL(f["url"]).SetName(f["name"]).Exec(ctx)
			},
		},
		"newsfeed": {
			TypeName: "News",
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) {
				return db.NewsFeed.Get(ctx, id)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error {
				return db.NewsFeed.DeleteOneID(id).Exec(ctx)
			},
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddNewsFeeds(obj.(*ent.NewsFeed))
			},
			CreateFields: func(db *ent.Client, ctx context.Context, f map[string]string) (any, error) {
				return db.NewsFeed.Create().SetURL(f["url"]).SetName(f["name"]).Save(ctx)
			},
			UpdateFields: func(db *ent.Client, ctx context.Context, id int, f map[string]string) error {
				return db.NewsFeed.UpdateOneID(id).SetURL(f["url"]).SetName(f["name"]).Exec(ctx)
			},
		},
		"genericapi": {
			TypeName: "Custom API",
			Get: func(db *ent.Client, ctx context.Context, id int) (any, error) {
				return db.GenericAPI.Get(ctx, id)
			},
			Delete: func(db *ent.Client, ctx context.Context, id int) error {
				return db.GenericAPI.DeleteOneID(id).Exec(ctx)
			},
			AddEdge: func(u *ent.GeneralSettingsUpdateOne, obj any) *ent.GeneralSettingsUpdateOne {
				return u.AddGenericApis(obj.(*ent.GenericAPI))
			},
			CreateFields: func(db *ent.Client, ctx context.Context, f map[string]string) (any, error) {
				return db.GenericAPI.Create().SetToken(f["token"]).SetURL(f["url"]).SetConfig(f["config"]).Save(ctx)
			},
			UpdateFields: func(db *ent.Client, ctx context.Context, id int, f map[string]string) error {
				return db.GenericAPI.UpdateOneID(id).SetToken(f["token"]).SetURL(f["url"]).SetConfig(f["config"]).Exec(ctx)
			},
		},
	}
}

// Generic handlers using registry

func (s *Server) createTokenURLDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok {
		slog.Error("unknown datasource endpoint", "endpoint", endpoint)
		SetFlash(c, "danger", "Unknown datasource type")
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	token := c.PostForm("token")
	url := c.PostForm("url")

	v := NewValidator().Required("Token", token)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/")
		return
	}

	obj, err := entry.Create(s.DB, s.Ctx, token, url)
	if err != nil {
		slog.Error("failed to create datasource", "endpoint", endpoint, "error", err)
		c.Redirect(http.StatusFound, "/admin/")
		return
	}

	// Add edge to GeneralSettings
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if err == nil && settings != nil {
		entry.AddEdge(s.DB.GeneralSettings.UpdateOne(settings), obj).Exec(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) editTokenURLDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := entry.Get(s.DB, s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	s.renderForm(c, entry.TypeName, endpoint, true, obj)
}

func (s *Server) updateTokenURLDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	token := c.PostForm("token")
	url := c.PostForm("url")
	if err := entry.Update(s.DB, s.Ctx, id, token, url); err != nil {
		slog.Error("failed to update datasource", "endpoint", endpoint, "id", id, "error", err)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) deleteTokenURLDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if err := entry.Delete(s.DB, s.Ctx, id); err != nil {
		slog.Error("failed to delete datasource", "endpoint", endpoint, "id", id, "error", err)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func datasourceTypeName(endpoint string) string {
	if entry, ok := dsRegistry[endpoint]; ok {
		return entry.TypeName
	}
	return endpoint
}

// formFields collects the standard datasource form fields into a map for the
// field-based CreateFields/UpdateFields registry variants.
func formFields(c *gin.Context) map[string]string {
	return map[string]string{
		"token":  c.PostForm("token"),
		"url":    c.PostForm("url"),
		"name":   c.PostForm("name"),
		"config": c.PostForm("config"),
	}
}

// createFieldDS creates a datasource through its field-based registry entry
// (name/url or token/url/config forms) and wires it into GeneralSettings.
func (s *Server) createFieldDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok || entry.CreateFields == nil {
		SetFlash(c, "danger", "Unknown datasource type")
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	if endpoint == "genericapi" && c.PostForm("url") == "" {
		SetFlash(c, "danger", "URL is required")
		c.Redirect(http.StatusFound, "/admin/datasources/"+endpoint+"/new")
		return
	}
	obj, err := entry.CreateFields(s.DB, s.Ctx, formFields(c))
	if err != nil {
		slog.Error("failed to create datasource", "endpoint", endpoint, "error", err)
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	if err == nil && settings != nil {
		entry.AddEdge(s.DB.GeneralSettings.UpdateOne(settings), obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", datasourceTypeName(endpoint)+" created")
	c.Redirect(http.StatusFound, "/admin/")
}

// editFieldDS renders the form for a field-based datasource.
func (s *Server) editFieldDS(c *gin.Context, endpoint string, extra gin.H) {
	entry, ok := dsRegistry[endpoint]
	if !ok {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := entry.Get(s.DB, s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	data := gin.H{
		"type":     entry.TypeName,
		"endpoint": endpoint,
		"obj":      obj,
		"edit":     true,
	}
	if entry.CreateFields != nil {
		data["has_name"] = endpoint != "genericapi"
		data["has_config"] = endpoint == "genericapi"
	}
	for k, v := range extra {
		data[k] = v
	}
	s.renderPage(c, http.StatusOK, "datasource_form.html", data)
}

// updateFieldDS updates a datasource through its field-based registry entry.
func (s *Server) updateFieldDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok || entry.UpdateFields == nil {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if err := entry.UpdateFields(s.DB, s.Ctx, id, formFields(c)); err != nil {
		slog.Error("failed to update datasource", "endpoint", endpoint, "id", id, "error", err)
		SetFlash(c, "danger", "Failed to update: "+err.Error())
	}
	SetFlash(c, "success", datasourceTypeName(endpoint)+" updated")
	c.Redirect(http.StatusFound, "/admin/")
}

// deleteFieldDS deletes a datasource through its registry entry.
func (s *Server) deleteFieldDS(c *gin.Context, endpoint string) {
	entry, ok := dsRegistry[endpoint]
	if !ok {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if err := entry.Delete(s.DB, s.Ctx, id); err != nil {
		slog.Error("failed to delete datasource", "endpoint", endpoint, "id", id, "error", err)
	}
	SetFlash(c, "success", datasourceTypeName(endpoint)+" deleted")
	c.Redirect(http.StatusFound, "/admin/")
}
