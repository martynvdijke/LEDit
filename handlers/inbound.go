package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/inboundadapter"
)

// InboundMessage is the normalized envelope every inbound adapter produces. One
// shape means one delivery path (DeliverInbound) and one set of rate-limit and
// allowlist rules.
type InboundMessage struct {
	Source     string // adapter kind: discord, slack, matrix, ntfy, gotify, pushover
	SourceID   string // channel/room/sender identifier, used for allowlisting
	SourceName string // human label for logs and fallback titles
	Title      string
	Body       string
	Priority   int    // 0-3, see the inboundPriority* constants
	MediaURL   string // remote image URL (optional)
	MediaPath  string // local file path (optional)
	TTL        time.Duration
}

// InboundAdapter is the lifecycle contract for long-poll adapters. HTTP-only
// receivers (push bridges, Discord interactions, Slack events) register too and
// make Start/Stop no-ops so the registry treats every kind uniformly.
type InboundAdapter interface {
	Kind() string
	Start() error
	Stop()
}

const (
	inboundPriorityLow    = 0
	inboundPriorityNormal = 1
	inboundPriorityHigh   = 2
	inboundPriorityUrgent = 3
)

// Per-minute admission limits. Kind-level guards a single adapter; source-level
// guards one noisy channel/sender inside it.
const (
	inboundKindRateLimit   = 120
	inboundSourceRateLimit = 20
)

// InboundAdapterConfig is the decoded per-kind settings row.
type InboundAdapterConfig struct {
	Kind      string
	Secret    string
	Allowlist []string
	Config    map[string]string
}

type inboundFactory func(s *Server, cfg InboundAdapterConfig) InboundAdapter

var (
	inboundRegMu     sync.Mutex
	inboundFactories = map[string]inboundFactory{}
	inboundAdapters  = map[string]InboundAdapter{}
)

// registerInboundFactory wires a kind's constructor. Called from each adapter
// file's init().
func registerInboundFactory(kind string, f inboundFactory) {
	inboundRegMu.Lock()
	defer inboundRegMu.Unlock()
	inboundFactories[kind] = f
}

func inboundConfigFromRow(row *ent.InboundAdapter) InboundAdapterConfig {
	var allow []string
	_ = json.Unmarshal([]byte(row.Allowlist), &allow)
	var cfg map[string]string
	_ = json.Unmarshal([]byte(row.Config), &cfg)
	if cfg == nil {
		cfg = map[string]string{}
	}
	return InboundAdapterConfig{Kind: row.Kind, Secret: row.Secret, Allowlist: allow, Config: cfg}
}

// StartInboundAdapters starts every enabled, configured adapter. Disabled or
// unconfigured kinds are inert. Called once at startup beside StartMQTT.
func StartInboundAdapters(s *Server) {
	rows, err := s.DB.InboundAdapter.Query().All(s.Ctx)
	if err != nil {
		slog.Warn("inbound adapters load failed", "error", err)
		return
	}
	for _, row := range rows {
		if !row.Enabled || row.Secret == "" {
			continue
		}
		startInboundAdapter(s, row)
	}
}

func startInboundAdapter(s *Server, row *ent.InboundAdapter) {
	inboundRegMu.Lock()
	f, ok := inboundFactories[row.Kind]
	inboundRegMu.Unlock()
	if !ok {
		return
	}
	a := f(s, inboundConfigFromRow(row))
	if a == nil {
		return
	}
	inboundRegMu.Lock()
	inboundAdapters[row.Kind] = a
	inboundRegMu.Unlock()
	if err := a.Start(); err != nil {
		slog.Warn("inbound adapter start failed", "kind", row.Kind, "error", err)
	}
}

// RestartInboundAdapter stops any running adapter of kind and starts it again
// from the current settings row. Called after an admin saves settings so a
// long-poll loop picks up new credentials without a process restart.
func RestartInboundAdapter(s *Server, kind string) {
	inboundRegMu.Lock()
	if old := inboundAdapters[kind]; old != nil {
		delete(inboundAdapters, kind)
		inboundRegMu.Unlock()
		old.Stop()
	} else {
		inboundRegMu.Unlock()
	}
	row, err := s.DB.InboundAdapter.Query().Where(inboundadapter.KindEQ(kind)).Only(s.Ctx)
	if err != nil || row == nil || !row.Enabled || row.Secret == "" {
		return
	}
	startInboundAdapter(s, row)
}

