package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent/guesttoken"
)

func (s *Server) FramePage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s.renderPage(c, http.StatusOK, "frame.html", gin.H{})
}

func (s *Server) FrameSession(c *gin.Context) {
	secret := strings.TrimSpace(c.PostForm("token"))
	if secret == "" {
		var req struct {
			Token string `json:"token"`
		}
		_ = c.ShouldBindJSON(&req)
		secret = strings.TrimSpace(req.Token)
	}

	if secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	tok, err := s.DB.GuestToken.Query().
		Where(guesttoken.TokenHashEQ(hashGuestToken(secret))).
		Only(c.Request.Context())
	if err != nil || tok.RevokedAt != nil || (tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now())) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if !hasGuestScope(tok.Scopes, "photo") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient_scope"})
		return
	}

	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(frameCookieName, secret, 3600, "/", "", secure, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
