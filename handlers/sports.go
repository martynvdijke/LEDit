package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
	"ledit/ent/sports"
)

var sportsProviderValues = []string{
	string(sports.ProviderEspn),
	string(sports.ProviderThesportsdb),
	string(sports.ProviderApifootball),
}

// sportsProviderOr normalizes an empty provider to the espn default. Used by
// the registry's field-based create/update.
func sportsProviderOr(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return string(sports.ProviderEspn)
	}
	return v
}

// sportsInput is the validated shape shared by the admin form and JSON API.
type sportsInput struct {
	Token              string
	URL                string
	Provider           string
	Config             string
	LiveRefreshSeconds int
	IdleRefreshSeconds int
}

func (in *sportsInput) applyDefaults() {
	in.Provider = sportsProviderOr(in.Provider)
	if in.LiveRefreshSeconds == 0 {
		in.LiveRefreshSeconds = 30
	}
	if in.IdleRefreshSeconds == 0 {
		in.IdleRefreshSeconds = 300
	}
}

func (in *sportsInput) validate() error {
	if !containsString(sportsProviderValues, in.Provider) {
		return fmt.Errorf("provider must be one of %s", strings.Join(sportsProviderValues, ", "))
	}
	if in.LiveRefreshSeconds < 15 {
		return fmt.Errorf("live refresh must be at least 15 seconds")
	}
	if in.IdleRefreshSeconds < 60 {
		return fmt.Errorf("idle refresh must be at least 60 seconds")
	}
	if utf8.RuneCountInString(in.Token) > 256 {
		return fmt.Errorf("API key must be at most 256 characters")
	}
	if err := datasource.ValidateSportsConfig(in.Config); err != nil {
		return err
	}
	return nil
}

func (in *sportsInput) fields() map[string]string {
	return map[string]string{
		"token":                in.Token,
		"url":                  in.URL,
		"provider":             in.Provider,
		"config":               in.Config,
		"live_refresh_seconds": strconv.Itoa(in.LiveRefreshSeconds),
		"idle_refresh_seconds": strconv.Itoa(in.IdleRefreshSeconds),
	}
}

func sportsInputFromForm(c *gin.Context) sportsInput {
	in := sportsInput{
		Token:              c.PostForm("token"),
		URL:                strings.TrimSpace(c.PostForm("url")),
		Provider:           strings.TrimSpace(c.PostForm("provider")),
		Config:             strings.TrimSpace(c.PostForm("config")),
		LiveRefreshSeconds: mustAtoi(c.DefaultPostForm("live_refresh_seconds", "30")),
		IdleRefreshSeconds: mustAtoi(c.DefaultPostForm("idle_refresh_seconds", "300")),
	}
	in.applyDefaults()
	return in
}

// --- Admin form handlers ---

func (s *Server) AdminSportsList(c *gin.Context) {
	items, err := s.DB.Sports.Query().Order(ent.Asc(sports.FieldID)).All(s.Ctx)
	if err != nil {
		items = []*ent.Sports{}
	}
	s.renderPage(c, http.StatusOK, "sports.html", gin.H{"sports": items})
}

func (s *Server) AdminSportsNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "sports_form.html", gin.H{})
}

func (s *Server) AdminSportsCreate(c *gin.Context) {
	in := sportsInputFromForm(c)
	if err := in.validate(); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusBadRequest, "sports_form.html", gin.H{"obj": in, "error": err.Error()})
		return
	}
	obj, err := dsRegistry["sports"].CreateFields(s.DB, s.Ctx, in.fields())
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		s.renderPage(c, http.StatusBadRequest, "sports_form.html", gin.H{"obj": in, "error": err.Error()})
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && settings != nil {
		dsRegistry["sports"].AddEdge(s.DB.GeneralSettings.UpdateOne(settings), obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Sports source created")
	c.Redirect(http.StatusFound, "/admin/sports")
}

func (s *Server) AdminSportsEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Sports.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Sports source not found")
		c.Redirect(http.StatusFound, "/admin/sports")
		return
	}
	s.renderPage(c, http.StatusOK, "sports_form.html", gin.H{"obj": obj, "edit": true, "id": id})
}

