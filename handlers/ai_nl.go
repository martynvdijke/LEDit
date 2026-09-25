package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"sort"
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
	ActionCreatePlaylist   = "create_playlist"
	ActionCreateScene      = "create_scene"
	ActionCreateRule       = "create_rule"
)

var validActions = map[string]bool{
	ActionNext:             true,
	ActionPause:            true,
	ActionResume:           true,
	ActionPriorityDisplay:  true,
	ActionSourcePinWithTTL: true,
	ActionStatusQuery:      true,
	ActionCreatePlaylist:   true,
	ActionCreateScene:      true,
	ActionCreateRule:       true,
}

// knownSourceTypes is the authoritative catalog from datasource.
var knownSourceTypes = datasource.KnownSourceTypes

// Intent is the validated NL intent.
type Intent struct {
	Action               string  `json:"action"`
	Text                 *string `json:"text,omitempty"`
	TTLSeconds           *int    `json:"ttl_seconds,omitempty"`
	SourceType           *string `json:"source_type,omitempty"`
	SourceID             *int    `json:"source_id,omitempty"`
	Name                 *string `json:"name,omitempty"`
	Items                *string `json:"items,omitempty"`
	ScheduleWindows      *string `json:"schedule_windows,omitempty"`
	Triggers             *string `json:"triggers,omitempty"`
	ActionsJSON          *string `json:"actions,omitempty"`
	Priority             *int    `json:"priority,omitempty"`
	Condition            *string `json:"condition,omitempty"`
	StatePath            *string `json:"state_path,omitempty"`
	CheckIntervalSeconds *int    `json:"check_interval_seconds,omitempty"`
	CooldownSeconds      *int    `json:"cooldown_seconds,omitempty"`
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
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	s = TruncateUserText(s)
	return s
}

func sanitizeName(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if n := len([]rune(trimmed)); n < 1 || n > 64 {
		return "", fmt.Errorf("%w: name length invalid", ErrInvalidIntent)
	}
	s = htmlTagRe.ReplaceAllString(trimmed, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return "", fmt.Errorf("%w: name empty after sanitization", ErrInvalidIntent)
	}
	return s, nil
}

