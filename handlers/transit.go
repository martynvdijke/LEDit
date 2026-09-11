package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"ledit/ent/generalsettings"
	"ledit/ent/transit"
)

const transitDefaultTimezone = "Europe/Berlin"

var (
	transitProviders = []string{
		string(transit.ProviderVbb),
		string(transit.ProviderTransitland),
		string(transit.Provider511),
		string(transit.ProviderCustom),
	}
	transitTimeModes = []string{
		string(transit.TimeModeMinutes),
		string(transit.TimeModeClock),
	}
)

// transitInput is the validated shape shared by the admin form and JSON API.
// Field names mirror the ent.Transit entity so templates can render either.
type transitInput struct {
	Token         string
	URL           string
	APIKey        string
	Provider      string
	MaxDepartures int
	RouteFilter   string
	WalkTimeMin   int
	Timezone      string
	TimeMode      string
}

func (in *transitInput) applyDefaults() {
	if strings.TrimSpace(in.Provider) == "" {
		in.Provider = string(transit.ProviderVbb)
	}
	if strings.TrimSpace(in.TimeMode) == "" {
		in.TimeMode = string(transit.TimeModeMinutes)
	}
	if strings.TrimSpace(in.Timezone) == "" {
		in.Timezone = transitDefaultTimezone
	}
}

func (in *transitInput) validate() error {
	if strings.TrimSpace(in.Token) == "" {
		return fmt.Errorf("stop id is required")
	}
	if utf8.RuneCountInString(in.Token) > 64 {
		return fmt.Errorf("stop id must be at most 64 characters")
	}
	if !containsString(transitProviders, in.Provider) {
		return fmt.Errorf("provider must be one of %s", strings.Join(transitProviders, ", "))
	}
	if in.MaxDepartures < 1 || in.MaxDepartures > 8 {
		return fmt.Errorf("max departures must be between 1 and 8")
	}
	if in.WalkTimeMin < 0 || in.WalkTimeMin > 60 {
		return fmt.Errorf("walk time must be between 0 and 60 minutes")
	}
	if utf8.RuneCountInString(in.RouteFilter) > 256 {
		return fmt.Errorf("route filter must be at most 256 characters")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return fmt.Errorf("timezone must be a valid IANA location")
	}
	if !containsString(transitTimeModes, in.TimeMode) {
		return fmt.Errorf("time mode must be one of %s", strings.Join(transitTimeModes, ", "))
	}
	return nil
}

// fields renders the input as the map the field-based registry entry consumes.
func (in *transitInput) fields() map[string]string {
	return map[string]string{
		"token":          in.Token,
		"url":            in.URL,
		"api_key":        in.APIKey,
		"provider":       in.Provider,
		"max_departures": strconv.Itoa(in.MaxDepartures),
		"route_filter":   in.RouteFilter,
		"walk_time_min":  strconv.Itoa(in.WalkTimeMin),
		"timezone":       in.Timezone,
		"time_mode":      in.TimeMode,
	}
}

func transitInputFromForm(c *gin.Context) transitInput {
	in := transitInput{
		Token:         strings.TrimSpace(c.PostForm("token")),
		URL:           strings.TrimSpace(c.PostForm("url")),
		APIKey:        c.PostForm("api_key"),
		Provider:      c.DefaultPostForm("provider", string(transit.ProviderVbb)),
		MaxDepartures: mustAtoi(c.DefaultPostForm("max_departures", "4")),
		RouteFilter:   strings.TrimSpace(c.PostForm("route_filter")),
		WalkTimeMin:   mustAtoi(c.DefaultPostForm("walk_time_min", "0")),
		Timezone:      strings.TrimSpace(c.PostForm("timezone")),
		TimeMode:      c.DefaultPostForm("time_mode", string(transit.TimeModeMinutes)),
	}
	in.applyDefaults()
	return in
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// --- Admin form handlers ---

func (s *Server) AdminTransitNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "transit_form.html", gin.H{})
}

