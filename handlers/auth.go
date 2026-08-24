package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent/apitoken"
)

type sessionData struct {
	UserID   int
	Username string
	Role     string
	Expiry   time.Time
}

var (
	authMu      sync.Mutex
	sessions    = map[string]sessionData{}
	authEnabled = false
)

// EnableAuth is used by tests to re-enable authentication.
func EnableAuth() {
	authMu.Lock()
	defer authMu.Unlock()
	authEnabled = true
}

func hashSessionToken(pwd string) string {
	h := sha256.Sum256([]byte(pwd))
	return hex.EncodeToString(h[:])
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.Next()
			return
		}
		token := ""
		if t, err := c.Cookie("session"); err == nil {
			token = t
		}
		authMu.Lock()
		sd, valid := sessions[token]
		if valid && sd.Expiry.Before(time.Now()) {
			valid = false
			delete(sessions, token)
		}
		authMu.Unlock()
		if !valid {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) LoginPage(c *gin.Context) {
	data := gin.H{}
	if c.Query("reset") == "1" {
		data["info"] = "Password reset successfully. Please log in with your new password."
	}
	if c.Query("setup") == "1" {
		data["info"] = "Setup complete. Please log in with your new credentials."
	}
	c.HTML(http.StatusOK, "login.html", data)
}

func (s *Server) LoginAction(c *gin.Context) {
	username := c.PostForm("username")
	pass := c.PostForm("password")

	// Try user table first.
	var uid int
	var role string
	var hash string
	foundUser := false
	// Manual case-insensitive search
	users, err := s.DB.User.Query().All(s.Ctx)
	if err == nil {
		for _, u := range users {
			if strings.EqualFold(u.Username, username) {
				uid = u.ID
				role = string(u.Role)
				hash = u.PasswordHash
				foundUser = true
				break
			}
		}
	}
	if !foundUser {
		// Fallback to legacy AdminSettings for migration period.
		admin, err := s.DB.AdminSettings.Query().First(s.Ctx)
		if err != nil {
			c.HTML(http.StatusOK, "login.html", gin.H{"error": "Authentication not configured"})
			return
		}
		if !strings.EqualFold(admin.Username, username) || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(pass)) != nil {
			c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid credentials"})
			return
		}
		// Legacy admin success: treat as admin role with id 0.
		uid = 0
		role = "admin"
		hash = admin.PasswordHash
		foundUser = true
		_ = hash
	} else {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) != nil {
			c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid credentials"})
			return
		}
	}

	token := hashSessionToken(time.Now().String() + username)
	authMu.Lock()
	sessions[token] = sessionData{UserID: uid, Username: username, Role: role, Expiry: time.Now().Add(24 * time.Hour)}
	authMu.Unlock()
	c.SetCookie("session", token, 86400, "/", "", false, true)
	// Update last_login_at if real user.
	if uid != 0 {
		_, _ = s.DB.User.UpdateOneID(uid).SetLastLoginAt(time.Now()).Save(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) LogoutAction(c *gin.Context) {
	if token, err := c.Cookie("session"); err == nil {
		authMu.Lock()
		delete(sessions, token)
		authMu.Unlock()
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// IsAuthenticated reports whether the request carries a valid session cookie
// or a valid bearer API token. When auth is disabled it always returns true.
func (s *Server) IsAuthenticated(c *gin.Context) bool {
	if !authEnabled {
		return true
	}
	if token, err := c.Cookie("session"); err == nil {
		authMu.Lock()
		sd, valid := sessions[token]
		if valid && sd.Expiry.Before(time.Now()) {
			valid = false
			delete(sessions, token)
		}
		authMu.Unlock()
		if valid {
			return true
		}
	}
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		secret := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if secret != "" {
			tok, err := s.DB.ApiToken.Query().Where(apitoken.TokenHashEQ(hashAPIToken(secret))).Only(c.Request.Context())
			if err == nil && tok.RevokedAt == nil && (tok.ExpiresAt == nil || tok.ExpiresAt.After(time.Now())) {
				return true
			}
		}
	}
	return false
}

// RequireAuthMiddleware rejects unauthenticated requests with 401 JSON.
// It accepts either a valid session cookie or a valid bearer token.
func (s *Server) RequireAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.IsAuthenticated(c) {
			c.Next()
			return
		}
		c.Header("WWW-Authenticate", `Bearer realm="LEDit"`)
		c.Header("Cache-Control", "no-store")
		c.Header("Vary", "Cookie, Authorization")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

// AdminPasswordChange page
func (s *Server) AdminPasswordChange(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "password_change.html", gin.H{})
}

func (s *Server) AdminPasswordChangeSave(c *gin.Context) {
	user := c.PostForm("username")
	currentPass := c.PostForm("current_password")
	newPass := c.PostForm("new_password")
	confirmPass := c.PostForm("confirm_password")

	admin, err := s.DB.AdminSettings.Query().First(s.Ctx)
	if err != nil {
		s.renderPage(c, http.StatusOK, "password_change.html", gin.H{"error": "Settings not found"})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(currentPass)) != nil {
		s.renderPage(c, http.StatusOK, "password_change.html", gin.H{"error": "Current password is incorrect"})
		return
	}

	if newPass == "" {
		s.renderPage(c, http.StatusOK, "password_change.html", gin.H{"error": "New password cannot be empty"})
		return
	}

	if newPass != confirmPass {
		s.renderPage(c, http.StatusOK, "password_change.html", gin.H{"error": "New passwords do not match"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		s.renderPage(c, http.StatusOK, "password_change.html", gin.H{"error": "Failed to hash password"})
		return
	}

	_, err = s.DB.AdminSettings.Update().SetUsername(user).SetPasswordHash(string(hash)).Save(s.Ctx)
	if err != nil {
		s.renderPage(c, http.StatusOK, "password_change.html", gin.H{"error": "Failed to save settings"})
		return
	}

	SetFlash(c, "success", "Password changed successfully")
	c.Redirect(http.StatusFound, "/admin/")
}
