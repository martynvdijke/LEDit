package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
)

// AdminTextSlideGenerate uses the configured AI provider to draft text-slide
// content for the given prompt. It is strictly human-in-the-loop: the result
// fills the form field, nothing is persisted until the user submits the form.
func (s *Server) AdminTextSlideGenerate(c *gin.Context) {
	ai, err := s.DB.AISettings.Query().Only(c.Request.Context())
	if err != nil || ai.Endpoint == "" || ai.Model == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "AI settings are not configured. Configure an endpoint and model in Settings → AI."})
		return
	}
	if ai.APIKey == "" && ai.Provider != "ollama" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "AI settings are not configured. Add an API key in Settings → AI."})
		return
	}

	prompt := c.PostForm("prompt")
	if prompt == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Describe what you want to display (e.g. \"Today's weather summary\")."})
		return
	}

	cfg := datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
	content, err := datasource.ChatCompletions(c.Request.Context(), cfg, []datasource.ChatMessage{
		{Role: "system", Content: datasource.BuildSlideSystemPrompt()},
		{Role: "user", Content: prompt},
	}, 200)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}
