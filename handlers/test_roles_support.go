package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent/user"
)

func (s *Server) TestEnableAuth(c *gin.Context) {
	authEnabled = true
	seedUsers(s.DB, s.Ctx)
	// When LEDIT_AUTH_DISABLE=true, AdminSettings was never seeded, so seedUsers
	// creates no user. Ensure an admin user exists for E2E.
	if cnt, _ := s.DB.User.Query().Count(s.Ctx); cnt == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte("ledit"), bcrypt.DefaultCost)
		_, _ = s.DB.User.Create().SetUsername("admin").SetPasswordHash(string(hash)).SetRole(user.RoleAdmin).SetCreatedAt(time.Now()).Save(s.Ctx)
		// also ensure AdminSettings exists for token owner
		if _, err := s.DB.AdminSettings.Query().First(s.Ctx); err != nil {
			_, _ = s.DB.AdminSettings.Create().SetUsername("admin").SetPasswordHash(string(hash)).Save(s.Ctx)
		}
	}
	c.JSON(http.StatusOK, gin.H{"authEnabled": true})
}
