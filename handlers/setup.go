package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent/generalsettings"
)

// NeedsSetup reports whether the first-time setup wizard should be shown.
// Setup is required when auth is enabled and any of:
//   - no admin account exists
//   - the admin password is still the default "ledit"
//   - no GeneralSettings row exists
func (s *Server) NeedsSetup() bool {
	if !authEnabled {
		return false
	}
	count, err := s.DB.AdminSettings.Query().Count(s.Ctx)
	if err != nil || count == 0 {
		return true
	}
	exists, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Exist(s.Ctx)
	if err != nil || !exists {
		return true
	}
	admin, err := s.DB.AdminSettings.Query().First(s.Ctx)
	if err == nil {
		if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("ledit")) == nil {
			return true
		}
	}
	return false
}

// SetupMiddleware redirects browser navigation to /setup while setup is required.
func (s *Server) SetupMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// Always allow the wizard itself, static/media, WebSocket feeds, and
		// public health/password-recovery pages — they must stay reachable on a
		// fresh install and for existing tests.
		if path == "/setup" ||
			strings.HasPrefix(path, "/static/") ||
			strings.HasPrefix(path, "/media/") ||
			strings.HasPrefix(path, "/ws/") ||
			path == "/api/health" ||
			path == "/api/trmnl/stats" ||
			path == "/login" || path == "/logout" ||
			strings.HasPrefix(path, "/forgot-password") ||
			strings.HasPrefix(path, "/reset-password") {
			c.Next()
			return
		}
		if s.NeedsSetup() {
			// Authenticated sessions may still access admin (lets existing
			// tests and an admin who already logged in with the default
			// password finish the wizard without being locked out).
			if token, err := c.Cookie("session"); err == nil {
				authMu.Lock()
				_, valid := sessions[token]
				authMu.Unlock()
				if valid {
					c.Next()
					return
				}
			}
			// Only redirect page navigations for the setup-required
			// surfaces (admin and the landing page). Public APIs, feeds,
			// and auth pages stay reachable.
			if c.Request.Method == http.MethodGet &&
				(strings.HasPrefix(path, "/admin") || path == "/") {
				c.Redirect(http.StatusFound, "/setup")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// SetupPage renders the first-time setup wizard.
// If setup is already complete, redirect to /admin.
func (s *Server) SetupPage(c *gin.Context) {
	if !s.NeedsSetup() {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	// Pre-fill current values where available.
	admin, _ := s.DB.AdminSettings.Query().First(s.Ctx)
	username := "admin"
	if admin != nil {
		username = admin.Username
	}
	settings, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx)
	timeout := 5.0
	width := 64
	height := 64
	random := false
	if settings != nil {
		timeout = settings.Timeout
		width = settings.Width
		height = settings.Height
		random = settings.Random
	}
	c.HTML(http.StatusOK, "setup.html", gin.H{
		"username": username,
		"timeout":  timeout,
		"width":    width,
		"height":   height,
		"random":   random,
	})
}

// SetupAction validates and persists the wizard form.
func (s *Server) SetupAction(c *gin.Context) {
	if !s.NeedsSetup() {
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	confirm := c.PostForm("confirm_password")
	timeoutStr := c.PostForm("timeout")
	widthStr := c.PostForm("width")
	heightStr := c.PostForm("height")
	random := c.PostForm("random") == "on"

	if username == "" {
		c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Username is required", "username": username})
		return
	}
	if len(password) < 4 {
		c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Password must be at least 4 characters", "username": username})
		return
	}
	if password != confirm {
		c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Passwords do not match", "username": username})
		return
	}
	timeout, _ := strconv.ParseFloat(timeoutStr, 64)
	if timeout == 0 {
		timeout = 5
	}
	width, _ := strconv.Atoi(widthStr)
	if width == 0 {
		width = 64
	}
	height, _ := strconv.Atoi(heightStr)
	if height == 0 {
		height = 64
	}
	v := NewValidator().
		RangeFloat("Timeout", timeout, 0.1, 3600).
		RangeInt("Width", width, 1, 512).
		RangeInt("Height", height, 1, 512)
	if !v.Valid() {
		c.HTML(http.StatusOK, "setup.html", gin.H{"error": v.Error(), "username": username})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Failed to hash password", "username": username})
		return
	}

	// Upsert admin.
	admin, err := s.DB.AdminSettings.Query().First(s.Ctx)
	if err != nil {
		if _, err := s.DB.AdminSettings.Create().
			SetUsername(username).
			SetPasswordHash(string(hash)).
			Save(s.Ctx); err != nil {
			c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Failed to create admin account", "username": username})
			return
		}
	} else {
		if _, err := s.DB.AdminSettings.UpdateOne(admin).
			SetUsername(username).
			SetPasswordHash(string(hash)).
			Save(s.Ctx); err != nil {
			c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Failed to update admin account", "username": username})
			return
		}
	}

	// Upsert general settings (ID 1).
	exists, _ := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Exist(s.Ctx)
	if !exists {
		_, err = s.DB.GeneralSettings.Create().
			SetTimeout(timeout).
			SetRandom(random).
			SetWidth(width).
			SetHeight(height).
			Save(s.Ctx)
	} else {
		_, err = s.DB.GeneralSettings.UpdateOneID(1).
			SetTimeout(timeout).
			SetRandom(random).
			SetWidth(width).
			SetHeight(height).
			Save(s.Ctx)
	}
	if err != nil {
		c.HTML(http.StatusOK, "setup.html", gin.H{"error": "Failed to save display settings", "username": username})
		return
	}

	c.Redirect(http.StatusFound, "/login?setup=1")
}