// DeliverInbound puts an admitted message onto the existing notification path.
// TTL is clamped to the webhook default range so no adapter can pin a message
// forever.
func (s *Server) DeliverInbound(msg InboundMessage) {
	secs := int(msg.TTL.Seconds())
	if msg.TTL <= 0 {
		secs = s.webhookDefaultTTL()
	}
	if secs < 1 {
		secs = 1
	}
	if secs > 3600 {
		secs = 3600
	}
	opts := []NotifOption{
		WithTTL(time.Duration(secs) * time.Second),
		WithPriority(msg.Priority),
	}
	if media := inboundMedia(msg); media != nil {
		opts = append(opts, WithMedia(media))
	}
	title := msg.Title
	if title == "" {
		title = msg.SourceName
	}
	if title == "" {
		title = strings.ToUpper(msg.Source)
	}
	s.AddNotification(title, msg.Body, opts...)

	// Delivery log is best-effort and off the caller's goroutine: a failing log
	// must never block or fail delivery.
	target := msg.SourceID
	kind := msg.Source
	go recordDelivery(DeliveryLogEntry{
		MessageID:   "inbound:" + kind,
		Kind:        kind,
		Surface:     "inbound",
		Target:      target,
		Status:      "delivered",
		AttemptedAt: time.Now(),
	})
}

// admitInbound is the single chokepoint for allowlist + rate limiting. New
// adapters deny by default: an empty allowlist admits nobody.
func admitInbound(cfg InboundAdapterConfig, msg InboundMessage) (bool, int) {
	if !inboundAllowed(cfg.Allowlist, msg.SourceID) {
		return false, 0
	}
	if ok, retry := checkWindowRateLimit("inbound:"+cfg.Kind, inboundKindRateLimit); !ok {
		return false, retry
	}
	if ok, retry := checkWindowRateLimit("inbound:"+cfg.Kind+":"+msg.SourceID, inboundSourceRateLimit); !ok {
		return false, retry
	}
	return true, 0
}

// inboundAllowed reports whether sourceID is on the allowlist. An empty
// allowlist admits nothing; "*" admits any non-empty source.
func inboundAllowed(allowlist []string, sourceID string) bool {
	if sourceID == "" {
		return false
	}
	for _, a := range allowlist {
		if a == "*" || a == sourceID {
			return true
		}
	}
	return false
}

func inboundMedia(msg InboundMessage) *MessageMedia {
	if msg.MediaURL == "" && msg.MediaPath == "" {
		return nil
	}
	return &MessageMedia{URL: msg.MediaURL, Path: msg.MediaPath}
}

// inboundSecretOK compares a presented secret against the configured one in
// constant time. An unset expected secret never authenticates.
func inboundSecretOK(want, got string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// abortInboundRateLimited rejects a throttled inbound HTTP push.
func abortInboundRateLimited(c *gin.Context, retryAfter int) {
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate_limit_exceeded", "code": "rate_limit_exceeded"})
}

// dropInbound logs a message rejected by admission. Long-poll adapters have no
// response to send, so they drop and continue rather than 429.
func dropInbound(kind, reason string) {
	slog.Debug("inbound message dropped", "kind", kind, "reason", reason)
}

// ---------------------------------------------------------------------------
// Priority mapping onto the internal 0-3 scale
// ---------------------------------------------------------------------------

// inboundPriorityFromNtfy maps ntfy's 1-5 scale (1 min, 3 default, 5 urgent).
func inboundPriorityFromNtfy(p int) int {
	switch {
	case p <= 1:
		return inboundPriorityLow
	case p >= 5:
		return inboundPriorityUrgent
	case p == 4:
		return inboundPriorityHigh
	default:
		return inboundPriorityNormal
	}
}

// inboundPriorityFromGotify maps Gotify's 1-10 scale (>=7 high, >=9 urgent).
func inboundPriorityFromGotify(p int) int {
	switch {
	case p <= 2:
		return inboundPriorityLow
	case p >= 9:
		return inboundPriorityUrgent
	case p >= 7:
		return inboundPriorityHigh
	default:
		return inboundPriorityNormal
	}
}

// inboundPriorityFromPushover maps Pushover's -2..2 scale.
func inboundPriorityFromPushover(p int) int {
	switch {
	case p <= -2:
		return inboundPriorityLow
	case p == -1:
		return inboundPriorityNormal
	case p == 0:
		return inboundPriorityNormal
	case p == 1:
		return inboundPriorityHigh
	default:
		return inboundPriorityUrgent
	}
}
