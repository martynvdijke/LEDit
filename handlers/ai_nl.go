package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"sync"
	"time"

	"ledit/datasource"
	"ledit/ent"
)

// Intent actions.
const (
	ActionNext             = "next"
	ActionPause            = "pause"
	ActionResume           = "resume"
	ActionPriorityDisplay  = "priority_display"
	ActionSourcePinWithTTL = "source_pin_with_ttl"
	ActionStatusQuery      = "status_query"
)

var validActions = map[string]bool{
	ActionNext:             true,
	ActionPause:            true,
	ActionResume:           true,
	ActionPriorityDisplay:  true,
	ActionSourcePinWithTTL: true,
	ActionStatusQuery:      true,
}

// Known source types for validation (prefixes used in cacheKey).
var knownSourceTypes = map[string]bool{
	"clock": true, "sonarr": true, "radarr": true, "f1": true, "weather": true,
	"homeassistant": true, "untappd": true, "images": true, "videos": true,
	"crypto": true, "stock": true, "systemstats": true, "screensaver": true,
	"rssfeed": true, "calendar": true, "textslides": true, "googlecalendar": true,
	"newsfeed": true, "genericapi": true, "transit": true, "uptime": true,
	"pihole": true, "github": true, "sports": true, "sunmoon": true, "jellyfin": true,
	"analog-clock": true, "matrix-rain": true, "audio": true, "countdown": true,
	"aidigest": true, "matrix": true, "qrcode": true, "pixelart": true,
}

// Intent is the validated NL intent.
type Intent struct {
	Action     string  `json:"action"`
	Text       *string `json:"text,omitempty"`
	TTLSeconds *int    `json:"ttl_seconds,omitempty"`
	SourceType *string `json:"source_type,omitempty"`
	SourceID   *int    `json:"source_id,omitempty"`
}

var (
	ErrInvalidIntent   = errors.New("invalid intent")
	ErrAINotConfigured = errors.New("AI not configured")
	ErrRateLimited     = errors.New("rate limited")
)

