package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/apitoken"
)

// Context keys set by APITokenMiddleware.
const (
	ctxAPITokenID    = "api_token_id"
	ctxAPITokenOwner = "api_token_owner_id"
)

// hashAPIToken returns the SHA-256 hex digest of a raw token secret. Only this
// digest is ever persisted; the raw secret is shown once at creation.
func hashAPIToken(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// generateAPIToken returns a new random secret and its display prefix. The
// secret is 32 random bytes (64 hex chars); the prefix is the first 8 chars,
// enough to identify a token in listings without exposing the secret.
func generateAPIToken() (secret, prefix string) {
	b := make([]byte, 32)
	rand.Read(b)
	secret = hex.EncodeToString(b)
	return secret, secret[:8]
}

// APITokenMiddleware authenticates API mutations via an `Authorization: Bearer
// <secret>` header. It validates the token against the stored hash, rejects
// revoked or expired tokens, records last use, and establishes the token's
// owner as the authenticated user. On failure it aborts with 401 and never
// leaks token metadata.
func (s *Server) APITokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			abortUnauthorized(c)
			return
		}
		secret := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if secret == "" {
			abortUnauthorized(c)
			return
		}

		tok, err := s.DB.ApiToken.Query().
			Where(apitoken.TokenHashEQ(hashAPIToken(secret))).
			Only(c.Request.Context())
		if err != nil {
			// Unknown token: reject without revealing whether the token exists.
			abortUnauthorized(c)
			return
		}

		// Revocation check.
		if tok.RevokedAt != nil {
			abortUnauthorized(c)
			return
		}
		// Expiry check.
		if tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now()) {
			abortUnauthorized(c)
			return
		}

		// Record last use (best-effort; never fail the request on write error).
		_, _ = s.DB.ApiToken.UpdateOneID(tok.ID).
			SetLastUsedAt(time.Now()).
			Save(c.Request.Context())

		// Establish the authenticated user as the token's owner.
		c.Set(ctxAPITokenID, tok.ID)
		c.Set(ctxAPITokenOwner, tok.OwnerID)
		c.Next()
	}
}

// abortUnauthorized rejects the request with 401 and a generic message.
func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

// currentAPITokenOwner returns the owner id established by APITokenMiddleware,
// or 0 if the request was not authenticated by a bearer token.
func currentAPITokenOwner(c *gin.Context) int {
	if v, ok := c.Get(ctxAPITokenOwner); ok {
		if id, ok := v.(int); ok {
			return id
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Token lifecycle (owner-only, session-authenticated)
// ---------------------------------------------------------------------------

// apiTokenView is the safe, secret-free view of a token for listings.
type apiTokenView struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func toAPITokenView(t *ent.ApiToken) apiTokenView {
	return apiTokenView{
		ID:         t.ID,
		Name:       t.Name,
		Prefix:     t.TokenPrefix,
		CreatedAt:  t.CreatedAt,
		ExpiresAt:  t.ExpiresAt,
		RevokedAt:  t.RevokedAt,
		LastUsedAt: t.LastUsedAt,
	}
}

// AdminAPITokens renders the token management page.
func (s *Server) AdminAPITokens(c *gin.Context) {
	tokens, err := s.DB.ApiToken.Query().
		Order(ent.Desc(apitoken.FieldCreatedAt)).
		All(s.Ctx)
	if err != nil {
		tokens = nil
	}
	views := make([]apiTokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, toAPITokenView(t))
	}
	s.renderPage(c, http.StatusOK, "api_tokens.html", gin.H{
		"tokens": views,
	})
}

// AdminAPITokenCreate creates a token and returns the one-time secret. The
// secret is never stored; only its hash is persisted.
func (s *Server) AdminAPITokenCreate(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = "default"
	}

	owner, err := s.DB.AdminSettings.Query().First(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no owner configured"})
		return
	}

	secret, prefix := generateAPIToken()
	tok, err := s.DB.ApiToken.Create().
		SetName(name).
		SetTokenHash(hashAPIToken(secret)).
		SetTokenPrefix(prefix).
		SetOwnerID(owner.ID).
		SetCreatedAt(time.Now()).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	// The secret is returned exactly once, in the create response only.
	c.JSON(http.StatusCreated, gin.H{
		"id":     tok.ID,
		"name":   tok.Name,
		"prefix": tok.TokenPrefix,
		"secret": secret,
	})
}

// AdminAPITokenRevoke revokes a token so it can no longer authenticate.
func (s *Server) AdminAPITokenRevoke(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}
	_, err = s.DB.ApiToken.UpdateOneID(id).
		SetRevokedAt(time.Now()).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// AdminAPITokenRotate revokes the current token and issues a replacement with
// a fresh secret. The new secret is returned once, exactly like creation.
func (s *Server) AdminAPITokenRotate(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}

	existing, err := s.DB.ApiToken.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	owner, err := s.DB.AdminSettings.Query().First(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no owner configured"})
		return
	}

	// Revoke the old token, then create a replacement with the same name.
	now := time.Now()
	if _, err := s.DB.ApiToken.UpdateOneID(id).SetRevokedAt(now).Save(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke old token"})
		return
	}

	secret, prefix := generateAPIToken()
	tok, err := s.DB.ApiToken.Create().
		SetName(existing.Name).
		SetTokenHash(hashAPIToken(secret)).
		SetTokenPrefix(prefix).
		SetOwnerID(owner.ID).
		SetCreatedAt(now).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create replacement token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":     tok.ID,
		"name":   tok.Name,
		"prefix": tok.TokenPrefix,
		"secret": secret,
	})
}

// parseIDParam parses the :id route param as an int.
func parseIDParam(c *gin.Context) (int, error) {
	var id int
	raw := c.Param("id")
	if err := json.Unmarshal([]byte(raw), &id); err != nil {
		return 0, err
	}
	return id, nil
}
