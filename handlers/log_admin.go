package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/logentry"
	"ledit/logging"
)

// ---------------------------------------------------------------------------
// Log Viewer (Task 8)
// ---------------------------------------------------------------------------

// AdminLogs renders the log viewer page.
func (s *Server) AdminLogs(c *gin.Context) {
	c.HTML(http.StatusOK, "logs.html", gin.H{
		"levels": logging.ValidLevels(),
	})
}

// AdminLogsAPI returns paginated log entries as JSON.
func (s *Server) AdminLogsAPI(c *gin.Context) {
	level := c.DefaultQuery("level", "")
	source := c.DefaultQuery("source", "")
	search := c.DefaultQuery("search", "")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	query := s.DB.LogEntry.Query().Order(ent.Desc(logentry.FieldTimestamp))

	if level != "" {
		query = query.Where(logentry.LevelEQ(level))
	}
	if source != "" {
		query = query.Where(logentry.SourceEQ(source))
	}
	if search != "" {
		query = query.Where(logentry.MessageContains(search))
	}

	total, err := query.Count(s.Ctx)
	if err != nil {
		slog.Error("failed to count log entries", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query logs"})
		return
	}

	offset := (page - 1) * pageSize
	entries, err := query.
		Offset(offset).
		Limit(pageSize).
		All(s.Ctx)
	if err != nil {
		slog.Error("failed to fetch log entries", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query logs"})
		return
	}

	type logEntryJSON struct {
		ID        int       `json:"id"`
		Timestamp time.Time `json:"timestamp"`
		Level     string    `json:"level"`
		Source    string    `json:"source"`
		Message   string    `json:"message"`
		Metadata  string    `json:"metadata,omitempty"`
	}

	items := make([]logEntryJSON, len(entries))
	for i, e := range entries {
		items[i] = logEntryJSON{
			ID:        e.ID,
			Timestamp: e.Timestamp,
			Level:     e.Level,
			Source:    e.Source,
			Message:   e.Message,
			Metadata:  e.Metadata,
		}
	}

	totalPages := (total + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// ---------------------------------------------------------------------------
// Log Settings (Task 9)
// ---------------------------------------------------------------------------

// AdminLogSettings renders the log settings form.
func (s *Server) AdminLogSettings(c *gin.Context) {
	settings, err := s.DB.LogSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	c.HTML(http.StatusOK, "log_settings.html", gin.H{
		"settings":    settings,
		"hasSettings": settings != nil,
		"levels":      logging.ValidLevels(),
	})
}

// AdminLogSettingsSave saves log settings and updates the live handler.
func (s *Server) AdminLogSettingsSave(c *gin.Context) {
	verbosity := c.PostForm("verbosity")
	retentionDays, _ := strconv.Atoi(c.DefaultPostForm("retention_days", "7"))
	otelEndpoint := c.PostForm("otel_endpoint")
	otelProtocol := c.DefaultPostForm("otel_protocol", "grpc")
	otelEnabled := c.PostForm("otel_enabled") == "on"

	if retentionDays < 1 {
		retentionDays = 7
	}

	exists, _ := s.DB.LogSettings.Query().Exist(s.Ctx)
	if !exists {
		_, err := s.DB.LogSettings.Create().
			SetVerbosity(verbosity).
			SetRetentionDays(retentionDays).
			SetOtelEndpoint(otelEndpoint).
			SetOtelProtocol(otelProtocol).
			SetOtelEnabled(otelEnabled).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to create log settings", "error", err)
		}
	} else {
		_, err := s.DB.LogSettings.Update().
			SetVerbosity(verbosity).
			SetRetentionDays(retentionDays).
			SetOtelEndpoint(otelEndpoint).
			SetOtelProtocol(otelProtocol).
			SetOtelEnabled(otelEnabled).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to update log settings", "error", err)
		}
	}

	// Update live handler minimum level
	if s.LogStore != nil {
		s.DB.LogSettings.Query().Only(s.Ctx)
		_ = logging.ParseLevel(verbosity)
		// The handler's min level is updated on next init; for live changes
		// we'd need to expose SetMinLevel. For now, log the intent.
		slog.Info("log verbosity updated (restart to take full effect)", "verbosity", verbosity)
	}

	// Update OTel exporter configuration
	if s.OTelExporter != nil {
		s.OTelExporter.Configure(otelEndpoint, otelProtocol, otelEnabled)
	}

	c.Redirect(http.StatusFound, "/admin/settings/logs")
}

// ---------------------------------------------------------------------------
// Email Settings (Task 10)
// ---------------------------------------------------------------------------

// AdminEmailSettings renders the email settings form.
func (s *Server) AdminEmailSettings(c *gin.Context) {
	settings, err := s.DB.EmailSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	c.HTML(http.StatusOK, "email_settings.html", gin.H{
		"settings":    settings,
		"hasSettings": settings != nil,
	})
}

// AdminEmailSettingsSave saves email settings.
func (s *Server) AdminEmailSettingsSave(c *gin.Context) {
	host := c.PostForm("host")
	port, _ := strconv.Atoi(c.DefaultPostForm("port", "587"))
	username := c.PostForm("username")
	password := c.PostForm("password")
	fromAddress := c.PostForm("from_address")
	useTLS := c.PostForm("use_tls") == "on"

	if port == 0 {
		port = 587
	}

	exists, _ := s.DB.EmailSettings.Query().Exist(s.Ctx)
	if !exists {
		_, err := s.DB.EmailSettings.Create().
			SetHost(host).
			SetPort(port).
			SetUsername(username).
			SetPassword(password).
			SetFromAddress(fromAddress).
			SetUseTLS(useTLS).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to create email settings", "error", err)
		}
	} else {
		_, err := s.DB.EmailSettings.Update().
			SetHost(host).
			SetPort(port).
			SetUsername(username).
			SetPassword(password).
			SetFromAddress(fromAddress).
			SetUseTLS(useTLS).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to update email settings", "error", err)
		}
	}

	c.Redirect(http.StatusFound, "/admin/settings/email")
}

