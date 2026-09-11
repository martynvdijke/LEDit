package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/guesttoken"
)

// ctxGuestToken is the gin context key holding the authenticated *ent.GuestToken.
const ctxGuestToken = "guest_token"

// guestValidScopes is the closed set of scopes a guest token may carry.
var guestValidScopes = map[string]struct{}{
	"pause":   {},
	"next":    {},
	"message": {},
}

// hashGuestToken returns the SHA-256 hex digest used for guest token lookup.
// Only the digest is persisted; the raw secret is shown once at creation.
func hashGuestToken(secret string) string { return hashAPIToken(secret) }

// generateGuestToken returns a new 256-bit random secret and its display prefix.
// It reuses the API token generator so the crypto lives in one place.
func generateGuestToken() (secret, prefix string) { return generateAPIToken() }

// currentGuestToken returns the token established by GuestAuthMiddleware.
func currentGuestToken(c *gin.Context) *ent.GuestToken {
	if v, ok := c.Get(ctxGuestToken); ok {
		if t, ok := v.(*ent.GuestToken); ok {
			return t
		}
	}
	return nil
}

func hasGuestScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

func abortGuestUnauthorized(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

// GuestAuthMiddleware authenticates guest API calls via the `X-Guest-Token`
// header (or `Authorization: Bearer`). It hashes the secret, looks up the row,
// rejects unknown/revoked/expired tokens with 401 without revealing whether the
// token exists, enforces requiredScope with 403 insufficient_scope, and records
// last use best-effort. It never reads cookies, so an admin session cannot
// authenticate a guest route.
func (s *Server) GuestAuthMiddleware(requiredScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		secret := strings.TrimSpace(c.GetHeader("X-Guest-Token"))
		if secret == "" {
			if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				secret = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if secret == "" {
			abortGuestUnauthorized(c)
			return
		}

		tok, err := s.DB.GuestToken.Query().
			Where(guesttoken.TokenHashEQ(hashGuestToken(secret))).
			Only(c.Request.Context())
		if err != nil || tok.RevokedAt != nil ||
			(tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now())) {
			abortGuestUnauthorized(c)
			return
		}

		if requiredScope != "" && !hasGuestScope(tok.Scopes, requiredScope) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient_scope", "code": "insufficient_scope"})
			return
		}

		// Record last use (best-effort; never fail the request on a write error).
		_, _ = s.DB.GuestToken.UpdateOneID(tok.ID).
			SetLastUsedAt(time.Now()).
			Save(c.Request.Context())

		c.Set(ctxGuestToken, tok)
		c.Next()
	}
}

// ---------------------------------------------------------------------------
// In-memory fixed-window rate limiter
// ---------------------------------------------------------------------------

var (
	guestRateMu sync.Mutex
	guestRate   = map[string][]time.Time{}
)

// checkGuestRateLimit applies a fixed one-minute window to key. It returns
// false and the whole seconds until the oldest hit leaves the window when the
// limit is exceeded, otherwise true. Entries older than the window are pruned
// on access.
//
// ponytail: in-memory limiter, single-process; move to a shared store only if
// LEDit runs multi-instance.
func checkGuestRateLimit(key string, limit int) (bool, int) {
	guestRateMu.Lock()
	defer guestRateMu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	times := guestRate[key]
	filtered := times[:0]
	for _, t := range times {
		if t.After(cut) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= limit {
		guestRate[key] = filtered
		retry := int(time.Until(filtered[0].Add(time.Minute)).Seconds())
		if retry < 1 {
			retry = 1
		}
		return false, retry
	}
	filtered = append(filtered, now)
	guestRate[key] = filtered
	return true, 0
}

// abortGuestRateLimited rejects a throttled request with 429 and Retry-After.
func abortGuestRateLimited(c *gin.Context, retryAfter int) {
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate_limit_exceeded", "code": "rate_limit_exceeded"})
}

// ---------------------------------------------------------------------------
// Guest API (scoped, rate-limited, reusing the existing feed/notification path)
// ---------------------------------------------------------------------------

// RemotePage serves the installable guest remote shell. It carries no feed,
// source, device, or notification data.
func (s *Server) RemotePage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.HTML(http.StatusOK, "remote.html", gin.H{})
}

// APIGuestStatus returns the minimal guest view: paused state, the token's
// scopes, and its expiry. Never source names, queue, devices, or pin data.
func (s *Server) APIGuestStatus(c *gin.Context) {
	tok := currentGuestToken(c)
	if tok == nil {
		abortGuestUnauthorized(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"paused":     GlobalFeed.IsPaused(),
		"scopes":     tok.Scopes,
		"expires_at": tok.ExpiresAt,
	})
}

// guestControl applies the shared control rate limit (30/min per token), runs
// the feed action, and returns {"status":"ok"}.
func (s *Server) guestControl(c *gin.Context, action func()) {
	tok := currentGuestToken(c)
	if tok == nil {
		abortGuestUnauthorized(c)
		return
	}
	if ok, retry := checkGuestRateLimit("ctrl:"+strconv.Itoa(tok.ID), 30); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	action()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APIGuestPause(c *gin.Context)  { s.guestControl(c, GlobalFeed.Pause) }
func (s *Server) APIGuestResume(c *gin.Context) { s.guestControl(c, GlobalFeed.Resume) }
func (s *Server) APIGuestNext(c *gin.Context)   { s.guestControl(c, GlobalFeed.Next) }

// APIGuestMessage pushes a short guest text to the wall through the existing
// AddNotification/TTL path. The admin-visible title is server-derived so guests
// cannot spoof it; the untrusted text is the message body.
func (s *Server) APIGuestMessage(c *gin.Context) {
	tok := currentGuestToken(c)
	if tok == nil {
		abortGuestUnauthorized(c)
		return
	}
	if ok, retry := checkGuestRateLimit("msg:tok:"+strconv.Itoa(tok.ID), 5); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	if ok, retry := checkGuestRateLimit("msg:ip:"+c.ClientIP(), 20); !ok {
		abortGuestRateLimited(c, retry)
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}
	if utf8.RuneCountInString(text) > 140 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message too long (max 140 characters)"})
		return
	}

	// webhookDefaultTTL is already clamped to 1-3600 seconds.
	ttlSec := s.webhookDefaultTTL()
	ttl := time.Duration(ttlSec) * time.Second
	s.AddNotification("Guest message", text, WithTTL(ttl))
	c.JSON(http.StatusAccepted, gin.H{
		"id":         CurrentNotifSeq(),
		"ttl":        ttlSec,
		"expires_at": time.Now().Add(ttl).UTC().Format(time.RFC3339),
	})
}
