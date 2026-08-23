package handlers

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// WebhookAuthMiddleware authenticates webhook/display requests via X-API-Key header or ?token= query param.
// If no key is configured (no row or empty key), it is a no-op.
func (s *Server) WebhookAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := s.webhookAPIKey()
		if key == "" {
			c.Next()
			return
		}
		provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if provided == "" {
			provided = strings.TrimSpace(c.Query("token"))
		} else {
			// Also trim query token comparison? No, header takes precedence.
			provided = strings.TrimSpace(provided)
		}
		if provided == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		// Constant-time compare.
		if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func (s *Server) webhookAPIKey() string {
	if s.DB == nil {
		return ""
	}
	ctx := s.Ctx
	if ctx == nil {
		return ""
	}
	// Load via GeneralSettings edge cheaply; fallback to direct query if no general settings.
	// Use direct WebhookSettings query: if no row, no key.
	ws, err := s.DB.WebhookSettings.Query().Only(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ws.APIKey)
}

func (s *Server) webhookDefaultTTL() int {
	if s.DB == nil {
		return 30
	}
	ctx := s.Ctx
	if ctx == nil {
		return 30
	}
	ws, err := s.DB.WebhookSettings.Query().Only(ctx)
	if err != nil {
		return 30
	}
	if ws.DefaultTTL < 1 {
		return 1
	}
	if ws.DefaultTTL > 3600 {
		return 3600
	}
	return ws.DefaultTTL
}

// APIDisplay handles GET /api/display?text=&ttl=&color=
func (s *Server) APIDisplay(c *gin.Context) {
	text := strings.TrimSpace(c.Query("text"))
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}
	color := strings.TrimSpace(c.Query("color"))

	// TTL handling: default from settings, clamp 1..3600. ttl=0 -> default.
	ttlSec := s.webhookDefaultTTL()
	if raw := strings.TrimSpace(c.Query("ttl")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			if v == 0 {
				ttlSec = s.webhookDefaultTTL()
			} else {
				ttlSec = v
				if ttlSec < 1 {
					ttlSec = 1
				}
				if ttlSec > 3600 {
					ttlSec = 3600
				}
			}
		}
	}

	ttl := time.Duration(ttlSec) * time.Second
	expiresAt := time.Now().Add(ttl)

	var opts []NotifOption
	opts = append(opts, WithTTL(ttl))
	if color != "" {
		opts = append(opts, withColor(color))
	}
	s.AddNotification(text, "", opts...)

	// Need ID of created notification. CurrentNotifSeq is last ID.
	id := CurrentNotifSeq()
	c.JSON(http.StatusAccepted, gin.H{
		"id":         id,
		"ttl":        ttlSec,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}