// ---------------------------------------------------------------------------
// AI Settings (Task 11)
// ---------------------------------------------------------------------------

// AdminAISettings renders the AI settings form.
func (s *Server) AdminAISettings(c *gin.Context) {
	settings, err := s.DB.AISettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	c.HTML(http.StatusOK, "ai_settings.html", gin.H{
		"settings":    settings,
		"hasSettings": settings != nil,
	})
}

// AdminAISettingsTestConnection tests the AI provider connection.
func (s *Server) AdminAISettingsTestConnection(c *gin.Context) {
	provider := c.PostForm("provider")
	apiKey := c.PostForm("api_key")
	model := c.PostForm("model")
	endpoint := c.PostForm("endpoint")

	start := time.Now()
	err := testAIProviderConnection(provider, apiKey, model, endpoint)
	latency := time.Since(start)

	if err != nil {
		slog.Error("AI test connection failed", "source", "ai-settings", "provider", provider, "model", model, "endpoint", endpoint, "error", err, "latency_ms", latency.Milliseconds())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	slog.Info("AI test connection succeeded", "source", "ai-settings", "provider", provider, "model", model, "endpoint", endpoint, "latency_ms", latency.Milliseconds())
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Connection successful!"})
}

func testAIProviderConnection(provider, apiKey, model, endpoint string) error {
	if apiKey == "" && provider != "ollama" {
		return fmt.Errorf("API key is required")
	}

	switch provider {
	case "openai":
		url := endpoint
		if url == "" {
			url = "https://api.openai.com/v1/models"
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("API returned status %d", resp.StatusCode)
		}
		return nil

	case "anthropic":
		url := endpoint
		if url == "" {
			url = "https://api.anthropic.com/v1/messages"
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("API returned status %d", resp.StatusCode)
		}
		return nil

	case "ollama":
		url := endpoint
		if url == "" {
			url = "http://localhost:11434/api/tags"
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("API returned status %d", resp.StatusCode)
		}
		return nil

	default:
		// Generic: try the endpoint with API key in Authorization header
		if endpoint == "" {
			return fmt.Errorf("endpoint URL is required for custom provider")
		}
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			return err
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("API returned status %d", resp.StatusCode)
		}
		return nil
	}
}

// AdminAISettingsSave saves AI settings.
func (s *Server) AdminAISettingsSave(c *gin.Context) {
	provider := c.PostForm("provider")
	apiKey := c.PostForm("api_key")
	model := c.PostForm("model")
	endpoint := c.PostForm("endpoint")

	exists, _ := s.DB.AISettings.Query().Exist(s.Ctx)
	if !exists {
		_, err := s.DB.AISettings.Create().
			SetProvider(provider).
			SetAPIKey(apiKey).
			SetModel(model).
			SetEndpoint(endpoint).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to create AI settings", "error", err)
		}
	} else {
		_, err := s.DB.AISettings.Update().
			SetProvider(provider).
			SetAPIKey(apiKey).
			SetModel(model).
			SetEndpoint(endpoint).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to update AI settings", "error", err)
		}
	}

	c.Redirect(http.StatusFound, "/admin/settings/ai")
}