// TruncateUserText truncates to 500 chars (runes).
func TruncateUserText(s string) string {
	runes := []rune(s)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return s
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func sanitizeText(s string) string {
	// strip HTML tags, unescape entities, trim
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	// truncate to 500
	s = TruncateUserText(s)
	return s
}

// ValidateIntent validates raw JSON string against strict schema.
func ValidateIntent(rawJSON string) (*Intent, error) {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidIntent)
	}
	// Decode into map to check additionalProperties
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return nil, fmt.Errorf("%w: malformed json: %v", ErrInvalidIntent, err)
	}
	if _, ok := m["action"]; !ok {
		return nil, fmt.Errorf("%w: missing action", ErrInvalidIntent)
	}
	// Determine action first
	var actionHolder struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &actionHolder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
	}
	if !validActions[actionHolder.Action] {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidIntent, actionHolder.Action)
	}
	// Allowed keys per action
	allowedByAction := map[string]map[string]bool{
		ActionNext:             {"action": true},
		ActionPause:            {"action": true},
		ActionResume:           {"action": true},
		ActionStatusQuery:      {"action": true},
		ActionPriorityDisplay:  {"action": true, "text": true, "ttl_seconds": true},
		ActionSourcePinWithTTL: {"action": true, "source_type": true, "source_id": true, "ttl_seconds": true},
	}
	allowed := allowedByAction[actionHolder.Action]
	for k := range m {
		if !allowed[k] {
			return nil, fmt.Errorf("%w: extra field %q for action %q", ErrInvalidIntent, k, actionHolder.Action)
		}
	}
	// Unmarshal into Intent
	var intent Intent
	if err := json.Unmarshal([]byte(rawJSON), &intent); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
	}
	// Per-action validation
	switch intent.Action {
	case ActionPriorityDisplay:
		if intent.Text == nil || strings.TrimSpace(*intent.Text) == "" {
			return nil, fmt.Errorf("%w: priority_display requires text", ErrInvalidIntent)
		}
		sanitized := sanitizeText(*intent.Text)
		if len(sanitized) == 0 || len(sanitized) > 500 {
			return nil, fmt.Errorf("%w: text length invalid", ErrInvalidIntent)
		}
		intent.Text = &sanitized
		if intent.TTLSeconds == nil {
			return nil, fmt.Errorf("%w: priority_display requires ttl_seconds", ErrInvalidIntent)
		}
		if *intent.TTLSeconds < 5 || *intent.TTLSeconds > 300 {
			return nil, fmt.Errorf("%w: ttl_seconds out of range", ErrInvalidIntent)
		}
		if intent.SourceType != nil || intent.SourceID != nil {
			return nil, fmt.Errorf("%w: unexpected source fields", ErrInvalidIntent)
		}
	case ActionSourcePinWithTTL:
		if intent.SourceType == nil || strings.TrimSpace(*intent.SourceType) == "" {
			return nil, fmt.Errorf("%w: source_type required", ErrInvalidIntent)
		}
		st := strings.TrimSpace(*intent.SourceType)
		if !knownSourceTypes[st] {
			return nil, fmt.Errorf("%w: unknown source_type %q", ErrInvalidIntent, st)
		}
		intent.SourceType = &st
		if intent.SourceID == nil {
			return nil, fmt.Errorf("%w: source_id required", ErrInvalidIntent)
		}
		if *intent.SourceID <= 0 {
			return nil, fmt.Errorf("%w: source_id must be >0", ErrInvalidIntent)
		}
		if intent.TTLSeconds == nil {
			return nil, fmt.Errorf("%w: ttl_seconds required", ErrInvalidIntent)
		}
		if *intent.TTLSeconds < 5 || *intent.TTLSeconds > 300 {
			return nil, fmt.Errorf("%w: ttl_seconds out of range", ErrInvalidIntent)
		}
		if intent.Text != nil {
			return nil, fmt.Errorf("%w: unexpected text field", ErrInvalidIntent)
		}
	case ActionNext, ActionPause, ActionResume, ActionStatusQuery:
		if intent.Text != nil || intent.TTLSeconds != nil || intent.SourceType != nil || intent.SourceID != nil {
			return nil, fmt.Errorf("%w: action %q takes no params", ErrInvalidIntent, intent.Action)
		}
	}
	return &intent, nil
}

// SourceInfo is used to build prompt source list.
type SourceInfo struct {
	ID   int
	Type string
	Name string
}

// BuildNLPrompt builds the LLM prompt for intent parsing.
func BuildNLPrompt(userText string, availableSources []SourceInfo) string {
	userText = TruncateUserText(strings.TrimSpace(userText))
	var sb strings.Builder
	sb.WriteString(`You are LEDit intent parser. Return ONLY JSON matching schema:
{"action": enum, "text?": string, "ttl_seconds?": int, "source_type?": string, "source_id?": int}
Actions:
- next: advance to next source
- pause/resume: pause/resume feed
- priority_display: {text, ttl_seconds(5-300)} — show text on wall
- source_pin_with_ttl: {source_type, source_id, ttl_seconds(5-300)} — pin a source
- status_query: return current feed status
Rules:
- If user intent doesn't match any action, return {"action":"status_query"}.
- Never return any other action or field.
Examples:
User: "pause" -> {"action":"pause"}
User: "show weather for a minute then resume" -> {"action":"source_pin_with_ttl","source_type":"weather","source_id":1,"ttl_seconds":60}
User: "hello wall for 30 seconds" -> {"action":"priority_display","text":"hello wall","ttl_seconds":30}
User: "what's playing?" -> {"action":"status_query"}
`)
	if len(availableSources) > 0 {
		sb.WriteString("Available sources:\n")
		for _, s := range availableSources {
			// ID+name only, truncate
			name := s.Name
			if len(name) > 50 {
				name = name[:50]
			}
			fmt.Fprintf(&sb, "- %s:%d %s\n", s.Type, s.ID, name)
		}
	}
	sb.WriteString(fmt.Sprintf("User: %q\nReturn JSON:", userText))
	return sb.String()
}

