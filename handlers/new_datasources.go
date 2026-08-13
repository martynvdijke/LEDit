package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
)

// ---------------------------------------------------------------------------
// Google Calendar
// ---------------------------------------------------------------------------

func (s *Server) AdminGoogleCalendarNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "datasource_form.html", gin.H{
		"type":     "Google Calendar",
		"endpoint": "googlecalendar",
		"has_name": true,
	})
}
func (s *Server) AdminGoogleCalendarCreate(c *gin.Context) { s.createFieldDS(c, "googlecalendar") }
func (s *Server) AdminGoogleCalendarEdit(c *gin.Context)   { s.editFieldDS(c, "googlecalendar", nil) }
func (s *Server) AdminGoogleCalendarUpdate(c *gin.Context) { s.updateFieldDS(c, "googlecalendar") }
func (s *Server) AdminGoogleCalendarDelete(c *gin.Context) { s.deleteFieldDS(c, "googlecalendar") }

// ---------------------------------------------------------------------------
// News (aggregated RSS feeds)
// ---------------------------------------------------------------------------

func (s *Server) AdminNewsFeedNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "datasource_form.html", gin.H{
		"type":     "News",
		"endpoint": "newsfeed",
		"has_name": true,
	})
}
func (s *Server) AdminNewsFeedCreate(c *gin.Context) { s.createFieldDS(c, "newsfeed") }
func (s *Server) AdminNewsFeedEdit(c *gin.Context)   { s.editFieldDS(c, "newsfeed", nil) }
func (s *Server) AdminNewsFeedUpdate(c *gin.Context) { s.updateFieldDS(c, "newsfeed") }
func (s *Server) AdminNewsFeedDelete(c *gin.Context) { s.deleteFieldDS(c, "newsfeed") }

// ---------------------------------------------------------------------------
// Custom API (generic JSON API)
// ---------------------------------------------------------------------------

func (s *Server) AdminGenericAPINew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "datasource_form.html", gin.H{
		"type":        "Custom API",
		"endpoint":    "genericapi",
		"has_config":  true,
		"config_hint": `{"title":"BTC","headers":{"Accept":"application/json"},"rows":[{"label":"Price","path":"bitcoin.usd"}]}`,
	})
}
func (s *Server) AdminGenericAPICreate(c *gin.Context) { s.createFieldDS(c, "genericapi") }
func (s *Server) AdminGenericAPIEdit(c *gin.Context)   { s.editFieldDS(c, "genericapi", nil) }
func (s *Server) AdminGenericAPIUpdate(c *gin.Context) { s.updateFieldDS(c, "genericapi") }
func (s *Server) AdminGenericAPIDelete(c *gin.Context) { s.deleteFieldDS(c, "genericapi") }

// AdminGenericAPITest is the test-preview action on the Custom API form: it
// fetches the configured URL with the current form values (not yet saved) and
// returns the extracted rows or an error, without touching the database.
func (s *Server) AdminGenericAPITest(c *gin.Context) {
	ds := &datasource.GenericAPIDS{
		Token:  c.PostForm("token"),
		URL:    c.PostForm("url"),
		Config: c.PostForm("config"),
	}
	title, rows, err := ds.Extract()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "title": title, "rows": rows})
}
