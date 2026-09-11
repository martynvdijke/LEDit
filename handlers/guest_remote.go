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

// ---------------------------------------------------------------------------
// Admin management (rendered page + JSON API), admin-only via AdminRoleMiddleware
// ---------------------------------------------------------------------------

// guestTokenView is the safe, secret-free view of a guest token for listings.
type guestTokenView struct {
	ID         int        `json:"id"`
	Label      string     `json:"label"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func toGuestTokenView(t *ent.GuestToken) guestTokenView {
	return guestTokenView{
		ID:         t.ID,
		Label:      t.Label,
		Prefix:     t.TokenPrefix,
		Scopes:     t.Scopes,
		CreatedAt:  t.CreatedAt,
		ExpiresAt:  t.ExpiresAt,
		RevokedAt:  t.RevokedAt,
		LastUsedAt: t.LastUsedAt,
	}
}

func (s *Server) guestTokenViews() []guestTokenView {
	toks, err := s.DB.GuestToken.Query().
		Order(ent.Desc(guesttoken.FieldCreatedAt)).
		All(s.Ctx)
	if err != nil {
		return nil
	}
	views := make([]guestTokenView, 0, len(toks))
	for _, t := range toks {
		views = append(views, toGuestTokenView(t))
	}
	return views
}

// normalizeGuestScopes validates and de-duplicates a requested scope set.
// An empty request defaults to all scopes.
func normalizeGuestScopes(in []string) ([]string, bool) {
	if len(in) == 0 {
		return []string{"pause", "next", "message"}, true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, sc := range in {
		sc = strings.TrimSpace(sc)
		if _, ok := guestValidScopes[sc]; !ok {
			return nil, false
		}
		if !seen[sc] {
			seen[sc] = true
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// AdminGuestRemotes renders the management page.
func (s *Server) AdminGuestRemotes(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "guest_remotes.html", gin.H{
		"tokens": s.guestTokenViews(),
	})
}

// APIGuestRemotesList returns metadata-only token views (never the hash).
func (s *Server) APIGuestRemotesList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, s.guestTokenViews())
}

// APIGuestRemotesCreate creates a scoped guest token and returns the one-time
// secret plus a share link carrying the secret in the URL fragment.
func (s *Server) APIGuestRemotesCreate(c *gin.Context) {
	var req struct {
		Label          string   `json:"label"`
		Scopes         []string `json:"scopes"`
		ExpiresInHours int      `json:"expires_in_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	label := strings.TrimSpace(req.Label)
	if utf8.RuneCountInString(label) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label too long (max 64 characters)"})
		return
	}
	scopes, ok := normalizeGuestScopes(req.Scopes)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scopes must be a non-empty subset of pause, next, message"})
		return
	}

	secret, prefix := generateGuestToken()
	builder := s.DB.GuestToken.Create().
		SetLabel(label).
		SetTokenHash(hashGuestToken(secret)).
		SetTokenPrefix(prefix).
		SetScopes(scopes).
		SetCreatedAt(time.Now())
	if req.ExpiresInHours > 0 {
		builder.SetExpiresAt(time.Now().Add(time.Duration(req.ExpiresInHours) * time.Hour))
	}
	tok, err := builder.Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create guest token"})
		return
	}

	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	link := scheme + "://" + c.Request.Host + "/remote#" + secret

	// The secret is returned exactly once, in the create response only.
	c.JSON(http.StatusCreated, gin.H{
		"id":         tok.ID,
		"label":      tok.Label,
		"prefix":     tok.TokenPrefix,
		"scopes":     tok.Scopes,
		"expires_at": tok.ExpiresAt,
		"secret":     secret,
		"link":       link,
	})
}

// APIGuestRemotesRevoke revokes a token so it can no longer authenticate.
func (s *Server) APIGuestRemotesRevoke(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}
	if _, err := s.DB.GuestToken.UpdateOneID(id).SetRevokedAt(time.Now()).Save(s.Ctx); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// APIGuestRemotesDelete permanently deletes a token.
func (s *Server) APIGuestRemotesDelete(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}
	if err := s.DB.GuestToken.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
