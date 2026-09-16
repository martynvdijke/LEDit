package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
)

// WebhookAuthMiddleware authenticates webhook/display requests via X-API-Key header or ?token= query param.
// If no key is configured (no row or empty key), it is a no-op.
// If SigningSecret is set, it additionally requires HMAC-SHA256 signing via
// X-LEDit-Timestamp and X-LEDit-Signature: sha256=<hex HMAC-SHA256(secret, timestamp + "." + body)>.
func (s *Server) WebhookAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ws := s.webhookSettings()
		key := ""
		signingSecret := ""
		window := 300
		if ws != nil {
			key = strings.TrimSpace(ws.APIKey)
			signingSecret = ws.SigningSecret
			if ws.SigningWindowSeconds > 0 {
				window = ws.SigningWindowSeconds
			}
		}
		if signingSecret == "" {
			if key == "" {
				c.Next()
				return
			}
			provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
			if provided == "" {
				provided = strings.TrimSpace(c.Query("token"))
			} else {
				provided = strings.TrimSpace(provided)
			}
			if provided == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			c.Next()
			return
		}
		// Signing required: require API key (if configured) AND valid signature.
		if key != "" {
			provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
			if provided == "" {
				provided = strings.TrimSpace(c.Query("token"))
			} else {
				provided = strings.TrimSpace(provided)
			}
			if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
		} else {
			// Signing is enabled but no API key configured: still require signature
			// (do not fall through to no-op).
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		tsStr := strings.TrimSpace(c.GetHeader("X-LEDit-Timestamp"))
		sigHeader := strings.TrimSpace(c.GetHeader("X-LEDit-Signature"))
		if tsStr == "" || sigHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if !strings.HasPrefix(sigHeader, "sha256=") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		hexPart := strings.TrimPrefix(sigHeader, "sha256=")
		providedSig, err := hex.DecodeString(hexPart)
		if err != nil || len(providedSig) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		now := time.Now().Unix()
		diff := now - ts
		if diff < 0 {
			diff = -diff
		}
		if diff > int64(window) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		mac := hmac.New(sha256.New, []byte(signingSecret))
		mac.Write([]byte(tsStr))
		mac.Write([]byte("."))
		mac.Write(body)
		expected := mac.Sum(nil)
		if !hmac.Equal(providedSig, expected) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func (s *Server) webhookSettings() *ent.WebhookSettings {
	if s.DB == nil {
		return nil
	}
	ctx := s.Ctx
	if ctx == nil {
		return nil
	}
	ws, err := s.DB.WebhookSettings.Query().Only(ctx)
	if err != nil {
		return nil
	}
	return ws
}

func (s *Server) webhookAPIKey() string {
	ws := s.webhookSettings()
	if ws == nil {
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
