package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"ledit/ent/inboundadapter"
)

func (s *Server) InboundPush(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		row, err := s.DB.InboundAdapter.Query().Where(inboundadapter.KindEQ(kind)).Only(s.Ctx)
		if err != nil || row == nil || !row.Enabled {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		got := c.GetHeader("X-Inbound-Secret")
		if got == "" {
			got = c.Query("secret")
		}
		if got == "" && kind == "ntfy" {
			ah := c.GetHeader("Authorization")
			if strings.HasPrefix(ah, "Bearer ") {
				got = strings.TrimSpace(strings.TrimPrefix(ah, "Bearer "))
			}
		}
		if !inboundSecretOK(row.Secret, got) {
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "body_too_large"})
			return
		}

		var title, msgBody string
		var priority int

		switch kind {
		case "ntfy":
			var p struct {
				Title    string `json:"title"`
				Message  string `json:"message"`
				Priority int    `json:"priority"`
			}
			if err := json.Unmarshal(body, &p); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
				return
			}
			title = p.Title
			if title == "" {
				title = "ntfy"
			}
			msgBody = p.Message
			if p.Priority != 0 {
				priority = inboundPriorityFromNtfy(p.Priority)
			} else {
				priority = inboundPriorityFromNtfy(3)
			}
		case "gotify":
			var p struct {
				Title    string `json:"title"`
				Message  string `json:"message"`
				Priority int    `json:"priority"`
			}
			if err := json.Unmarshal(body, &p); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
				return
			}
			title = p.Title
			if title == "" {
				title = "Gotify"
			}
			msgBody = p.Message
			priority = inboundPriorityFromGotify(p.Priority)
		case "pushover":
			vals, err := url.ParseQuery(string(body))
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
				return
			}
			// Also handle if body was form with proper encoding via ParseQuery is enough.
			title = vals.Get("title")
			if title == "" {
				title = "Pushover"
			}
			msgBody = vals.Get("message")
			pStr := vals.Get("priority")
			pInt := 0
			if pStr != "" {
				f, ferr := strconv.ParseFloat(strings.TrimSpace(pStr), 64)
				if ferr == nil {
					pInt = int(f)
				}
			}
			priority = inboundPriorityFromPushover(pInt)
		default:
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}

		title = strings.TrimSpace(title)
		msgBody = strings.TrimSpace(msgBody)
		if msgBody == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "empty_body"})
			return
		}
		if len(title) > 200 {
			title = title[:200]
		}
		if len(msgBody) > 2000 {
			msgBody = msgBody[:2000]
		}

		cfg := inboundConfigFromRow(row)
		imsg := InboundMessage{
			Source:     kind,
			SourceID:   c.ClientIP(),
			SourceName: capitalizeKind(kind),
			Title:      title,
			Body:       msgBody,
			Priority:   priority,
		}
		if ok, retry := admitInbound(cfg, imsg); !ok {
			abortInboundRateLimited(c, retry)
			return
		}
		s.DeliverInbound(imsg)
		c.JSON(http.StatusOK, gin.H{"status": "delivered"})
	}
}

func capitalizeKind(k string) string {
	if k == "" {
		return ""
	}
	return strings.ToUpper(k[:1]) + k[1:]
}
