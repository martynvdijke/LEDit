package handlers

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent/user"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type userView struct {
	ID          int        `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func toUserView(u *userView) {}

func (s *Server) AdminUsersPage(c *gin.Context) {
	users, _ := s.DB.User.Query().All(s.Ctx)
	// count admins
	adminCount := 0
	for _, u := range users {
		if u.Role == user.RoleAdmin {
			adminCount++
		}
	}
	views := make([]userView, 0, len(users))
	for _, u := range users {
		views = append(views, userView{ID: u.ID, Username: u.Username, Role: string(u.Role), CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt})
	}
	s.renderPage(c, http.StatusOK, "users.html", gin.H{
		"users":      views,
		"adminCount": adminCount,
	})
}

func (s *Server) APIUsersList(c *gin.Context) {
	users, _ := s.DB.User.Query().All(s.Ctx)
	views := make([]userView, 0, len(users))
	for _, u := range users {
		views = append(views, userView{ID: u.ID, Username: u.Username, Role: string(u.Role), CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt})
	}
	c.JSON(http.StatusOK, views)
}

func (s *Server) APIUsersCreate(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	if username == "" {
		username = strings.TrimSpace(c.Query("username"))
	}
	if username == "" {
		// try JSON
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Username != "" {
			username = body.Username
			// handle below
			password := body.Password
			roleStr := body.Role
			s.createUser(c, username, password, roleStr)
			return
		}
	}
	password := c.PostForm("password")
	roleStr := c.PostForm("role")
	s.createUser(c, username, password, roleStr)
}

func (s *Server) createUser(c *gin.Context, username, password, roleStr string) {
	if len(username) < 3 || len(username) > 64 || !usernameRegex.MatchString(username) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid username"})
		return
	}
	if len(password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}
	if roleStr == "" {
		roleStr = "viewer"
	}
	if roleStr != "admin" && roleStr != "viewer" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	// duplicate check case-insensitive
	existing, _ := s.DB.User.Query().All(s.Ctx)
	for _, u := range existing {
		if strings.EqualFold(u.Username, username) {
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate username"})
			return
		}
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	var role user.Role
	if roleStr == "admin" {
		role = user.RoleAdmin
	} else {
		role = user.RoleViewer
	}
	u, err := s.DB.User.Create().SetUsername(username).SetPasswordHash(string(hash)).SetRole(role).SetCreatedAt(time.Now()).Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	c.JSON(http.StatusCreated, userView{ID: u.ID, Username: u.Username, Role: string(u.Role), CreatedAt: u.CreatedAt})
}

func (s *Server) APIUsersDelete(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	u, err := s.DB.User.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// check last admin
	if u.Role == user.RoleAdmin {
		count, _ := s.DB.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(s.Ctx)
		if count <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete last admin"})
			return
		}
	}
	_ = s.DB.User.DeleteOneID(id).Exec(s.Ctx)
	// clear sessions for that user
	authMu.Lock()
	for tok, sd := range sessions {
		if sd.UserID == id {
			delete(sessions, tok)
		}
	}
	authMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) APIUsersChangeRole(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	roleStr := c.PostForm("role")
	if roleStr == "" {
		var body struct {
			Role string `json:"role"`
		}
		_ = c.ShouldBindJSON(&body)
		roleStr = body.Role
	}
	if roleStr != "admin" && roleStr != "viewer" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	u, err := s.DB.User.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if u.Role == user.RoleAdmin && roleStr == "viewer" {
		count, _ := s.DB.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(s.Ctx)
		if count <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote last admin"})
			return
		}
	}
	var role user.Role
	if roleStr == "admin" {
		role = user.RoleAdmin
	} else {
		role = user.RoleViewer
	}
	_, _ = s.DB.User.UpdateOneID(id).SetRole(role).Save(s.Ctx)
	// force re-login
	authMu.Lock()
	for tok, sd := range sessions {
		if sd.UserID == id {
			delete(sessions, tok)
		}
	}
	authMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (s *Server) APIUsersResetPassword(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	password := c.PostForm("password")
	if password == "" {
		var body struct {
			Password string `json:"password"`
		}
		_ = c.ShouldBindJSON(&body)
		password = body.Password
	}
	if len(password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	_, err = s.DB.User.UpdateOneID(id).SetPasswordHash(string(hash)).Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// clear sessions
	authMu.Lock()
	for tok, sd := range sessions {
		if sd.UserID == id {
			delete(sessions, tok)
		}
	}
	authMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "password reset"})
}
