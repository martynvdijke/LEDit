package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/nowplayingsource"
)

// nowPlayingInput is the validated shape shared by the admin form and API.
type nowPlayingInput struct {
	Name         string
	Provider     string
	URL          string
	Token        string
	Username     string
	ShowAlbumArt bool
}

func (in *nowPlayingInput) validate() error {
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 64 {
		return fmt.Errorf("name must be 1-64 characters")
	}
	switch in.Provider {
	case "spotify", "plex", "jellyfin":
	default:
		return fmt.Errorf("provider must be spotify, plex or jellyfin")
	}
	if in.Provider != "spotify" && strings.TrimSpace(in.URL) == "" {
		return fmt.Errorf("url is required for %s", in.Provider)
	}
	if utf8.RuneCountInString(in.Username) > 64 {
		return fmt.Errorf("username must be 0-64 characters")
	}
	return nil
}

func nowPlayingFromForm(c *gin.Context) nowPlayingInput {
	show := c.PostForm("show_album_art")
	return nowPlayingInput{
		Name:         c.PostForm("name"),
		Provider:     c.DefaultPostForm("provider", "jellyfin"),
		URL:          c.PostForm("url"),
		Token:        c.PostForm("token"),
		Username:     c.PostForm("username"),
		ShowAlbumArt: show == "on" || show == "true" || show == "1",
	}
}

func (s *Server) AdminNowPlayingList(c *gin.Context) {
	items, err := s.DB.NowPlayingSource.Query().Order(ent.Asc(nowplayingsource.FieldID)).All(s.Ctx)
	if err != nil {
		items = []*ent.NowPlayingSource{}
	}
	s.renderPage(c, http.StatusOK, "now_playing.html", gin.H{"sources": items})
}

func (s *Server) AdminNowPlayingNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "now_playing_form.html", gin.H{})
}

func (s *Server) AdminNowPlayingCreate(c *gin.Context) {
	in := nowPlayingFromForm(c)
	if err := in.validate(); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusOK, "now_playing_form.html", gin.H{"obj": in, "error": err.Error()})
		return
	}
	obj, err := s.DB.NowPlayingSource.Create().
		SetName(in.Name).SetProvider(nowplayingsource.Provider(in.Provider)).
		SetURL(in.URL).SetToken(in.Token).SetUsername(in.Username).SetShowAlbumArt(in.ShowAlbumArt).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		s.renderPage(c, http.StatusOK, "now_playing_form.html", gin.H{"obj": in, "error": err.Error()})
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddNowPlayingSources(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Now Playing source created")
	c.Redirect(http.StatusFound, "/admin/nowplaying")
}

func (s *Server) AdminNowPlayingEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.NowPlayingSource.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Now Playing source not found")
		c.Redirect(http.StatusFound, "/admin/nowplaying")
		return
	}
	s.renderPage(c, http.StatusOK, "now_playing_form.html", gin.H{"obj": obj, "edit": true, "id": id})
}

func (s *Server) AdminNowPlayingUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	in := nowPlayingFromForm(c)
	if err := in.validate(); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusOK, "now_playing_form.html", gin.H{"obj": in, "edit": true, "id": id, "error": err.Error()})
		return
	}
	if in.Token == "" {
		if existing, err := s.DB.NowPlayingSource.Get(s.Ctx, id); err == nil {
			in.Token = existing.Token
		}
	}
	err := s.DB.NowPlayingSource.UpdateOneID(id).
		SetName(in.Name).SetProvider(nowplayingsource.Provider(in.Provider)).
		SetURL(in.URL).SetToken(in.Token).SetUsername(in.Username).SetShowAlbumArt(in.ShowAlbumArt).
		Exec(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		s.renderPage(c, http.StatusOK, "now_playing_form.html", gin.H{"obj": in, "edit": true, "id": id, "error": err.Error()})
		return
	}
	SetFlash(c, "success", "Now Playing source updated")
	c.Redirect(http.StatusFound, "/admin/nowplaying")
}

func (s *Server) AdminNowPlayingDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.NowPlayingSource.DeleteOneID(id).Exec(s.Ctx)
	SetFlash(c, "success", "Now Playing source deleted")
	c.Redirect(http.StatusFound, "/admin/nowplaying")
}

// --- JSON API (session auth) ---

type nowPlayingAPIRequest struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	URL          string `json:"url"`
	Token        string `json:"token"`
	Username     string `json:"username"`
	ShowAlbumArt *bool  `json:"show_album_art"`
}

func (r *nowPlayingAPIRequest) input() nowPlayingInput {
	in := nowPlayingInput{
		Name: r.Name, Provider: r.Provider, URL: r.URL,
		Token: r.Token, Username: r.Username, ShowAlbumArt: true,
	}
	if r.Provider == "" {
		in.Provider = "jellyfin"
	}
	if r.ShowAlbumArt != nil {
		in.ShowAlbumArt = *r.ShowAlbumArt
	}
	return in
}

func (s *Server) APINowPlayingList(c *gin.Context) {
	items, _ := s.DB.NowPlayingSource.Query().Order(ent.Asc(nowplayingsource.FieldID)).All(s.Ctx)
	c.JSON(http.StatusOK, items)
}

func (s *Server) APINowPlayingGet(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.NowPlayingSource.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, obj)
}

func (s *Server) APINowPlayingCreate(c *gin.Context) {
	var req nowPlayingAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	in := req.input()
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	obj, err := s.DB.NowPlayingSource.Create().
		SetName(in.Name).SetProvider(nowplayingsource.Provider(in.Provider)).
		SetURL(in.URL).SetToken(in.Token).SetUsername(in.Username).SetShowAlbumArt(in.ShowAlbumArt).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddNowPlayingSources(obj).Exec(s.Ctx)
	}
	c.JSON(http.StatusCreated, obj)
}

func (s *Server) APINowPlayingUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req nowPlayingAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	in := req.input()
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	upd := s.DB.NowPlayingSource.UpdateOneID(id).
		SetName(in.Name).SetProvider(nowplayingsource.Provider(in.Provider)).
		SetURL(in.URL).SetUsername(in.Username)
	if in.Token != "" {
		upd = upd.SetToken(in.Token)
	}
	if req.ShowAlbumArt != nil {
		upd = upd.SetShowAlbumArt(*req.ShowAlbumArt)
	}
	if err := upd.Exec(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	obj, _ := s.DB.NowPlayingSource.Get(s.Ctx, id)
	c.JSON(http.StatusOK, obj)
}

func (s *Server) APINowPlayingDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.NowPlayingSource.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
