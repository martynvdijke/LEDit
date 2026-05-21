package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	authMu      sync.Mutex
	sessions    = map[string]time.Time{}
	authEnabled = false
	adminUser   = "admin"
	adminPass   = "ledit"
)

func hashPassword(pwd string) string {
	h := sha256.Sum256([]byte(pwd))
	return hex.EncodeToString(h[:])
}

func init() {
	// Default credentials
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
	if user == adminUser && pass == adminPass {
		token := hashPassword(time.Now().String())
		authMu.Lock()
		sessions[token] = time.Now().Add(24 * time.Hour)
		authMu.Unlock()
		c.SetCookie("session", token, 86400, "/", "", false, true)
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid credentials"})
}

func (s *Server) LogoutAction(c *gin.Context) {
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}
