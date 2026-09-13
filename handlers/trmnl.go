package handlers

import (
	"net/http"
	"time"

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

// APITrmnlMessages serves active unified messages to the TRMNL/e-ink poller.
// Public exactly like /api/trmnl/stats; no ETag, so Cache-Control: no-store
// keeps every poll fresh (the refresh trigger is therefore unnecessary).
func (s *Server) APITrmnlMessages(c *gin.Context) {
	msgs := ActiveMessages()
	if msgs == nil {
		msgs = []Message{}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"messages":     msgs,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// APITrmnlMessagesRefresh is a no-op cache-buster for pollers that explicitly
// request fresh data before their next GET. The messages endpoint is always
// computed live, so there is nothing to invalidate.
func (s *Server) APITrmnlMessagesRefresh(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