func (s *Server) AdminTransitCreate(c *gin.Context) {
	in := transitInputFromForm(c)
	if err := in.validate(); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusBadRequest, "transit_form.html", gin.H{"obj": in, "error": err.Error()})
		return
	}
	obj, err := dsRegistry["transit"].CreateFields(s.DB, s.Ctx, in.fields())
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		s.renderPage(c, http.StatusBadRequest, "transit_form.html", gin.H{"obj": in, "error": err.Error()})
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && settings != nil {
		dsRegistry["transit"].AddEdge(s.DB.GeneralSettings.UpdateOne(settings), obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Transit created")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminTransitEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Transit.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Transit source not found")
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	s.renderPage(c, http.StatusOK, "transit_form.html", gin.H{"obj": obj, "edit": true, "id": id})
}

func (s *Server) AdminTransitUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	in := transitInputFromForm(c)
	if err := in.validate(); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusBadRequest, "transit_form.html", gin.H{"obj": in, "edit": true, "id": id, "error": err.Error()})
		return
	}
	if in.APIKey == "" {
		if existing, err := s.DB.Transit.Get(s.Ctx, id); err == nil {
			in.APIKey = existing.APIKey
		}
	}
	if err := dsRegistry["transit"].UpdateFields(s.DB, s.Ctx, id, in.fields()); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		s.renderPage(c, http.StatusBadRequest, "transit_form.html", gin.H{"obj": in, "edit": true, "id": id, "error": err.Error()})
		return
	}
	SetFlash(c, "success", "Transit updated")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminTransitDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := dsRegistry["transit"].Delete(s.DB, s.Ctx, id); err != nil {
		slog.Error("failed to delete transit", "id", id, "error", err)
	}
	SetFlash(c, "success", "Transit deleted")
	c.Redirect(http.StatusFound, "/admin/")
}

// --- JSON API (session auth) ---

type transitAPIRequest struct {
	Token         string `json:"token"`
	StopID        string `json:"stop_id"`
	URL           string `json:"url"`
	APIKey        string `json:"api_key"`
	Provider      string `json:"provider"`
	MaxDepartures *int   `json:"max_departures"`
	RouteFilter   string `json:"route_filter"`
	WalkTimeMin   *int   `json:"walk_time_min"`
	Timezone      string `json:"timezone"`
	TimeMode      string `json:"time_mode"`
}

func (r *transitAPIRequest) input() transitInput {
	in := transitInput{
		Token:       firstNonEmpty(r.Token, r.StopID),
		URL:         r.URL,
		APIKey:      r.APIKey,
		Provider:    r.Provider,
		RouteFilter: r.RouteFilter,
		Timezone:    r.Timezone,
		TimeMode:    r.TimeMode,
	}
	if r.MaxDepartures != nil {
		in.MaxDepartures = *r.MaxDepartures
	} else {
		in.MaxDepartures = 4
	}
	if r.WalkTimeMin != nil {
		in.WalkTimeMin = *r.WalkTimeMin
	}
	in.applyDefaults()
	return in
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (s *Server) APITransitList(c *gin.Context) {
	items, _ := s.DB.Transit.Query().All(s.Ctx)
	c.JSON(http.StatusOK, items)
}

func (s *Server) APITransitGet(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Transit.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, obj)
}

func (s *Server) APITransitCreate(c *gin.Context) {
	var req transitAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	in := req.input()
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	obj, err := dsRegistry["transit"].CreateFields(s.DB, s.Ctx, in.fields())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && settings != nil {
		dsRegistry["transit"].AddEdge(s.DB.GeneralSettings.UpdateOne(settings), obj).Exec(s.Ctx)
	}
	c.JSON(http.StatusCreated, obj)
}

func (s *Server) APITransitUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req transitAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	in := req.input()
	if err := in.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.APIKey == "" {
		if existing, err := s.DB.Transit.Get(s.Ctx, id); err == nil {
			in.APIKey = existing.APIKey
		}
	}
	if err := dsRegistry["transit"].UpdateFields(s.DB, s.Ctx, id, in.fields()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	obj, _ := s.DB.Transit.Get(s.Ctx, id)
	c.JSON(http.StatusOK, obj)
}

func (s *Server) APITransitDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Transit.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