// callLLMFunc seam for tests.
var callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, messages []datasource.ChatMessage, maxTokens int) (string, error) {
	return datasource.ChatCompletions(ctx, cfg, messages, maxTokens)
}

// ParseIntent calls LLM and validates result.
func ParseIntent(ctx context.Context, userText string, cfg datasource.AIConfig) (*Intent, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, ErrAINotConfigured
	}
	userText = TruncateUserText(userText)
	// Build prompt without source list (caller may provide sources via global helper)
	prompt := BuildNLPrompt(userText, nil)
	// 5s timeout
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	content, err := callLLMFunc(cctx, cfg, []datasource.ChatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: userText},
	}, 200)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline") {
			return nil, fmt.Errorf("%w: timeout", ErrInvalidIntent)
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
	}
	content = strings.TrimSpace(content)
	// Extract JSON if wrapped in code fence
	if strings.HasPrefix(content, "```") {
		// strip fences
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(strings.TrimSpace(content), "```")
		content = strings.TrimSpace(content)
	}
	intent, err := ValidateIntent(content)
	if err != nil {
		return nil, err
	}
	return intent, nil
}

// LoadAIConfig loads AI config from DB.
func LoadAIConfig(s *Server) (datasource.AIConfig, bool) {
	if s == nil || s.DB == nil {
		return datasource.AIConfig{}, false
	}
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ai, err := s.DB.AISettings.Query().Only(ctx)
	if err != nil {
		return datasource.AIConfig{}, false
	}
	cfg := datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Model) == "" {
		return cfg, false
	}
	return cfg, true
}

