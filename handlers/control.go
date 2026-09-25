package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
)

// Security note: control execution only resolves sources from the admin-
// configured catalog (GeneralSettings edges via buildSourceIndex). No
// arbitrary outbound URL/SSRF surface is added — params are forwarded only
// to an already-configured source's Actuate method.

// APIControlActions lists the supported control actions for a source type.
func (s *Server) APIControlActions(c *gin.Context) {
	sourceType := c.Query("source_type")
	if sourceType == "" {
		var body struct {
			SourceType string `json:"source_type"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.SourceType != "" {
			sourceType = body.SourceType
		}
	}
	actions := datasource.ControlActions(sourceType)
	if actions == nil {
		actions = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"source_type": sourceType, "actions": actions})
}

// APIControlExecute executes a control action on a configured source.
// Route is wired under RequireAdmin.
func (s *Server) APIControlExecute(c *gin.Context) {
	var req struct {
		SourceType string            `json:"source_type"`
		SourceID   int               `json:"source_id"`
		Action     string            `json:"action"`
		Params     map[string]string `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.SourceType == "" || req.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_type and action are required"})
		return
	}
	allowed := datasource.ControlActions(req.SourceType)
	ok := false
	for _, a := range allowed {
		if a == req.Action {
			ok = true
			break
		}
	}
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown_action"})
		return
	}
	settings, err := s.loadSettingsWithAll(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to load settings"})
		return
	}
	aiCfg := datasource.AIConfig{}
	if ai, aerr := s.DB.AISettings.Query().Only(c.Request.Context()); aerr == nil && ai != nil {
		aiCfg = datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
	}
	idx := buildSourceIndex(settings, aiCfg)
	ds, _, err := idx.Resolve(req.SourceType, req.SourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source not found"})
		return
	}
	act, ok := ds.(datasource.Actuator)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not_actuatable"})
		return
	}
	slog.Info("control execute", "source_type", req.SourceType, "source_id", req.SourceID, "action", req.Action)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := act.Actuate(ctx, req.Action, req.Params); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "action": req.Action})
}
