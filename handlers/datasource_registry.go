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
