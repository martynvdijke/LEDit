package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent"
	"ledit/ent/apitoken"
	"ledit/ent/websession"
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

// initSessionStore gives the session helpers a database handle so logins
// survive restarts. The in-memory map stays as a hot cache and still works in
// tests that do not create an ent client.
func initSessionStore() {
	authMu.Lock()
	defer authMu.Unlock()
	sessions = map[string]sessionData{}
}

// EnableAuth is used by tests to re-enable authentication.
func EnableAuth() {
	authMu.Lock()
	defer authMu.Unlock()
	authEnabled = true
}

func hashSessionToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func newSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// putSession creates a random session token, persists its hash, and caches it.
func putSession(db *ent.Client, sd sessionData) (string, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", err
	}
	hash := hashSessionToken(token)
	authMu.Lock()
	defer authMu.Unlock()
	if db != nil {
		if _, err := db.WebSession.Create().
			SetTokenHash(hash).
			SetUserID(sd.UserID).
			SetUsername(sd.Username).
			SetRole(sd.Role).
			SetExpiry(sd.Expiry).
			Save(context.Background()); err != nil {
			return "", err
		}
	}
	sessions[hash] = sd
	return token, nil
}

// lookupSession returns a cached session or reloads it from the database.
func lookupSession(db *ent.Client, token string) (sessionData, bool) {
	if token == "" {
		return sessionData{}, false
	}
	hash := hashSessionToken(token)
	now := time.Now()
	authMu.Lock()
	if sd, ok := sessions[hash]; ok {
		if sd.Expiry.After(now) {
			authMu.Unlock()
			return sd, true
		}
		delete(sessions, hash)
	}
	authMu.Unlock()
	if db == nil {
		return sessionData{}, false
	}
	row, err := db.WebSession.Query().Where(websession.TokenHashEQ(hash)).Only(context.Background())
	if err != nil {
		return sessionData{}, false
	}
	if !row.Expiry.After(now) {
		_ = db.WebSession.DeleteOneID(row.ID).Exec(context.Background())
		return sessionData{}, false
	}
	sd := sessionData{UserID: row.UserID, Username: row.Username, Role: row.Role, Expiry: row.Expiry}
	authMu.Lock()
	sessions[hash] = sd
	authMu.Unlock()
	return sd, true
}

func deleteSession(db *ent.Client, token string) {
	if token == "" {
		return
	}
	hash := hashSessionToken(token)
	authMu.Lock()
	delete(sessions, hash)
	authMu.Unlock()
	if db != nil {
		_, _ = db.WebSession.Delete().Where(websession.TokenHashEQ(hash)).Exec(context.Background())
	}
}

func clearUserSessions(db *ent.Client, userID int) {
	authMu.Lock()
	for hash, sd := range sessions {
		if sd.UserID == userID {
			delete(sessions, hash)
		}
	}
	authMu.Unlock()
	if db != nil {
		_, _ = db.WebSession.Delete().Where(websession.UserID(userID)).Exec(context.Background())
	}
}

func clearAllSessions(db *ent.Client) {
	authMu.Lock()
	sessions = map[string]sessionData{}
	authMu.Unlock()
	if db != nil {
		_, _ = db.WebSession.Delete().Exec(context.Background())
	}
}

func sessionCookieSecure(c *gin.Context) bool {
	if v := strings.TrimSpace(strings.ToLower(c.GetHeader("X-Forwarded-Proto"))); v != "" {
		return v == "https"
	}
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("COOKIE_SECURE"))); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	return c.Request.TLS != nil
}

func setSessionCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("session", token, maxAge, "/", "", sessionCookieSecure(c), true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("session", "", -1, "/", "", sessionCookieSecure(c), true)
}

func (s *Server) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.Next()
			return
		}
		token, err := c.Cookie("session")
		if err != nil {
			token = ""
		}
		if _, valid := lookupSession(s.DB, token); !valid {
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
	data["OIDCEnabled"] = oidcEnabled()
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

	token, err := putSession(s.DB, sessionData{UserID: uid, Username: username, Role: role, Expiry: time.Now().Add(24 * time.Hour)})
	if err != nil {
		slog.Error("failed to persist session", "error", err)
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Unable to start session"})
		return
	}
	setSessionCookie(c, token, 86400)
	// Update last_login_at if real user.
	if uid != 0 {
		_, _ = s.DB.User.UpdateOneID(uid).SetLastLoginAt(time.Now()).Save(s.Ctx)
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) LogoutAction(c *gin.Context) {
	if token, err := c.Cookie("session"); err == nil {
		deleteSession(s.DB, token)
	}
	clearSessionCookie(c)
	c.Redirect(http.StatusFound, "/login")
}

// IsAuthenticated reports whether the request carries a valid session cookie
// or a valid bearer API token. When auth is disabled it always returns true.
func (s *Server) IsAuthenticated(c *gin.Context) bool {
	if !authEnabled {
		return true
	}
	if token, err := c.Cookie("session"); err == nil {
		if _, valid := lookupSession(s.DB, token); valid {
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
