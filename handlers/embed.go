package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// embedCookieName is the short-lived HttpOnly cookie a device embed exchanges
// its URL-fragment secret for at POST /embed/session. Like the frame cookie it
// carries the same high-entropy guest secret (not a separate token space) and
// is scoped by the routes that read it.
const embedCookieName = "ledit_embed"

// Embed surface rate limits (per fixed one-minute window). The shared embed is
// public, so page loads and WebSocket upgrades are throttled per client IP;
// token clients are keyed by token id instead of sharing the anonymous bucket.
const (
	embedPageIPLimit    = 120
	embedUpgradeIPLimit = 60
	embedUpgradeTokRate = 120
	embedFrameIPLimit   = 120
	embedFrameTokLimit  = 240
	embedUpgradeKeyIP   = "embed:ws:ip:"
)

func abortEmbedUnauthorized(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

func abortEmbedForbidden(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient_scope", "code": "insufficient_scope"})
}

// embedAuthorizeRequest authorizes a device-scoped embed request in place. It
// returns true when the request may proceed; otherwise it has already written
// the JSON error response. It accepts an authenticated session or API token, or
// a guest secret (ledit_embed cookie, X-Guest-Token header, or Bearer) carrying
// the read-only `embed` scope.
func (s *Server) embedAuthorizeRequest(c *gin.Context) bool {
	if authed, _ := s.isAuthenticatedWithRole(c, "viewer"); authed {
		return true
	}
	secret := guestSecretFromRequest(c, embedCookieName)
	if secret == "" {
		abortEmbedUnauthorized(c)
		return false
	}
	tok, ok := s.lookupGuestToken(c.Request.Context(), secret)
	if !ok {
		abortEmbedUnauthorized(c)
		return false
	}
	if !hasGuestScope(tok.Scopes, "embed") {
		abortEmbedForbidden(c)
		return false
	}
	_, _ = s.DB.GuestToken.UpdateOneID(tok.ID).
		SetLastUsedAt(time.Now()).
		Save(c.Request.Context())
	c.Set(ctxGuestToken, tok)
	return true
}

// EmbedAuthMiddleware authorizes the device embed surfaces. It accepts the
// `ledit_embed` HttpOnly cookie, an `X-Guest-Token` header, or an
// `Authorization: Bearer` secret, and also accepts an authenticated session or
// API token. Guest tokens must carry the read-only `embed` scope. Failures are
// JSON 401 (unknown/revoked/expired or absent) or 403 (scope missing) and never
// redirect to the login page, so an expired OBS source shows a static error.
func (s *Server) EmbedAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.embedAuthorizeRequest(c) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// embedPageVars supplies the chrome-free shell's data attributes. Only the
// target kind and id are rendered; no source, device name, or queue data.
func embedPageVars(target string, deviceID int) gin.H {
	return gin.H{
		"embedTarget":   target,
		"embedDeviceID": deviceID,
	}
}

// EmbedPage serves the shared-feed embed shell (public).
func (s *Server) EmbedPage(c *gin.Context) {
	if ok, retry := checkWindowRateLimit("embed:page:ip:"+c.ClientIP(), embedPageIPLimit); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "Cookie, Authorization")
	s.renderPage(c, http.StatusOK, "embed.html", embedPageVars("shared", 0))
}

// EmbedDevicePage serves the device embed shell. The page itself is public: it
// renders no device data, and the feed upgrade is what requires the embed
// token, which the client sends from the URL fragment (never the query string).
func (s *Server) EmbedDevicePage(c *gin.Context) {
	if ok, retry := checkWindowRateLimit("embed:page:ip:"+c.ClientIP(), embedPageIPLimit); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "Cookie, Authorization")
	s.renderPage(c, http.StatusOK, "embed.html", embedPageVars("device", id))
}

// EmbedSession exchanges a device-embed secret (posted in the JSON or form body)
// for the short-lived HttpOnly `ledit_embed` cookie, mirroring FrameSession. The
// secret is read from the body only, so it never appears in a request line.
func (s *Server) EmbedSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
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

	tok, ok := s.lookupGuestToken(c.Request.Context(), secret)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if !hasGuestScope(tok.Scopes, "embed") {
		abortEmbedForbidden(c)
		return
	}

	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	// Cross-site iframes (OBS, other origins) need SameSite=None, which browsers
	// only accept together with Secure. Over plain HTTP we fall back to the
	// browser default so local development still works.
	if secure {
		c.SetSameSite(http.SameSiteNoneMode)
	}
	c.SetCookie(embedCookieName, secret, 3600, "/", "", secure, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
