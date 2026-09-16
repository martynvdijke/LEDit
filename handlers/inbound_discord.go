package handlers

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/ent/inboundadapter"
)

func init() { registerInboundFactory("discord", newDiscordInbound) }

func newDiscordInbound(s *Server, cfg InboundAdapterConfig) InboundAdapter {
	return &DiscordInbound{s: s, cfg: cfg, stop: make(chan struct{}), stopped: make(chan struct{})}
}

var discordDial = func(url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(url, header)
}

type DiscordInbound struct {
	s       *Server
	cfg     InboundAdapterConfig
	stop    chan struct{}
	stopped chan struct{}
	mu      sync.Mutex
}

func (d *DiscordInbound) Kind() string { return "discord" }

func (d *DiscordInbound) Start() error {
	go d.Run()
	return nil
}

func (d *DiscordInbound) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
}

func (d *DiscordInbound) Run() {
	defer close(d.stopped)
	attempt := 0
	gatewayURL := d.cfg.Config["gateway_url"]
	if gatewayURL == "" {
		gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		if err := d.connectAndLoop(gatewayURL); err != nil {
			// reconnect with backoff
			select {
			case <-d.stop:
				return
			case <-time.After(backoffDelay(attempt)):
			}
			attempt++
			continue
		}
		attempt = 0
	}
}

func (d *DiscordInbound) connectAndLoop(gatewayURL string) error {
	conn, _, err := discordDial(gatewayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Read Hello
	var hello struct {
		Op int `json:"op"`
		D  struct {
			HeartbeatInterval int `json:"heartbeat_interval"`
		} `json:"d"`
	}
	if err := conn.ReadJSON(&hello); err != nil {
		return err
	}
	if hello.Op != 10 {
		// unexpected, continue
	}

	// Send Identify
	identify := map[string]any{
		"op": 2,
		"d": map[string]any{
			"token":   d.cfg.Secret,
			"intents": 1<<9 | 1<<15,
			"properties": map[string]string{
				"os": "linux", "browser": "ledit", "device": "ledit",
			},
		},
	}
	if err := conn.WriteJSON(identify); err != nil {
		return err
	}

	// Heartbeat
	interval := time.Duration(hello.D.HeartbeatInterval) * time.Millisecond
	if interval <= 0 {
		interval = 41250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-d.stop:
				return
			case <-ticker.C:
				_ = conn.WriteJSON(map[string]any{"op": 1, "d": nil})
			}
		}
	}()

	for {
		select {
		case <-d.stop:
			return nil
		default:
		}
		var raw struct {
			Op int             `json:"op"`
			T  string          `json:"t"`
			D  json.RawMessage `json:"d"`
		}
		if err := conn.ReadJSON(&raw); err != nil {
			return err
		}
		if raw.Op == 11 {
			continue
		}
		if raw.Op == 1 {
			_ = conn.WriteJSON(map[string]any{"op": 11, "d": nil})
			continue
		}
		if raw.Op == 0 && raw.T == "MESSAGE_CREATE" {
			var msg struct {
				Content   string `json:"content"`
				ChannelID string `json:"channel_id"`
				GuildID   string `json:"guild_id"`
			}
			if err := json.Unmarshal(raw.D, &msg); err != nil {
				continue
			}
			if !inboundAllowed(d.cfg.Allowlist, msg.GuildID) {
				if !inboundAllowed(d.cfg.Allowlist, msg.ChannelID) {
					dropInbound("discord", "not_allowlisted")
					continue
				}
			}
			d.handleDiscordContent(msg.ChannelID, msg.Content)
		}
	}
}

// discordCommand parses slash commands. Returns action and body for display.
func discordCommand(content string) (action, body string) {
	text := strings.TrimSpace(content)
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "/display"):
		rest := ""
		if len(text) >= len("/display") {
			rest = strings.TrimSpace(text[len("/display"):])
		}
		// strip @bot mention prefix
		if strings.HasPrefix(rest, "@") {
			if idx := strings.Index(rest, " "); idx != -1 {
				rest = strings.TrimSpace(rest[idx+1:])
			} else {
				rest = ""
			}
		}
		if rest == "" && strings.HasPrefix(lower, "/display@") {
			if idx := strings.Index(text, " "); idx != -1 {
				rest = strings.TrimSpace(text[idx+1:])
			} else {
				rest = ""
			}
		}
		return "display", rest
	case lower == "/next" || strings.HasPrefix(lower, "/next ") || strings.HasPrefix(lower, "/next@"):
		return "next", ""
	case lower == "/pause" || strings.HasPrefix(lower, "/pause ") || strings.HasPrefix(lower, "/pause@"):
		return "pause", ""
	case lower == "/resume" || strings.HasPrefix(lower, "/resume ") || strings.HasPrefix(lower, "/resume@"):
		return "resume", ""
	case lower == "/status" || strings.HasPrefix(lower, "/status ") || strings.HasPrefix(lower, "/status@"):
		return "status", ""
	default:
		return "", ""
	}
}