// ValidateIntent validates raw JSON string against strict schema.
func ValidateIntent(rawJSON string) (*Intent, error) {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidIntent)
	}
	if len(rawJSON) > 8000 {
		return nil, fmt.Errorf("%w: payload too large", ErrInvalidIntent)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return nil, fmt.Errorf("%w: malformed json: %v", ErrInvalidIntent, err)
	}
	if _, ok := m["action"]; !ok {
		return nil, fmt.Errorf("%w: missing action", ErrInvalidIntent)
	}
	var actionHolder struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &actionHolder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
	}
	if !validActions[actionHolder.Action] {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidIntent, actionHolder.Action)
	}
	allowedByAction := map[string]map[string]bool{
		ActionNext:             {"action": true},
		ActionPause:            {"action": true},
		ActionResume:           {"action": true},
		ActionStatusQuery:      {"action": true},
		ActionPriorityDisplay:  {"action": true, "text": true, "ttl_seconds": true},
		ActionSourcePinWithTTL: {"action": true, "source_type": true, "source_id": true, "ttl_seconds": true},
		ActionCreatePlaylist:   {"action": true, "name": true, "items": true, "schedule_windows": true},
		ActionCreateScene:      {"action": true, "name": true, "triggers": true, "actions": true, "priority": true, "ttl_seconds": true},
		ActionCreateRule:       {"action": true, "name": true, "source_type": true, "source_id": true, "condition": true, "state_path": true, "check_interval_seconds": true, "cooldown_seconds": true},
	}
	allowed := allowedByAction[actionHolder.Action]
	for k := range m {
		if !allowed[k] {
			return nil, fmt.Errorf("%w: extra field %q for action %q", ErrInvalidIntent, k, actionHolder.Action)
		}
	}
	var intent Intent
	if err := json.Unmarshal([]byte(rawJSON), &intent); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
	}
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
	case ActionCreatePlaylist:
		if intent.Name == nil || strings.TrimSpace(*intent.Name) == "" {
			return nil, fmt.Errorf("%w: create_playlist requires name", ErrInvalidIntent)
		}
		sn, err := sanitizeName(*intent.Name)
		if err != nil {
			return nil, err
		}
		intent.Name = &sn
		if intent.Items == nil || strings.TrimSpace(*intent.Items) == "" {
			return nil, fmt.Errorf("%w: create_playlist requires items", ErrInvalidIntent)
		}
		if len(*intent.Items) > 10000 {
			return nil, fmt.Errorf("%w: items too large", ErrInvalidIntent)
		}
		items, err := datasource.ParsePlaylistItems(*intent.Items)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
		}
		if len(items) == 0 || len(items) > datasource.MaxPlaylistItems {
			return nil, fmt.Errorf("%w: items count invalid", ErrInvalidIntent)
		}
		// allow up to 65 as per spec (cap 65) – datasource caps 64; enforce 65 len check already
		if len(items) > 65 {
			return nil, fmt.Errorf("%w: too many items", ErrInvalidIntent)
		}
		if intent.ScheduleWindows != nil && strings.TrimSpace(*intent.ScheduleWindows) != "" {
			if len(*intent.ScheduleWindows) > 10000 {
				return nil, fmt.Errorf("%w: schedule_windows too large", ErrInvalidIntent)
			}
			windows, err := ParseScheduleWindows(*intent.ScheduleWindows)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
			}
			if err := ValidateWindows(windows); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
			}
		}
	case ActionCreateScene:
		if intent.Name == nil || strings.TrimSpace(*intent.Name) == "" {
			return nil, fmt.Errorf("%w: create_scene requires name", ErrInvalidIntent)
		}
		sn, err := sanitizeName(*intent.Name)
		if err != nil {
			return nil, err
		}
		intent.Name = &sn
		if intent.Triggers == nil || strings.TrimSpace(*intent.Triggers) == "" {
			return nil, fmt.Errorf("%w: create_scene requires triggers", ErrInvalidIntent)
		}
		if len(*intent.Triggers) > 10000 {
			return nil, fmt.Errorf("%w: triggers too large", ErrInvalidIntent)
		}
		groups, err := parseSceneTriggers(*intent.Triggers)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
		}
		if len(groups) == 0 {
			return nil, fmt.Errorf("%w: triggers empty", ErrInvalidIntent)
		}
		if len(groups) > 8 {
			return nil, fmt.Errorf("%w: too many trigger groups", ErrInvalidIntent)
		}
		totalConds := 0
		for _, g := range groups {
			if g.Op != "" && g.Op != "all-of" && g.Op != "any-of" {
				return nil, fmt.Errorf("%w: invalid trigger op %q", ErrInvalidIntent, g.Op)
			}
			if len(g.Conditions) == 0 {
				return nil, fmt.Errorf("%w: trigger group empty", ErrInvalidIntent)
			}
			if len(g.Conditions) > 8 {
				return nil, fmt.Errorf("%w: too many conditions in group", ErrInvalidIntent)
			}
			for _, c := range g.Conditions {
				if strings.TrimSpace(c.EntityID) == "" || strings.TrimSpace(c.Operator) == "" {
					return nil, fmt.Errorf("%w: condition missing fields", ErrInvalidIntent)
				}
				if !validSceneOperator(c.Operator) {
					return nil, fmt.Errorf("%w: invalid operator %q", ErrInvalidIntent, c.Operator)
				}
			}
			totalConds += len(g.Conditions)
		}
		if totalConds > 32 {
			return nil, fmt.Errorf("%w: too many conditions", ErrInvalidIntent)
		}
		if intent.ActionsJSON == nil || strings.TrimSpace(*intent.ActionsJSON) == "" {
			return nil, fmt.Errorf("%w: create_scene requires actions", ErrInvalidIntent)
		}
		if len(*intent.ActionsJSON) > 10000 {
			return nil, fmt.Errorf("%w: actions too large", ErrInvalidIntent)
		}
		if _, err := parseSceneActions(*intent.ActionsJSON); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
		}
		if intent.Priority != nil && (*intent.Priority < -100 || *intent.Priority > 100) {
			return nil, fmt.Errorf("%w: priority out of range", ErrInvalidIntent)
		}
		if intent.TTLSeconds != nil && (*intent.TTLSeconds < 5 || *intent.TTLSeconds > 86400) {
			return nil, fmt.Errorf("%w: ttl_seconds out of range", ErrInvalidIntent)
		}
	case ActionCreateRule:
		if intent.Name == nil || strings.TrimSpace(*intent.Name) == "" {
			return nil, fmt.Errorf("%w: create_rule requires name", ErrInvalidIntent)
		}
		sn, err := sanitizeName(*intent.Name)
		if err != nil {
			return nil, err
		}
		intent.Name = &sn
		if intent.SourceType == nil || strings.TrimSpace(*intent.SourceType) == "" {
			return nil, fmt.Errorf("%w: source_type required", ErrInvalidIntent)
		}
		st := strings.TrimSpace(*intent.SourceType)
		if !knownSourceTypes[st] {
			return nil, fmt.Errorf("%w: unknown source_type %q", ErrInvalidIntent, st)
		}
		intent.SourceType = &st
		if intent.SourceID == nil || *intent.SourceID <= 0 {
			return nil, fmt.Errorf("%w: source_id must be >0", ErrInvalidIntent)
		}
		if intent.Condition == nil || strings.TrimSpace(*intent.Condition) == "" {
			return nil, fmt.Errorf("%w: condition required", ErrInvalidIntent)
		}
		if len(*intent.Condition) > 5000 {
			return nil, fmt.Errorf("%w: condition too large", ErrInvalidIntent)
		}
		if _, err := datasource.ParseCondition(*intent.Condition); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidIntent, err)
		}
		if intent.StatePath != nil && len(*intent.StatePath) > 200 {
			return nil, fmt.Errorf("%w: state_path too long", ErrInvalidIntent)
		}
		if intent.CheckIntervalSeconds != nil && *intent.CheckIntervalSeconds < 5 {
			return nil, fmt.Errorf("%w: check_interval_seconds must be >=5", ErrInvalidIntent)
		}
		if intent.CooldownSeconds != nil && *intent.CooldownSeconds < 0 {
			return nil, fmt.Errorf("%w: cooldown_seconds must be >=0", ErrInvalidIntent)
		}
	case ActionNext, ActionPause, ActionResume, ActionStatusQuery:
		if intent.Text != nil || intent.TTLSeconds != nil || intent.SourceType != nil || intent.SourceID != nil || intent.Name != nil || intent.Items != nil || intent.ScheduleWindows != nil || intent.Triggers != nil || intent.ActionsJSON != nil || intent.Priority != nil || intent.Condition != nil || intent.StatePath != nil || intent.CheckIntervalSeconds != nil || intent.CooldownSeconds != nil {
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
{"action": enum, "text?": string, "ttl_seconds?": int, "source_type?": string, "source_id?": int, "name?": string, "items?": string, "schedule_windows?": string, "triggers?": string, "actions?": string, "priority?": int, "condition?": string, "state_path?": string, "check_interval_seconds?": int, "cooldown_seconds?": int}
Actions:
- next: advance to next source
- pause/resume: pause/resume feed
- priority_display: {text, ttl_seconds(5-300)} — show text on wall
- source_pin_with_ttl: {source_type, source_id, ttl_seconds(5-300)} — pin a source
- status_query: return current feed status
- create_playlist: {name(1-64), items(JSON array string), schedule_windows?(JSON string)} — create playlist
- create_scene: {name, triggers(JSON), actions(JSON), priority?(-100..100), ttl_seconds?(5-86400)} — create scene
- create_rule: {name, source_type, source_id, condition(JSON), state_path?, check_interval_seconds(>=5), cooldown_seconds(>=0)} — create display rule
Rules:
- If user intent doesn't match any action, return {"action":"status_query"}.
- Never return any other action or field.
Examples:
User: "pause" -> {"action":"pause"}
User: "show weather for a minute then resume" -> {"action":"source_pin_with_ttl","source_type":"weather","source_id":1,"ttl_seconds":60}
User: "hello wall for 30 seconds" -> {"action":"priority_display","text":"hello wall","ttl_seconds":30}
User: "what's playing?" -> {"action":"status_query"}
User: "create a playlist called Morning with weather" -> {"action":"create_playlist","name":"Morning","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}
User: "create scene Night with trigger" -> {"action":"create_scene","name":"Night","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"==\",\"value\":\"on\"}]}]","actions":"{}"}
User: "create rule Alert for weather" -> {"action":"create_rule","name":"Alert","source_type":"weather","source_id":1,"condition":"{\"path\":\"temp\",\"operator\":\"gt\",\"value\":30}"}
`)
	if len(availableSources) > 0 {
		sb.WriteString("Available sources:\n")
		for _, s := range availableSources {
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
	return ParseIntentWithSources(ctx, userText, cfg, nil)
}

// ParseIntentWithSources is like ParseIntent but includes available sources in prompt.
func ParseIntentWithSources(ctx context.Context, userText string, cfg datasource.AIConfig, sources []SourceInfo) (*Intent, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, ErrAINotConfigured
	}
	userText = TruncateUserText(userText)
	prompt := BuildNLPrompt(userText, sources)
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
	if strings.HasPrefix(content, "```") {
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
	if s == nil || s.DB == nil {
		return nil
	}
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Build from GeneralSettings with full edge set via buildSourceIndex (real catalog), cap 40.
	if idx, ok := sceneSourceIndex(s.DB); ok && idx != nil {
		var out []SourceInfo
		for k, name := range idx.names {
			parts := strings.SplitN(k, ":", 2)
			if len(parts) != 2 {
				continue
			}
			id := 0
			fmt.Sscanf(parts[1], "%d", &id)
			out = append(out, SourceInfo{Type: parts[0], ID: id, Name: name})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Type == out[j].Type {
				return out[i].ID < out[j].ID
			}
			return out[i].Type < out[j].Type
		})
		if len(out) > 40 {
			out = out[:40]
		}
		return out
	}
	// Fallback minimal
	var out []SourceInfo
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
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// --- Rate limiter ---

type nlBucket struct {
	times []time.Time
	mu    sync.Mutex
}

var nlRateLimitMap sync.Map // map[int64]*nlBucket
var nlCreateRateLimitMap sync.Map

func checkRateLimit(chatID int64) error {
	v, _ := nlRateLimitMap.LoadOrStore(chatID, &nlBucket{})
	b := v.(*nlBucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	cutoff5 := now.Add(-5 * time.Minute)
	var kept []time.Time
	for _, t := range b.times {
		if t.After(cutoff5) {
			kept = append(kept, t)
		}
	}
	b.times = kept
	if len(b.times) == 0 {
		// GC empty bucket to avoid unbounded sync.Map growth; recreate for this request
		nlRateLimitMap.Delete(chatID)
		b.times = nil
	}
	cutoff1 := now.Add(-1 * time.Minute)
	cnt1 := 0
	for _, t := range b.times {
		if t.After(cutoff1) {
			cnt1++
		}
	}
	if cnt1 >= 10 {
		if len(b.times) == 0 {
			nlRateLimitMap.Delete(chatID)
		}
		return ErrRateLimited
	}
	if len(b.times) >= 30 {
		if len(b.times) == 0 {
			nlRateLimitMap.Delete(chatID)
		}
		return ErrRateLimited
	}
	b.times = append(b.times, now)
	nlRateLimitMap.Store(chatID, b)
	return nil
}

func checkCreateRateLimit(chatID int64) error {
	v, _ := nlCreateRateLimitMap.LoadOrStore(chatID, &nlBucket{})
	b := v.(*nlBucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	var kept []time.Time
	for _, t := range b.times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.times = kept
	if len(b.times) == 0 {
		nlCreateRateLimitMap.Delete(chatID)
		b.times = nil
	}
	if len(b.times) >= 3 {
		if len(b.times) == 0 {
			nlCreateRateLimitMap.Delete(chatID)
		}
		return ErrRateLimited
	}
	b.times = append(b.times, now)
	nlCreateRateLimitMap.Store(chatID, b)
	return nil
}

func peekCreateRateLimited(chatID int64) bool {
	v, ok := nlCreateRateLimitMap.Load(chatID)
	if !ok {
		return false
	}
	b := v.(*nlBucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	cnt := 0
	for _, t := range b.times {
		if t.After(cutoff) {
			cnt++
		}
	}
	return cnt >= 3
}

func looksLikeCreate(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "create") || strings.Contains(lower, "make ") || strings.Contains(lower, "add ")
}

// ResetRateLimiterForTest clears limiter (test helper).
func ResetRateLimiterForTest() {
	nlRateLimitMap = sync.Map{}
	nlCreateRateLimitMap = sync.Map{}
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
func createDisabledReply() string {
	return "Creating entities by voice is disabled — enable it in Admin → AI Settings."
}

func isCreateAction(a string) bool {
	return a == ActionCreatePlaylist || a == ActionCreateScene || a == ActionCreateRule
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
		time.AfterFunc(time.Duration(ttl)*time.Second, func() {
			if k, _, ok := GlobalFeed.IsPinned(); ok && k == key {
				GlobalFeed.Unpin()
			}
		})
		return fmt.Sprintf("Pinned %s for %ds", key, ttl)
	case ActionStatusQuery:
		st := GlobalFeed.Status()
		paused, _ := st["paused"].(bool)
		current, _ := st["current"].(string)
		if current == "" {
			current = "(none)"
		}
		return fmt.Sprintf("paused: %v\ncurrent: %s", paused, current)
	case ActionCreatePlaylist:
		if s == nil || s.DB == nil {
			return invalidIntentReply()
		}
		ctx := s.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		ai, err := s.DB.AISettings.Query().Only(ctx)
		if err != nil || !ai.NlCreateEnabled {
			return createDisabledReply()
		}
		name := ""
		if intent.Name != nil {
			name = *intent.Name
		}
		itemsStr := ""
		if intent.Items != nil {
			itemsStr = *intent.Items
		}
		schedStr := "[]"
		if intent.ScheduleWindows != nil && strings.TrimSpace(*intent.ScheduleWindows) != "" {
			schedStr = *intent.ScheduleWindows
		}
		// ponytail: digest narration deferred — publish via existing outbound event/callback when needed, no scheduler here
		pl, err := s.DB.Playlist.Create().SetName(name).SetEnabled(true).SetItems(itemsStr).SetScheduleWindows(schedStr).Save(ctx)
		if err != nil {
			return fmt.Sprintf("Failed to create playlist: %v", err)
		}
		// link to GeneralSettings
		if gs, err := s.DB.GeneralSettings.Query().Only(ctx); err == nil {
			_ = s.DB.GeneralSettings.UpdateOne(gs).AddPlaylists(pl).Exec(ctx)
		}
		return fmt.Sprintf("Created playlist \"%s\" (id %d)", name, pl.ID)
	case ActionCreateScene:
		if s == nil || s.DB == nil {
			return invalidIntentReply()
		}
		ctx := s.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		ai, err := s.DB.AISettings.Query().Only(ctx)
		if err != nil || !ai.NlCreateEnabled {
			return createDisabledReply()
		}
		name := ""
		if intent.Name != nil {
			name = *intent.Name
		}
		trigStr := ""
		if intent.Triggers != nil {
			trigStr = *intent.Triggers
		}
		actStr := "{}"
		if intent.ActionsJSON != nil {
			actStr = *intent.ActionsJSON
		}
		priority := 0
		if intent.Priority != nil {
			priority = *intent.Priority
		}
		ttl := intent.TTLSeconds
		sc, err := s.DB.Scene.Create().SetName(name).SetEnabled(true).SetTriggers(trigStr).SetActions(actStr).SetPriority(priority).SetNillableTTLSeconds(ttl).Save(ctx)
		if err != nil {
			return fmt.Sprintf("Failed to create scene: %v", err)
		}
		if gs, err := s.DB.GeneralSettings.Query().Only(ctx); err == nil {
			_ = s.DB.GeneralSettings.UpdateOne(gs).AddScenes(sc).Exec(ctx)
		}
		return fmt.Sprintf("Created scene \"%s\" (id %d)", name, sc.ID)
	case ActionCreateRule:
		if s == nil || s.DB == nil {
			return invalidIntentReply()
		}
		ctx := s.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		ai, err := s.DB.AISettings.Query().Only(ctx)
		if err != nil || !ai.NlCreateEnabled {
			return createDisabledReply()
		}
		name := ""
		if intent.Name != nil {
			name = *intent.Name
		}
		st := ""
		if intent.SourceType != nil {
			st = *intent.SourceType
		}
		sid := 0
		if intent.SourceID != nil {
			sid = *intent.SourceID
		}
		cond := "{}"
		if intent.Condition != nil {
			cond = *intent.Condition
		}
		statePath := ""
		if intent.StatePath != nil {
			statePath = *intent.StatePath
		}
		ci := 30
		if intent.CheckIntervalSeconds != nil {
			ci = *intent.CheckIntervalSeconds
		}
		cooldown := 0
		if intent.CooldownSeconds != nil {
			cooldown = *intent.CooldownSeconds
		}
		rule, err := s.DB.DisplayRule.Create().SetName(name).SetEnabled(true).SetSourceType(st).SetSourceID(sid).SetCondition(cond).SetStatePath(statePath).SetCheckIntervalSeconds(ci).SetCooldownSeconds(cooldown).Save(ctx)
		if err != nil {
			return fmt.Sprintf("Failed to create rule: %v", err)
		}
		if gs, err := s.DB.GeneralSettings.Query().Only(ctx); err == nil {
			_ = s.DB.GeneralSettings.UpdateOne(gs).AddDisplayrules(rule).Exec(ctx)
		}
		return fmt.Sprintf("Created rule \"%s\" (id %d)", name, rule.ID)
	default:
		return invalidIntentReply()
	}
}

func nlCreateEnabled(s *Server) bool {
	if s == nil || s.DB == nil {
		return false
	}
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ai, err := s.DB.AISettings.Query().Only(ctx)
	if err != nil {
		return false
	}
	return ai.NlCreateEnabled
}

// HandleNLText is the shared entry for Telegram/MQTT free-text.
func HandleNLText(ctx context.Context, s *Server, chatID int64, userText string) string {
	if err := checkRateLimit(chatID); err != nil {
		return rateLimitedReply()
	}
	cfg, ok := LoadAIConfig(s)
	if !ok {
		return aiNotConfiguredReply()
	}
	userText = TruncateUserText(userText)
	// Pre-check create rate limit before LLM to avoid burning tokens; heuristic via looksLikeCreate.
	if nlCreateEnabled(s) && peekCreateRateLimited(chatID) && looksLikeCreate(userText) {
		return rateLimitedReply()
	}
	sources := AvailableSourcesForPrompt(s)
	intent, err := ParseIntentWithSources(ctx, userText, cfg, sources)
	if err != nil {
		if errors.Is(err, ErrAINotConfigured) {
			return aiNotConfiguredReply()
		}
		if strings.Contains(err.Error(), "source_type") || strings.Contains(err.Error(), "source_id") {
			return "I couldn't find that source — try /sources to list them."
		}
		return invalidIntentReply()
	}
	if isCreateAction(intent.Action) {
		if err := checkCreateRateLimit(chatID); err != nil {
			return rateLimitedReply()
		}
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
