package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
	"ledit/ent/playlist"
)

func (s *Server) AdminPlaylistList(c *gin.Context) {
	rows, err := s.DB.Playlist.Query().Order(ent.Asc(playlist.FieldName)).All(s.Ctx)
	if err != nil {
		rows = []*ent.Playlist{}
	}
	s.renderPage(c, http.StatusOK, "playlists.html", gin.H{"playlists": rows})
}

func (s *Server) AdminPlaylistNew(c *gin.Context) {
	opts := s.bindingOptions(c)
	s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
		"options":      opts,
		"options_json": bindingOptionsJSON(opts),
	})
}

func (s *Server) AdminPlaylistCreate(c *gin.Context) {
	name := c.PostForm("name")
	items := c.PostForm("items")
	if items == "" {
		items = "[]"
	}
	enabled := c.PostForm("enabled") == "on"

	if name == "" {
		SetFlash(c, "danger", "name is required")
		opts := s.bindingOptions(c)
		s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
			"obj":          map[string]string{"name": name, "items": items, "enabled": c.PostForm("enabled")},
			"error":        "name is required",
			"options":      opts,
			"options_json": bindingOptionsJSON(opts),
		})
		return
	}
	if _, err := datasource.ParsePlaylistItems(items); err != nil {
		SetFlash(c, "danger", err.Error())
		opts := s.bindingOptions(c)
		s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
			"obj":          map[string]string{"name": name, "items": items, "enabled": c.PostForm("enabled")},
			"error":        err.Error(),
			"options":      opts,
			"options_json": bindingOptionsJSON(opts),
		})
		return
	}

	obj, err := s.DB.Playlist.Create().SetName(name).SetItems(items).SetEnabled(enabled).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		opts := s.bindingOptions(c)
		s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
			"obj":          map[string]string{"name": name, "items": items, "enabled": c.PostForm("enabled")},
			"options":      opts,
			"options_json": bindingOptionsJSON(opts),
		})
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddPlaylists(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Playlist created")
	c.Redirect(http.StatusFound, "/admin/playlists")
}

func (s *Server) AdminPlaylistEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Playlist.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Playlist not found")
		c.Redirect(http.StatusFound, "/admin/playlists")
		return
	}
	opts := s.bindingOptions(c)
	s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
		"obj":          obj,
		"edit":         true,
		"options":      opts,
		"options_json": bindingOptionsJSON(opts),
	})
}

func (s *Server) AdminPlaylistUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	items := c.PostForm("items")
	if items == "" {
		items = "[]"
	}
	enabled := c.PostForm("enabled") == "on"

	if name == "" {
		SetFlash(c, "danger", "name is required")
		opts := s.bindingOptions(c)
		s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
			"obj":          map[string]string{"name": name, "items": items, "enabled": c.PostForm("enabled")},
			"edit":         true,
			"id":           id,
			"error":        "name is required",
			"options":      opts,
			"options_json": bindingOptionsJSON(opts),
		})
		return
	}
	if _, err := datasource.ParsePlaylistItems(items); err != nil {
		SetFlash(c, "danger", err.Error())
		opts := s.bindingOptions(c)
		s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
			"obj":          map[string]string{"name": name, "items": items, "enabled": c.PostForm("enabled")},
			"edit":         true,
			"id":           id,
			"error":        err.Error(),
			"options":      opts,
			"options_json": bindingOptionsJSON(opts),
		})
		return
	}

	if err := s.DB.Playlist.UpdateOneID(id).SetName(name).SetItems(items).SetEnabled(enabled).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		opts := s.bindingOptions(c)
		s.renderPage(c, http.StatusOK, "playlist_form.html", gin.H{
			"obj":          map[string]string{"name": name, "items": items, "enabled": c.PostForm("enabled")},
			"edit":         true,
			"options":      opts,
			"options_json": bindingOptionsJSON(opts),
		})
		return
	}
	SetFlash(c, "success", "Playlist updated")
	c.Redirect(http.StatusFound, "/admin/playlists")
}

func (s *Server) AdminPlaylistDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Playlist.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/playlists")
		return
	}
	SetFlash(c, "success", "Playlist deleted")
	c.Redirect(http.StatusFound, "/admin/playlists")
}