func (s *Server) AdminSportsUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	in := sportsInputFromForm(c)
	if err := in.validate(); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusBadRequest, "sports_form.html", gin.H{"obj": in, "edit": true, "id": id, "error": err.Error()})
		return
	}
	if in.Token == "" {
		if existing, err := s.DB.Sports.Get(s.Ctx, id); err == nil {
			in.Token = existing.Token
		}
	}
	if err := dsRegistry["sports"].UpdateFields(s.DB, s.Ctx, id, in.fields()); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		s.renderPage(c, http.StatusBadRequest, "sports_form.html", gin.H{"obj": in, "edit": true, "id": id, "error": err.Error()})
		return
	}
	SetFlash(c, "success", "Sports source updated")
	c.Redirect(http.StatusFound, "/admin/sports")
}

func (s *Server) AdminSportsDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := dsRegistry["sports"].Delete(s.DB, s.Ctx, id); err != nil {
		SetFlash(c, "danger", "Failed to delete: "+err.Error())
	} else {
		SetFlash(c, "success", "Sports source deleted")
	}
	c.Redirect(http.StatusFound, "/admin/sports")
}

// --- JSON API (session auth) ---

type sportsAPIRequest struct {
	Token              string `json:"token"`
	APIKey             string `json:"api_key"`
	URL                string `json:"url"`
	Provider           string `json:"provider"`
	Config             string `json:"config"`
	LiveRefreshSeconds *int   `json:"live_refresh_seconds"`
	IdleRefreshSeconds *int   `json:"idle_refresh_seconds"`
}

func (r *sportsAPIRequest) input() sportsInput {
	in := sportsInput{
		Token:    firstNonEmpty(r.Token, r.APIKey),
		URL:      r.URL,
		Provider: r.Provider,
		Config:   r.Config,
	}
	if r.LiveRefreshSeconds != nil {
		in.LiveRefreshSeconds = *r.LiveRefreshSeconds
	}
	if r.IdleRefreshSeconds != nil {
		in.IdleRefreshSeconds = *r.IdleRefreshSeconds
	}
	in.applyDefaults()
	return in
}

// sportsListView omits the stored API key from list payloads.
func sportsListView(items []*ent.Sports) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, sp := range items {
		out = append(out, gin.H{
			"ID":                 sp.ID,
			"URL":                sp.URL,
			"Provider":           string(sp.Provider),
			"Config":             sp.Config,
			"LiveRefreshSeconds": sp.LiveRefreshSeconds,
			"IdleRefreshSeconds": sp.IdleRefreshSeconds,
		})
	}
	return out
}

func (s *Server) APISportsList(c *gin.Context) {
	items, _ := s.DB.Sports.Query().Order(ent.Asc(sports.FieldID)).All(s.Ctx)
	c.JSON(http.StatusOK, sportsListView(items))
}

func (s *Server) APISportsGet(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Sports.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, obj)
}

func (s *Server) APISportsCreate(c *gin.Context) {
	var req sportsAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	in := req.input()
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	obj, err := dsRegistry["sports"].CreateFields(s.DB, s.Ctx, in.fields())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && settings != nil {
		dsRegistry["sports"].AddEdge(s.DB.GeneralSettings.UpdateOne(settings), obj).Exec(s.Ctx)
	}
	c.JSON(http.StatusCreated, obj)
}

func (s *Server) APISportsUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req sportsAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	in := req.input()
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.Token == "" {
		if existing, err := s.DB.Sports.Get(s.Ctx, id); err == nil {
			in.Token = existing.Token
		}
	}
	if err := dsRegistry["sports"].UpdateFields(s.DB, s.Ctx, id, in.fields()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	obj, _ := s.DB.Sports.Get(s.Ctx, id)
	c.JSON(http.StatusOK, obj)
}

func (s *Server) APISportsDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Sports.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