// AvailableSourcesForPrompt returns source list for prompt injection.
func AvailableSourcesForPrompt(s *Server) []SourceInfo {
	if s == nil || s.DB == nil || s.WSHub == nil {
		return nil
	}
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Load general settings with edges would be heavy; use placeholder minimal list
	// Query known datasource tables for IDs+names minimal.
	var out []SourceInfo
	// Use WSHub.loadSources via a minimal GeneralSettings fetch to get current sources
	// Fallback: query each table directly via ent if needed; for prompt we just want IDs.
	// Try to load via GeneralSettings if exists.
	gs, err := s.DB.GeneralSettings.Query().Only(ctx)
	if err == nil && gs != nil {
		// We have settings; use hub's loadSources helper via a full query with edges?
		// Instead do direct queries for a few tables to keep prompt short.
		_ = gs
	}
	// Simple: query a few tables for prompt (best effort, ignore errors)
	if rows, err := s.DB.Weather.Query().All(ctx); err == nil {
		for _, r := range rows {
			out = append(out, SourceInfo{ID: r.ID, Type: "weather", Name: "Weather"})
		}
	}
	if rows, err := s.DB.TextSlide.Query().All(ctx); err == nil {
		for _, r := range rows {
			n := r.Content
			if len(n) > 30 {
				n = n[:30]
			}
			out = append(out, SourceInfo{ID: r.ID, Type: "textslides", Name: "Text: " + n})
		}
	}
	// Cap to 20
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// --- Rate limiter ---

type nlBucket struct {
	times []time.Time
	mu    sync.Mutex
}

var nlRateLimitMap sync.Map // map[int64]*nlBucket

func checkRateLimit(chatID int64) error {
	v, _ := nlRateLimitMap.LoadOrStore(chatID, &nlBucket{})
	b := v.(*nlBucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	// prune older than 5 min
	cutoff5 := now.Add(-5 * time.Minute)
	var kept []time.Time
	for _, t := range b.times {
		if t.After(cutoff5) {
			kept = append(kept, t)
		}
	}
	b.times = kept
	// count within 1 min
	cutoff1 := now.Add(-1 * time.Minute)
	cnt1 := 0
	for _, t := range b.times {
		if t.After(cutoff1) {
			cnt1++
		}
	}
	if cnt1 >= 10 {
		return ErrRateLimited
	}
	if len(b.times) >= 30 {
		return ErrRateLimited
	}
	b.times = append(b.times, now)
	return nil
}

// ResetRateLimiterForTest clears limiter (test helper).
func ResetRateLimiterForTest() {
	nlRateLimitMap = sync.Map{}
}

// --- Execution ---

func aiNotConfiguredReply() string {
	return "🤖 AI not configured — set provider in Admin → AI. Try: /pause /resume /next /status /sources /display <text>"
}
func invalidIntentReply() string {
	return "😕 Didn't understand — try: /pause /resume /next or rephrase (e.g., 'pause for 2 minutes')"
}
func rateLimitedReply() string {
	return "⏳ Too many requests — wait a minute."
}

// ExecuteIntent executes intent and returns reply text.
func ExecuteIntent(s *Server, intent *Intent) string {
	if intent == nil {
		return invalidIntentReply()
	}
	switch intent.Action {
	case ActionNext:
		GlobalFeed.Next()
		return "⏭ Skipped to next"
	case ActionPause:
		GlobalFeed.Pause()
		return "⏸ Paused"
	case ActionResume:
		GlobalFeed.Resume()
		return "▶ Resumed"
	case ActionPriorityDisplay:
		text := ""
		if intent.Text != nil {
			text = *intent.Text
		}
		ttl := 30
		if intent.TTLSeconds != nil {
			ttl = *intent.TTLSeconds
		}
		if s != nil {
			s.AddNotification(text, "", WithTTL(time.Duration(ttl)*time.Second))
		} else {
			addToMemoryQueueWithOptions(text, "", WithTTL(time.Duration(ttl)*time.Second))
		}
		return fmt.Sprintf("Displayed \"%s\" for %ds", text, ttl)
	case ActionSourcePinWithTTL:
		ttl := 60
		if intent.TTLSeconds != nil {
			ttl = *intent.TTLSeconds
		}
		if ttl < 5 {
			ttl = 5
		}
		if ttl > 300 {
			ttl = 300
		}
		key := fmt.Sprintf("%s:%d", *intent.SourceType, *intent.SourceID)
		GlobalFeed.Pin(key, "nl")
		// auto-unpin after TTL
		time.AfterFunc(time.Duration(ttl)*time.Second, func() {
			// only unpin if still pinned to same key
			if k, _, ok := GlobalFeed.IsPinned(); ok && k == key {
				GlobalFeed.Unpin()
			}
		})
		return fmt.Sprintf("Pinned %s for %ds", key, ttl)
	case ActionStatusQuery:
		// Build status reply similar to telegram buildStatusReply but without device count
		st := GlobalFeed.Status()
		paused, _ := st["paused"].(bool)
		current, _ := st["current"].(string)
		if current == "" {
			current = "(none)"
		}
		return fmt.Sprintf("paused: %v\ncurrent: %s", paused, current)
	default:
		return invalidIntentReply()
	}
}

// HandleNLText is the shared entry for Telegram/MQTT free-text.
func HandleNLText(ctx context.Context, s *Server, chatID int64, userText string) string {
	// check rate limit before LLM
	if err := checkRateLimit(chatID); err != nil {
		return rateLimitedReply()
	}
	cfg, ok := LoadAIConfig(s)
	if !ok {
		return aiNotConfiguredReply()
	}
	// truncate
	userText = TruncateUserText(userText)
	intent, err := ParseIntent(ctx, userText, cfg)
	if err != nil {
		if errors.Is(err, ErrAINotConfigured) {
			return aiNotConfiguredReply()
		}
		// For source not found, provide specific hint if error contains source
		if strings.Contains(err.Error(), "source_type") || strings.Contains(err.Error(), "source_id") {
			return "I couldn't find that source — try /sources to list them."
		}
		return invalidIntentReply()
	}
	return ExecuteIntent(s, intent)
}

// isAllowedChat checks Telegram allowlist (mirrors telegram.go logic).
func isAllowedChat(allowedChatID, chatID int64) bool {
	if allowedChatID == 0 {
		return true
	}
	return allowedChatID == chatID
}

// Ensure ent import used
var _ = ent.AISettings{}
