package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const einkCookieName = "ledit_eink"
const einkRefreshCookie = "ledit_eink_refresh"
const einkCookieMaxAge = 365 * 24 * 60 * 60 // 1 year

// EInkMiddleware reads the ledit_eink cookie and injects EInkMode into the request context.
// It also sets a default from server settings if no cookie is present.
func (s *Server) EInkMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		einkMode := false

		// Check cookie first (takes precedence)
		cookieVal, err := c.Cookie(einkCookieName)
		if err == nil {
			einkMode = cookieVal == "true"
		} else {
			// Fall back to server-side default setting
			settings, err := s.DB.GeneralSettings.Query().First(s.Ctx)
			if err == nil {
				einkMode = settings.EinkMode
			}
		}

		c.Set("eink_mode", einkMode)
		c.Next()
	}
}

// Helper to check e-ink mode from gin context
func getEInkMode(c *gin.Context) bool {
	if v, ok := c.Get("eink_mode"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// AdminEInkToggle toggles the e-ink mode cookie.
func (s *Server) AdminEInkToggle(c *gin.Context) {
	current := getEInkMode(c)

	if current {
		// Clear cookie (disable e-ink)
		c.SetCookie(einkCookieName, "", -1, "/", "", false, true)
	} else {
		// Set cookie (enable e-ink)
		c.SetCookie(einkCookieName, "true", einkCookieMaxAge, "/", "", false, true)
	}

	// Also set/remove the eink_mode in context for this request
	c.Set("eink_mode", !current)

	// Redirect back to referrer, or admin home
	referrer := c.GetHeader("Referer")
	if referrer == "" {
		referrer = "/admin/"
	}
	c.Redirect(http.StatusFound, referrer)
}

// AdminEInkToggleFeed handles e-ink toggle from the feed page (no auth required).
func (s *Server) AdminEInkToggleFeed(c *gin.Context) {
	current := getEInkMode(c)

	if current {
		c.SetCookie(einkCookieName, "", -1, "/", "", false, true)
	} else {
		c.SetCookie(einkCookieName, "true", einkCookieMaxAge, "/", "", false, true)
	}

	c.Set("eink_mode", !current)

	referrer := c.GetHeader("Referer")
	if referrer == "" {
		referrer = "/"
	}
	c.Redirect(http.StatusFound, referrer)
}

// AdminEInkRefresh sets the e-ink refresh interval cookie.
func (s *Server) AdminEInkRefresh(c *gin.Context) {
	intervalStr := c.DefaultPostForm("interval", "30")
	interval, err := strconv.Atoi(intervalStr)
	if err != nil || interval < 5 || interval > 3600 {
		interval = 30
	}
	c.SetCookie(einkRefreshCookie, strconv.Itoa(interval), einkCookieMaxAge, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}
