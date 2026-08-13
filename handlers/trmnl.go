package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ledit/datasource"
)

// APITrmnlStats serves system stats and display analytics as JSON for TRMNL
// e-ink displays. Read-only and unauthenticated by design — it exposes no
// secrets, only the same low-sensitivity stats already rendered into the
// public LED feed.
func (s *Server) APITrmnlStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"system":    datasource.GetSystemStats(),
		"analytics": GetAnalytics(),
	})
}
