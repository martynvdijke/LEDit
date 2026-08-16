package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/ent"
)

// Alert priorities, matching Gotify's scale (1=low .. 10=high).
const (
	AlertPriorityRecover = 2
	AlertPriorityStale   = 5
	AlertPriorityFailing = 5
)

// Alert is a single outbound alert message.
type Alert struct {
	Title    string
	Message  string
	Priority int
}

// AlertSender delivers an alert to an external channel.
type AlertSender interface {
	Name() string
	Enabled() bool
	Send(ctx context.Context, a Alert) error
}

// ---------------------------------------------------------------------------
// Gotify sender
// ---------------------------------------------------------------------------

// GotifySender POSTs alerts to a Gotify server's /message endpoint.
type GotifySender struct {
	ServerURL string
	Token     string
}

func (g *GotifySender) Name() string { return "gotify" }

func (g *GotifySender) Enabled() bool { return g.ServerURL != "" && g.Token != "" }

func (g *GotifySender) Send(ctx context.Context, a Alert) error {
	url := strings.TrimSuffix(g.ServerURL, "/") + "/message"
	body, err := json.Marshal(map[string]any{
		"title":    a.Title,
		"message":  a.Message,
		"priority": a.Priority,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", g.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gotify returned status %d", resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Email sender (stdlib net/smtp)
// ---------------------------------------------------------------------------

// EmailSender sends plain-text alerts via SMTP using the dormant
// EmailSettings configuration.
type EmailSender struct {
	Host        string
	Port        int
	Username    string
	Password    string
	From        string
	Recipient   string
	UseTLS      bool
	InsecureTLS bool
}

func (e *EmailSender) Name() string { return "email" }

func (e *EmailSender) Enabled() bool {
	return e.Host != "" && e.Port > 0 && e.From != "" && e.Recipient != ""
}

func (e *EmailSender) Send(ctx context.Context, a Alert) error {
	return e.SendMessage(e.Recipient, a.Title, a.Message)
}

// SendMessage delivers an arbitrary plain-text email to the given recipient
// using the sender's SMTP configuration. Shared by the alert engine and the
// password-reset flow.
func (e *EmailSender) SendMessage(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", e.Host, e.Port)
	msg := buildEmailMessage(e.From, to, subject, body)

	if e.UseTLS {
		// Implicit TLS (e.g. port 465): wrap the connection before SMTP.
		tlsCfg := &tls.Config{
			ServerName:         e.Host,
			InsecureSkipVerify: e.InsecureTLS,
		}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("tls dial %s: %w", addr, err)
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, e.Host)
		if err != nil {
			return err
		}
		defer c.Close()
		return smtpSendClient(c, e.Username, e.Password, e.Host, e.From, to, msg)
	}

	auth := smtp.PlainAuth("", e.Username, e.Password, e.Host)
	return smtp.SendMail(addr, auth, e.From, []string{to}, msg)
}

func smtpSendClient(c *smtp.Client, username, password, host, from, recipient string, msg []byte) error {
	if ok, _ := c.Extension("AUTH"); ok {
		auth := smtp.PlainAuth("", username, password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := c.Rcpt(recipient); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func buildEmailMessage(from, to, subject, body string) []byte {
	var b bytes.Buffer
	h := textproto.MIMEHeader{}
	h.Set("From", from)
	h.Set("To", to)
	h.Set("Subject", subject)
	h.Set("MIME-Version", "1.0")
	h.Set("Content-Type", "text/plain; charset=utf-8")
	for k, vs := range h {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(&b, "\r\n%s\r\n", body)
	return b.Bytes()
}

// ---------------------------------------------------------------------------
// Alert engine
// ---------------------------------------------------------------------------

// alertKey identifies the state of one source/device alert subject.
type alertKey struct {
	Kind string // "source" | "device"
	ID   string // cacheKey or device id
}

// alertState tracks what alert state a key is currently in.
type alertState struct {
	Failing bool
	Stale   bool
}

// AlertConfig is the engine's view of the AlertSettings row.
type AlertConfig struct {
	FailureThreshold int
	CooldownMinutes  int
	StaleMultiplier  int
	NotifyRecovery   bool
}

// AlertEngine polls the health registry and device fleet, sending alerts on
// state transitions. It never writes health state and never blocks the feed.
type AlertEngine struct {
	senders  func() []AlertSender
	state    map[alertKey]*alertState
	cooldown map[alertKey]time.Time
	now      func() time.Time
}

// NewAlertEngine creates an engine with the given sender factory. The factory
// is invoked per tick so runtime config changes apply immediately.
func NewAlertEngine(senders func() []AlertSender) *AlertEngine {
	return &AlertEngine{
		senders:  senders,
		state:    map[alertKey]*alertState{},
		cooldown: map[alertKey]time.Time{},
		now:      time.Now,
	}
}

// Reset clears the transition state machine (used by tests).
func (e *AlertEngine) Reset() {
	e.state = map[alertKey]*alertState{}
	e.cooldown = map[alertKey]time.Time{}
}

// Tick runs one poll of the health registry and device fleet.
func (e *AlertEngine) Tick(ctx context.Context, health *HealthRegistry, devices []*ent.DeviceSettings, cfg AlertConfig) {
	if health == nil {
		return
	}
	senders := []AlertSender{}
	for _, s := range e.senders() {
		if s.Enabled() {
			senders = append(senders, s)
		}
	}
	if len(senders) == 0 {
		return // short-circuit: no channels enabled
	}

	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}
	staleMult := cfg.StaleMultiplier
	if staleMult <= 0 {
		staleMult = 3
	}

	snap := health.Snapshot()
	now := e.now()

	// Sources: failing / recovered transitions.
	for key, sh := range snap {
		ak := alertKey{Kind: "source", ID: key}
		st := e.stateOf(ak)
		if sh.ConsecutiveFails >= threshold {
			if !st.Failing && !e.onCooldown(ak, now, cooldown) {
				e.sendAll(ctx, senders, Alert{
					Title:    "LEDit: source failing",
					Message:  fmt.Sprintf("%s has failed %d times consecutively", key, sh.ConsecutiveFails),
					Priority: AlertPriorityFailing,
				})
				e.record(ak, now)
				st.Failing = true
			}
		} else if st.Failing {
			st.Failing = false
			if cfg.NotifyRecovery && !e.onCooldown(ak, now, cooldown) {
				e.sendAll(ctx, senders, Alert{
					Title:    "LEDit: source recovered",
					Message:  fmt.Sprintf("%s is rendering again", key),
					Priority: AlertPriorityRecover,
				})
				e.record(ak, now)
			}
		}
		e.set(ak, st)
	}

	// Devices: stale / back-online transitions.
	for _, d := range devices {
		if !d.Enabled {
			continue
		}
		ak := alertKey{Kind: "device", ID: fmt.Sprintf("%d", d.ID)}
		st := e.stateOf(ak)
		stale := deviceLiveness(d.LastSeenAt, d.RefreshInterval*staleMult) == "stale"
		if stale {
			if !st.Stale && !e.onCooldown(ak, now, cooldown) {
				e.sendAll(ctx, senders, Alert{
					Title:    "LEDit: device offline",
					Message:  fmt.Sprintf("%s (IP %s) has not reported in over %dx its refresh interval", d.Name, d.IP, staleMult),
					Priority: AlertPriorityStale,
				})
				e.record(ak, now)
				st.Stale = true
			}
		} else if st.Stale {
			st.Stale = false
			if cfg.NotifyRecovery && !e.onCooldown(ak, now, cooldown) {
				e.sendAll(ctx, senders, Alert{
					Title:    "LEDit: device back online",
					Message:  fmt.Sprintf("%s (IP %s) is reporting again", d.Name, d.IP),
					Priority: AlertPriorityRecover,
				})
				e.record(ak, now)
			}
		}
		e.set(ak, st)
	}
}

func (e *AlertEngine) stateOf(ak alertKey) *alertState {
	if s, ok := e.state[ak]; ok {
		return s
	}
	s := &alertState{}
	e.state[ak] = s
	return s
}

func (e *AlertEngine) set(ak alertKey, st *alertState) {
	e.state[ak] = st
}

func (e *AlertEngine) onCooldown(ak alertKey, now time.Time, cooldown time.Duration) bool {
	last, ok := e.cooldown[ak]
	if !ok {
		return false
	}
	return now.Sub(last) < cooldown
}

func (e *AlertEngine) record(ak alertKey, now time.Time) {
	e.cooldown[ak] = now
}

// sendAll delivers an alert through every enabled sender. Delivery failures
// are logged and never fatal.
func (e *AlertEngine) sendAll(ctx context.Context, senders []AlertSender, a Alert) {
	for _, s := range senders {
		if err := s.Send(ctx, a); err != nil {
			slog.Error("alert delivery failed", "source", "alerting", "channel", s.Name(), "error", err)
		} else {
			slog.Info("alert delivered", "source", "alerting", "channel", s.Name(), "title", a.Title)
		}
	}
}

// StartAlertEngine runs the alert engine in the background on a 30s ticker
// until ctx is cancelled. Send a nil engine (e.g. no senders configured) to
// disable it entirely.
func (s *Server) StartAlertEngine(ctx context.Context) {
	engine := NewAlertEngine(func() []AlertSender {
		return s.alertSenders()
	})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		// Initial poll shortly after startup.
		engine.Tick(ctx, Health, s.loadDevices(ctx), s.loadAlertConfig(ctx))
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				engine.Tick(ctx, Health, s.loadDevices(ctx), s.loadAlertConfig(ctx))
			}
		}
	}()
	slog.Info("alert engine started", "source", "alerting")
}

// loadAlertConfig reads the current AlertSettings row (or defaults).
func (s *Server) loadAlertConfig(ctx context.Context) AlertConfig {
	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	as, err := s.DB.AlertSettings.Query().Only(ctx)
	if err != nil {
		return cfg
	}
	cfg.FailureThreshold = as.FailureThreshold
	cfg.CooldownMinutes = as.CooldownMinutes
	cfg.StaleMultiplier = as.StaleMultiplier
	cfg.NotifyRecovery = as.NotifyRecovery
	return cfg
}

// loadDevices reads all device rows for the engine.
func (s *Server) loadDevices(ctx context.Context) []*ent.DeviceSettings {
	devs, err := s.DB.DeviceSettings.Query().All(ctx)
	if err != nil {
		slog.Warn("alert engine: failed to load devices", "source", "alerting", "error", err)
		return nil
	}
	return devs
}

// alertSenders builds the active senders from the current settings rows.
func (s *Server) alertSenders() []AlertSender {
	ctx := context.Background()
	var senders []AlertSender

	as, err := s.DB.AlertSettings.Query().Only(ctx)
	if err == nil {
		if as.GotifyEnabled {
			senders = append(senders, &GotifySender{ServerURL: as.GotifyURL, Token: as.GotifyToken})
		}
		if as.EmailEnabled {
			email, eerr := s.DB.EmailSettings.Query().Only(ctx)
			if eerr == nil {
				senders = append(senders, &EmailSender{
					Host:        email.Host,
					Port:        email.Port,
					Username:    email.Username,
					Password:    email.Password,
					From:        email.FromAddress,
					Recipient:   as.RecipientEmail,
					UseTLS:      email.UseTLS,
					InsecureTLS: true,
				})
			} else {
				slog.Warn("alert engine: email enabled but no SMTP settings", "source", "alerting", "error", eerr)
			}
		}
	}
	return senders
}

// SendTestAlert fires a test alert through every enabled channel, bypassing
// the cooldown, and reports per-channel results.
func (s *Server) SendTestAlert(ctx context.Context) map[string]string {
	results := map[string]string{}
	for _, sender := range s.alertSenders() {
		if !sender.Enabled() {
			continue
		}
		alert := Alert{
			Title:    "LEDit test alert",
			Message:  "This is a test alert from your LEDit server.",
			Priority: AlertPriorityRecover,
		}
		if err := sender.Send(ctx, alert); err != nil {
			results[sender.Name()] = "failed: " + err.Error()
			slog.Error("test alert failed", "source", "alerting", "channel", sender.Name(), "error", err)
		} else {
			results[sender.Name()] = "ok"
		}
	}
	if len(results) == 0 {
		results["none"] = "no channels enabled"
	}
	return results
}

// ---------------------------------------------------------------------------
// Admin handlers
// ---------------------------------------------------------------------------

// AdminAlertSettings renders the alert settings form.
func (s *Server) AdminAlertSettings(c *gin.Context) {
	settings, err := s.DB.AlertSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	s.renderPage(c, 200, "alert_settings.html", gin.H{
		"settings":    settings,
		"hasSettings": settings != nil,
	})
}

// AdminAlertSettingsSave persists the alert settings row.
func (s *Server) AdminAlertSettingsSave(c *gin.Context) {
	gotifyEnabled := c.PostForm("gotify_enabled") == "on"
	gotifyURL := c.PostForm("gotify_url")
	gotifyToken := c.PostForm("gotify_token")
	emailEnabled := c.PostForm("email_enabled") == "on"
	recipient := c.PostForm("recipient_email")
	failureThreshold := atoiDefault(c.PostForm("failure_threshold"), 3)
	cooldown := atoiDefault(c.PostForm("cooldown_minutes"), 15)
	staleMult := atoiDefault(c.PostForm("stale_multiplier"), 3)
	notifyRecovery := c.PostForm("notify_recovery") == "on"

	v := NewValidator().
		RangeInt("Failure threshold", failureThreshold, 1, 1000).
		RangeInt("Cooldown minutes", cooldown, 1, 10080).
		RangeInt("Stale multiplier", staleMult, 1, 100)
	if gotifyEnabled {
		v.Required("Gotify URL", gotifyURL).Required("Gotify token", gotifyToken)
	}
	if emailEnabled {
		v.Required("Recipient email", recipient)
	}
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(302, "/admin/settings/alerts")
		return
	}

	exists, _ := s.DB.AlertSettings.Query().Exist(s.Ctx)
	if !exists {
		_, err := s.DB.AlertSettings.Create().
			SetGotifyEnabled(gotifyEnabled).
			SetGotifyURL(gotifyURL).
			SetGotifyToken(gotifyToken).
			SetEmailEnabled(emailEnabled).
			SetRecipientEmail(recipient).
			SetFailureThreshold(failureThreshold).
			SetCooldownMinutes(cooldown).
			SetStaleMultiplier(staleMult).
			SetNotifyRecovery(notifyRecovery).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to create alert settings", "source", "alerting", "error", err)
			SetFlash(c, "danger", "Failed to save alert settings")
			c.Redirect(302, "/admin/settings/alerts")
			return
		}
	} else {
		_, err := s.DB.AlertSettings.Update().
			SetGotifyEnabled(gotifyEnabled).
			SetGotifyURL(gotifyURL).
			SetGotifyToken(gotifyToken).
			SetEmailEnabled(emailEnabled).
			SetRecipientEmail(recipient).
			SetFailureThreshold(failureThreshold).
			SetCooldownMinutes(cooldown).
			SetStaleMultiplier(staleMult).
			SetNotifyRecovery(notifyRecovery).
			Save(s.Ctx)
		if err != nil {
			slog.Error("failed to update alert settings", "source", "alerting", "error", err)
			SetFlash(c, "danger", "Failed to save alert settings")
			c.Redirect(302, "/admin/settings/alerts")
			return
		}
	}

	SetFlash(c, "success", "Alert settings saved")
	c.Redirect(302, "/admin/settings/alerts")
}

// AdminAlertSettingsTest fires a test alert through each enabled channel.
func (s *Server) AdminAlertSettingsTest(c *gin.Context) {
	results := s.SendTestAlert(c.Request.Context())
	c.JSON(200, gin.H{"results": results})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// atoiDefault parses an int with a fallback.
func atoiDefault(v string, def int) int {
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}
