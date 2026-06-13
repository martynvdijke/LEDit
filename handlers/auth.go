package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

var (
	authMu      sync.Mutex
	sessions    = map[string]time.Time{}
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
		_, valid := sessions[token]
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
	c.HTML(http.StatusOK, "login.html", gin.H{})
}

func (s *Server) LoginAction(c *gin.Context) {
	user := c.PostForm("username")
	pass := c.PostForm("password")

	admin, err := s.DB.AdminSettings.Query().First(s.Ctx)
	if err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Authentication not configured"})
		return
	}

	if user != admin.Username || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(pass)) != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid credentials"})
		return
	}

	token := hashSessionToken(time.Now().String())
	authMu.Lock()
	sessions[token] = time.Now().Add(24 * time.Hour)
	authMu.Unlock()
	c.SetCookie("session", token, 86400, "/", "", false, true)
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) LogoutAction(c *gin.Context) {
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
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
