package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/guestphoto"
	"ledit/ent/inboundadapter"
)

var knownInboundKinds = []string{"discord", "slack", "matrix", "ntfy", "gotify", "pushover"}

type inboundAdapterView struct {
	Kind           string
	Title          string
	Enabled        bool
	Configured     bool
	AllowlistCount int
	Allowlist      string // raw JSON array
	AllowlistText  string // newline-joined for textarea
	Config         string
	LastError      string
	LastMessageAt  string
}

func titleKind(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func allowlistJSON(list []string) string {
	if list == nil {
		list = []string{}
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func parseAllowlist(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "[]"
	}
	// split on comma, newline, space, tab
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ';'
	})
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// AdminInbound GET /admin/inbound
func (s *Server) AdminInbound(c *gin.Context) {
	rows, err := s.DB.InboundAdapter.Query().All(s.Ctx)
	if err != nil {
		rows = nil
	}
	byKind := map[string]*ent.InboundAdapter{}
	for _, r := range rows {
		byKind[r.Kind] = r
	}
	views := make([]inboundAdapterView, 0, len(knownInboundKinds))
	for _, kind := range knownInboundKinds {
		var v inboundAdapterView
		v.Kind = kind
		v.Title = titleKind(kind)
		if row, ok := byKind[kind]; ok {
			v.Enabled = row.Enabled
			v.Configured = row.Secret != ""
			var list []string
			_ = json.Unmarshal([]byte(row.Allowlist), &list)
			v.AllowlistCount = len(list)
			v.Allowlist = row.Allowlist
			if len(list) > 0 {
				v.AllowlistText = strings.Join(list, "\n")
			}
			v.Config = row.Config
			if v.Config == "" {
				v.Config = "{}"
			}
			var cfg map[string]string
			_ = json.Unmarshal([]byte(row.Config), &cfg)
			if cfg != nil {
				v.LastError = cfg["last_error"]
				v.LastMessageAt = cfg["last_message_at"]
			}
		} else {
			v.Config = "{}"
			v.Allowlist = "[]"
			v.AllowlistCount = 0
		}
		views = append(views, v)
	}
	s.renderPage(c, http.StatusOK, "inbound_adapters.html", gin.H{
		"adapters": views,
		"active":   "inbound",
	})
}

// AdminInboundSave POST /admin/inbound/:kind
func (s *Server) AdminInboundSave(c *gin.Context) {
	kind := strings.ToLower(strings.TrimSpace(c.Param("kind")))
	enabled := c.PostForm("enabled") == "on"
	secretRaw := c.PostForm("secret")
	allowlistRaw := c.PostForm("allowlist")
	configRaw := strings.TrimSpace(c.PostForm("config"))
	if configRaw == "" {
		configRaw = "{}"
	}
	if !json.Valid([]byte(configRaw)) {
		SetFlash(c, "danger", "config: invalid JSON")
		c.Redirect(http.StatusFound, "/admin/inbound")
		return
	}
	allowlistJSONStr := parseAllowlist(allowlistRaw)

	existing, _ := s.DB.InboundAdapter.Query().Where(inboundadapter.KindEQ(kind)).Only(s.Ctx)

	newSecret := secretRaw
	if existing != nil {
		if secretRaw == "********" || secretRaw == "" {
			newSecret = existing.Secret
		}
	} else {
		if newSecret == "********" {
			newSecret = ""
		}
	}

	if existing != nil {
		upd := s.DB.InboundAdapter.UpdateOne(existing).
			SetEnabled(enabled).
			SetAllowlist(allowlistJSONStr).
			SetConfig(configRaw)
		if secretRaw != "********" && secretRaw != "" {
			upd.SetSecret(newSecret)
		}
		if err := upd.Exec(s.Ctx); err != nil {
			SetFlash(c, "danger", "Failed to save adapter: "+err.Error())
			c.Redirect(http.StatusFound, "/admin/inbound")
			return
		}
	} else {
		b := s.DB.InboundAdapter.Create().
			SetKind(kind).
			SetEnabled(enabled).
			SetSecret(newSecret).
			SetAllowlist(allowlistJSONStr).
			SetConfig(configRaw)
		if err := b.Exec(s.Ctx); err != nil {
			SetFlash(c, "danger", "Failed to save adapter: "+err.Error())
			c.Redirect(http.StatusFound, "/admin/inbound")
			return
		}
	}

	RestartInboundAdapter(s, kind)
	SetFlash(c, "success", "Inbound adapter saved")
	c.Redirect(http.StatusFound, "/admin/inbound")
}

// AdminInboundTest POST /admin/inbound/:kind/test
func (s *Server) AdminInboundTest(c *gin.Context) {
	kind := strings.ToLower(strings.TrimSpace(c.Param("kind")))
	s.DeliverInbound(InboundMessage{
		Source:     kind,
		SourceID:   "test",
		SourceName: titleKind(kind),
		Title:      "Inbound test",
		Body:       "Test message from " + kind,
		Priority:   inboundPriorityNormal,
	})
	SetFlash(c, "success", "Test message delivered")
	c.Redirect(http.StatusFound, "/admin/inbound")
}

// AdminGuestPhotos GET /admin/photo-frame
func (s *Server) AdminGuestPhotos(c *gin.Context) {
	statusFilter := strings.TrimSpace(c.Query("status"))
	var photos []*ent.GuestPhoto
	var err error
	switch statusFilter {
	case "pending", "approved", "rejected":
		photos, err = s.DB.GuestPhoto.Query().
			Where(guestphoto.StatusEQ(guestphoto.Status(statusFilter))).
			Order(ent.Desc(guestphoto.FieldCreatedAt), ent.Desc(guestphoto.FieldID)).
			All(s.Ctx)
	default:
		statusFilter = ""
		photos, err = s.DB.GuestPhoto.Query().
			Order(ent.Desc(guestphoto.FieldCreatedAt), ent.Desc(guestphoto.FieldID)).
			All(s.Ctx)
	}
	if err != nil {
		photos = nil
	}
	// counts
	all, _ := s.DB.GuestPhoto.Query().All(s.Ctx)
	counts := map[string]int{"all": len(all), "pending": 0, "approved": 0, "rejected": 0}
	for _, p := range all {
		counts[string(p.Status)]++
	}
	s.renderPage(c, http.StatusOK, "guest_photos.html", gin.H{
		"photos": photos,
		"counts": counts,
		"status": statusFilter,
		"active": "photo-frame",
	})
}