func discordStatusBody() string {
	st := GlobalFeed.Status()
	paused, _ := st["paused"].(bool)
	current, _ := st["current"].(string)
	next, _ := st["next"].(string)
	var sb strings.Builder
	sb.WriteString("paused: ")
	if paused {
		sb.WriteString("true")
	} else {
		sb.WriteString("false")
	}
	if current != "" {
		sb.WriteString("\ncurrent: ")
		sb.WriteString(current)
	}
	if next != "" {
		sb.WriteString("\nnext: ")
		sb.WriteString(next)
	}
	return sb.String()
}

func (d *DiscordInbound) handleDiscordContent(channelID, content string) {
	action, body := discordCommand(content)
	switch action {
	case "display":
		if body == "" {
			return
		}
		msg := InboundMessage{Source: "discord", SourceID: channelID, SourceName: "Discord", Title: "Discord", Body: body, Priority: inboundPriorityNormal}
		if ok, _ := admitInbound(d.cfg, msg); !ok {
			dropInbound("discord", "not_admitted")
			return
		}
		d.s.DeliverInbound(msg)
	case "next":
		GlobalFeed.Next()
	case "pause":
		GlobalFeed.Pause()
	case "resume":
		GlobalFeed.Resume()
	case "status":
		msg := InboundMessage{Source: "discord", SourceID: channelID, SourceName: "Discord", Title: "Discord", Body: discordStatusBody(), Priority: inboundPriorityNormal}
		if ok, _ := admitInbound(d.cfg, msg); !ok {
			dropInbound("discord", "not_admitted")
			return
		}
		d.s.DeliverInbound(msg)
	default:
		// unknown: ignore
	}
}

// InboundDiscord handles POST /api/inbound/discord interactions.
func (s *Server) InboundDiscord(c *gin.Context) {
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	row, err := s.DB.InboundAdapter.Query().Where(inboundadapter.KindEQ("discord")).Only(ctx)
	if err != nil || row == nil || !row.Enabled || strings.TrimSpace(row.Secret) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	cfg := inboundConfigFromRow(row)

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}
	if len(raw) > 64*1024 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "payload too large"})
		return
	}

	sigHex := c.GetHeader("X-Signature-Ed25519")
	ts := c.GetHeader("X-Signature-Timestamp")
	if sigHex == "" || ts == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	pubHex := cfg.Config["public_key"]
	if pubHex == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	pubBytes, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	sigBytes, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	msg := append([]byte(ts), raw...)
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), msg, sigBytes) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var body struct {
		Type int `json:"type"`
		Data *struct {
			Name    string `json:"name"`
			Options []struct {
				Name  string `json:"name"`
				Value any    `json:"value"`
			} `json:"options"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	switch body.Type {
	case 1:
		c.JSON(http.StatusOK, gin.H{"type": 1})
		return
	case 2:
		if body.Data == nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}
		name := strings.ToLower(strings.TrimSpace(body.Data.Name))
		var reply string
		switch name {
		case "display":
			var text string
			for _, o := range body.Data.Options {
				if strings.ToLower(o.Name) == "text" || strings.ToLower(o.Name) == "message" || strings.ToLower(o.Name) == "content" {
					if s, ok := o.Value.(string); ok {
						text = s
					} else {
						text = strings.TrimSpace(strings.Trim(stringMust(o.Value), `"`))
					}
					break
				}
			}
			if len(body.Data.Options) > 0 && text == "" {
				// fallback: first option value as string
				if s, ok := body.Data.Options[0].Value.(string); ok {
					text = s
				}
			}
			if strings.TrimSpace(text) == "" {
				reply = "Usage: /display <text>"
			} else {
				// For HTTP we don't have channelID; use first allowlist entry or "discord"
				sourceID := ""
				if len(cfg.Allowlist) > 0 && cfg.Allowlist[0] != "*" {
					sourceID = cfg.Allowlist[0]
				} else {
					sourceID = "discord"
				}
				im := InboundMessage{Source: "discord", SourceID: sourceID, SourceName: "Discord", Title: "Discord", Body: strings.TrimSpace(text), Priority: inboundPriorityNormal}
				if ok, retry := admitInbound(cfg, im); !ok {
					if retry > 0 {
						c.Header("Retry-After", strconv.Itoa(retry))
						c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate_limit_exceeded"})
						return
					}
					dropInbound("discord", "not_admitted")
					reply = "Not allowed"
				} else {
					s.DeliverInbound(im)
					reply = "Displayed"
				}
			}
		case "next":
			GlobalFeed.Next()
			reply = "Skipped to next"
		case "pause":
			GlobalFeed.Pause()
			reply = "Feed paused"
		case "resume":
			GlobalFeed.Resume()
			reply = "Feed resumed"
		case "status":
			reply = discordStatusBody()
		default:
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "unknown command"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"type": 4, "data": gin.H{"content": reply}})
		return
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "unknown type"})
		return
	}
}

func stringMust(v any) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
