package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/apitoken"
	"ledit/ent/user"
)

const ctxAPITokenRole = "api_token_role"

// CurrentUser returns the authenticated user for the request, if any.
func (s *Server) CurrentUser(c *gin.Context) (*ent.User, bool) {
	if !authEnabled {
		return nil, false
	}
	// Try session first.
	if token, err := c.Cookie("session"); err == nil {
		authMu.Lock()
		sd, ok := sessions[token]
		authMu.Unlock()
		if ok && sd.Expiry.After(time.Now()) {
			u, err := s.DB.User.Get(c.Request.Context(), sd.UserID)
			if err == nil {
				return u, true
			}
			// fallback to lookup by username if ID stale
			u2, err2 := s.DB.User.Query().Where(user.UsernameEQ(sd.Username)).Only(c.Request.Context())
			if err2 == nil {
				return u2, true
			}
		}
	}
	// Try bearer token.
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		secret := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if secret != "" {
			tok, err := s.DB.ApiToken.Query().Where(apitoken.TokenHashEQ(hashAPIToken(secret))).Only(c.Request.Context())
			if err == nil && tok.RevokedAt == nil && (tok.ExpiresAt == nil || tok.ExpiresAt.After(time.Now())) {
				// Return a synthetic user representing the token owner role?
				// For token auth we don't have a user row, but we still need role check.
				// Return nil, caller should use token role directly.
				_ = tok
			}
		}
	}
	return nil, false
}

func abortInsufficientRole(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient_role", "code": "insufficient_role"})
}

// getSessionRole returns role of session if valid, else "".
func getSessionRole(c *gin.Context) string {
	token, err := c.Cookie("session")
	if err != nil {
		return ""
	}
	authMu.Lock()
	sd, ok := sessions[token]
	authMu.Unlock()
	if !ok || sd.Expiry.Before(time.Now()) {
		return ""
	}
	return sd.Role
}

// getBearerRole returns role of bearer token if valid, else "".
func (s *Server) getBearerRole(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	secret := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if secret == "" {
		return ""
	}
	tok, err := s.DB.ApiToken.Query().Where(apitoken.TokenHashEQ(hashAPIToken(secret))).Only(c.Request.Context())
	if err != nil || tok.RevokedAt != nil || (tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now())) {
		return ""
	}
	return string(tok.Role)
}

// isAuthenticatedWithRole checks if request has at least the required role.
// required = "viewer" means viewer or admin suffices; "admin" means admin only.
func (s *Server) isAuthenticatedWithRole(c *gin.Context, required string) (authenticated bool, role string) {
	if !authEnabled {
		return true, "admin"
	}
	// Session has priority.
	if r := getSessionRole(c); r != "" {
		if required == "viewer" {
			return true, r
		}
		if required == "admin" && r == "admin" {
			return true, r
		}
		if required == "admin" && r != "admin" {
			return true, r // authenticated but insufficient
		}
	}
	if r := s.getBearerRole(c); r != "" {
		return true, r
	}
	// Check if any valid session or token exists but with wrong role handled above.
	// Determine if completely unauthenticated.
	if getSessionRole(c) == "" && s.getBearerRole(c) == "" {
		// also check raw session existence for unauthenticated vs insufficient
		if token, err := c.Cookie("session"); err == nil {
			authMu.Lock()
			_, ok := sessions[token]
			authMu.Unlock()
			if ok {
				// expired etc handled
			}
		}
		return false, ""
	}
	return true, ""
}

// RequireViewer allows viewer or admin.
func (s *Server) RequireViewer() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.Next()
			return
		}
		// Check session.
		if r := getSessionRole(c); r == "viewer" || r == "admin" {
			c.Set("user_role", r)
			c.Next()
			return
		}
		// Check bearer.
		if r := s.getBearerRole(c); r == "viewer" || r == "admin" {
			c.Set("user_role", r)
			c.Set(ctxAPITokenRole, r)
			c.Next()
			return
		}
		// Determine if unauthenticated vs insufficient (viewer always sufficient).
		// If no valid auth, 401.
		c.Header("WWW-Authenticate", `Bearer realm="LEDit"`)
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

// AdminRoleMiddleware enforces viewer for GET, admin for mutations inside /admin group.
func (s *Server) AdminRoleMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.Next()
			return
		}
		// Users page is admin-only even for GET.
		if c.Request.URL.Path == "/admin/users" || strings.HasPrefix(c.Request.URL.Path, "/admin/users") || strings.HasPrefix(c.Request.URL.Path, "/admin/api/users") {
			r := getSessionRole(c)
			if r == "" {
				r = s.getBearerRole(c)
			}
			if r == "" {
				c.Redirect(http.StatusFound, "/login")
				c.Abort()
				return
			}
			if r != "admin" {
				// For page, redirect to /admin with flash; for API, 403.
				if strings.HasPrefix(c.Request.URL.Path, "/admin/api/") {
					abortInsufficientRole(c)
				} else {
					SetFlash(c, "danger", "Viewer access: read-only")
					c.Redirect(http.StatusFound, "/admin/")
					c.Abort()
				}
				return
			}
			c.Set("user_role", r)
			c.Next()
			return
		}
		// API tokens management is admin-only.
		if strings.HasPrefix(c.Request.URL.Path, "/admin/api-tokens") {
			r := getSessionRole(c)
			if r == "" {
				r = s.getBearerRole(c)
			}
			if r == "" {
				c.Redirect(http.StatusFound, "/login")
				c.Abort()
				return
			}
			if r != "admin" {
				abortInsufficientRole(c)
				return
			}
			c.Set("user_role", r)
			c.Next()
			return
		}
		if c.Request.Method == http.MethodGet {
			// require viewer (viewer or admin)
			if r := getSessionRole(c); r == "viewer" || r == "admin" {
				c.Set("user_role", r)
				c.Next()
				return
			}
			if r := s.getBearerRole(c); r == "viewer" || r == "admin" {
				c.Set("user_role", r)
				c.Next()
				return
			}
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		// Mutations require admin
		if r := getSessionRole(c); r != "" {
			if r == "admin" {
				c.Set("user_role", r)
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient_role", "code": "insufficient_role"})
			return
		}
		if r := s.getBearerRole(c); r != "" {
			if r == "admin" {
				c.Set("user_role", r)
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient_role", "code": "insufficient_role"})
			return
		}
		c.Header("WWW-Authenticate", `Bearer realm="LEDit"`)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

// RequireAdmin allows only admin.
func (s *Server) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.Next()
			return
		}
		if r := getSessionRole(c); r != "" {
			if r == "admin" {
				c.Set("user_role", r)
				c.Next()
				return
			}
			abortInsufficientRole(c)
			return
		}
		if r := s.getBearerRole(c); r != "" {
			if r == "admin" {
				c.Set("user_role", r)
				c.Set(ctxAPITokenRole, r)
				// also record token owner for api_token middleware compatibility
				auth := c.GetHeader("Authorization")
				secret := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
				if tok, err := s.DB.ApiToken.Query().Where(apitoken.TokenHashEQ(hashAPIToken(secret))).Only(c.Request.Context()); err == nil {
					c.Set(ctxAPITokenID, tok.ID)
					c.Set(ctxAPITokenOwner, tok.OwnerID)
				}
				c.Next()
				return
			}
			abortInsufficientRole(c)
			return
		}
		c.Header("WWW-Authenticate", `Bearer realm="LEDit"`)
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}
