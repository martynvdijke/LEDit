package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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
