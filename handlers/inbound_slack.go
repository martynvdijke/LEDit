package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent/inboundadapter"
)

const slackBodyCap = 128 * 1024

func (s *Server) InboundSlack(c *gin.Context) {
	raw, ok := readSlackBody(c)
	if !ok {
		return
	}
	cfgRow, err := s.DB.InboundAdapter.Query().Where(inboundadapter.KindEQ("slack")).Only(s.Ctx)
	if err != nil || cfgRow == nil || !cfgRow.Enabled || strings.TrimSpace(cfgRow.Secret) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	cfg := inboundConfigFromRow(cfgRow)

	sig := c.GetHeader("X-Slack-Signature")
	tsStr := c.GetHeader("X-Slack-Request-Timestamp")
	if sig == "" || tsStr == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	tsStr = strings.TrimSpace(tsStr)
	tsInt, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ts := time.Unix(tsInt, 0)
	if d := time.Since(ts); d > 300*time.Second || d < -300*time.Second {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	base := "v0:" + tsStr + ":" + string(raw)
	mac := hmac.New(sha256.New, []byte(cfg.Secret))
	mac.Write([]byte(base))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ct := c.GetHeader("Content-Type")
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		handleSlackSlash(c, s, cfg, raw)
		return
	}
	handleSlackJSON(c, s, cfg, raw)
}

func readSlackBody(c *gin.Context) ([]byte, bool) {
	limited := io.LimitReader(c.Request.Body, slackBodyCap+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return nil, false
	}
	if len(raw) > slackBodyCap {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "body_too_large"})
		return nil, false
	}
	return raw, true
}

func handleSlackJSON(c *gin.Context, s *Server, cfg InboundAdapterConfig, raw []byte) {
	var env struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Event     *struct {
			Type    string `json:"type"`
			SubType string `json:"subtype"`
			BotID   string `json:"bot_id"`
			Channel string `json:"channel"`
			User    string `json:"user"`
			Text    string `json:"text"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	switch env.Type {
	case "url_verification":
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, gin.H{"challenge": env.Challenge})
		return
	case "event_callback":
		if env.Event == nil || env.Event.Type != "message" {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		if env.Event.BotID != "" || env.Event.SubType != "" {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		channel := env.Event.Channel
		user := env.Event.User
		allowed := inboundAllowed(cfg.Allowlist, channel) || inboundAllowed(cfg.Allowlist, user)
		if !allowed {
			dropInbound("slack", "allowlist")
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		msg := InboundMessage{Source: "slack", SourceID: channel, SourceName: "Slack", Body: env.Event.Text, Priority: inboundPriorityNormal}
		if ok, retry := admitInbound(cfg, msg); !ok {
			if retry > 0 {
				dropInbound("slack", "rate_limited")
			} else {
				dropInbound("slack", "admission")
			}
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		s.DeliverInbound(msg)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	default:
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
}

func handleSlackSlash(c *gin.Context, s *Server, cfg InboundAdapterConfig, raw []byte) {
	vals, err := url.ParseQuery(string(raw))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"response_type": "ephemeral", "text": "bad request"})
		return
	}
	cmd := strings.TrimSpace(vals.Get("command"))
	if cmd == "" {
		var probe map[string]string
		if jsonErr := json.Unmarshal(raw, &probe); jsonErr == nil {
			cmd = strings.TrimSpace(probe["command"])
		}
	}
	channelID := vals.Get("channel_id")
	if channelID == "" {
		channelID = vals.Get("channel")
	}
	userID := vals.Get("user_id")
	if userID == "" {
		userID = vals.Get("user")
	}
	allowed := inboundAllowed(cfg.Allowlist, channelID) || inboundAllowed(cfg.Allowlist, userID)
	if !allowed {
		dropInbound("slack", "allowlist")
		c.JSON(http.StatusOK, gin.H{"response_type": "ephemeral", "text": "not allowed"})
		return
	}
	text := strings.TrimSpace(vals.Get("text"))
	var respText string
	switch cmd {
	case "/display":
		if text == "" {
			respText = "usage: /display <text>"
		} else {
			msg := InboundMessage{Source: "slack", SourceID: channelID, SourceName: "Slack", Body: text, Priority: inboundPriorityNormal}
			if ok, _ := admitInbound(cfg, msg); ok {
				s.DeliverInbound(msg)
				respText = "displayed: " + text
			} else {
				dropInbound("slack", "rate_limited")
				respText = "rate limited, try again"
			}
		}
	case "/next":
		GlobalFeed.Next()
		respText = "skipped to next"
	case "/pause":
		GlobalFeed.Pause()
		respText = "paused"
	case "/resume":
		GlobalFeed.Resume()
		respText = "resumed"
	case "/status":
		st := GlobalFeed.Status()
		paused := st["paused"]
		cur, _ := st["current"].(string)
		next, _ := st["next"].(string)
		paStr := ""
		if b, ok := paused.(bool); ok {
			if b {
				paStr = "true"
			} else {
				paStr = "false"
			}
		}
		respText = "paused=" + paStr + " current=" + cur + " next=" + next
	default:
		respText = "unknown command: " + cmd
	}
	c.JSON(http.StatusOK, gin.H{"response_type": "ephemeral", "text": respText})
}
