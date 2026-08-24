package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
)

// TelegramBot polls Telegram getUpdates and dispatches commands.
type TelegramBot struct {
	s             *Server
	token         string
	allowedChatID int64
	apiBase       string
	httpc         *http.Client
	offset        int64
	stop          chan struct{}
	stopped       chan struct{}
	mu            sync.Mutex
}

// LoadTelegramSettings returns the first TelegramSettings row or nil.
// Nil-safe for nil server/client.
func LoadTelegramSettings(s *Server) *ent.TelegramSettings {
	if s == nil || s.DB == nil {
		return nil
	}
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ts, err := s.DB.TelegramSettings.Query().First(ctx)
	if err != nil {
		return nil
	}
	return ts
}

// backoffDelay alias uses BackoffDelay defined in mqtt.go.
// StartTelegram starts the bot if enabled and token is non-empty, otherwise returns nil.
func StartTelegram(s *Server) *TelegramBot {
	if s == nil || s.DB == nil {
		return nil
	}
	ts := LoadTelegramSettings(s)
	if ts == nil || !ts.Enabled || strings.TrimSpace(ts.BotToken) == "" {
		return nil
	}
	b := &TelegramBot{
		s:             s,
		token:         ts.BotToken,
		allowedChatID: ts.AllowedChatID,
		apiBase:       "https://api.telegram.org",
		httpc:         &http.Client{Timeout: 70 * time.Second},
		stop:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
	go b.loop()
	return b
}

// Stop idempotently stops the polling loop.
func (b *TelegramBot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.stop:
		// already closed
	default:
		close(b.stop)
	}
}

// RestartWithSettings stops the current bot and starts a new one with fresh settings.
func (b *TelegramBot) RestartWithSettings(s *Server) *TelegramBot {
	if b != nil {
		b.Stop()
		// Wait for loop to exit, with timeout to avoid blocking forever.
		select {
		case <-b.stopped:
		case <-time.After(2 * time.Second):
		}
	}
	return StartTelegram(s)
}

func (b *TelegramBot) loop() {
	defer close(b.stopped)
	attempt := 0
	for {
		select {
		case <-b.stop:
			return
		default:
		}

		if err := b.pollOnce(); err != nil {
			slog.Warn("telegram poll error", "error", err, "attempt", attempt)
			d := backoffDelay(attempt)
			attempt++
			select {
			case <-b.stop:
				return
			case <-time.After(d):
			}
			continue
		}
		attempt = 0
	}
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	Text string `json:"text"`
	Chat tgChat `json:"chat"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgGetUpdatesResp struct {
	Ok     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

func (b *TelegramBot) pollOnce() error {
	// Build URL with timeout 60 and cursor offset.
	url := fmt.Sprintf("%s/bot%s/getUpdates?timeout=60&offset=%d&allowed_updates=[\"message\"]", b.apiBase, b.token, b.offset)

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	// Also abort if stop is signaled
	go func() {
		select {
		case <-b.stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := b.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram getUpdates status %d", resp.StatusCode)
	}
	var gr tgGetUpdatesResp
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return err
	}
	for _, u := range gr.Result {
		if u.Message != nil {
			if b.allowedChatID != 0 && u.Message.Chat.ID != b.allowedChatID {
				slog.Debug("telegram message from disallowed chat", "chat_id", u.Message.Chat.ID)
			} else {
				b.handleMessage(u.Message)
			}
		}
		// Advance cursor after processing.
		if u.UpdateID+1 > b.offset {
			b.offset = u.UpdateID + 1
		}
	}
	return nil
}

func (b *TelegramBot) handleMessage(msg *tgMessage) {
	text := strings.TrimSpace(msg.Text)
	lower := strings.ToLower(text)

	// Slash commands bypass NL entirely
	isSlash := strings.HasPrefix(lower, "/")
	var reply string

	switch {
	case strings.HasPrefix(lower, "/display"):
		// Extract payload after command, preserving original case.
		rest := ""
		if len(text) >= len("/display") {
			rest = strings.TrimSpace(text[len("/display"):])
			// Handle /display@botname case: strip @bot suffix
			if strings.HasPrefix(rest, "@") {
				// find space after @bot
				if idx := strings.Index(rest, " "); idx != -1 {
					rest = strings.TrimSpace(rest[idx+1:])
				} else {
					rest = ""
				}
			}
		}
		// Also handle "/display@bot hello" where lower includes @
		// Normalize by checking lower prefix with @
		if rest == "" && lower != "/display" && !strings.HasPrefix(lower, "/display ") {
			// Check for @bot variant
			if strings.HasPrefix(lower, "/display@") {
				// extract after first space
				if idx := strings.Index(text, " "); idx != -1 {
					rest = strings.TrimSpace(text[idx+1:])
				}
			}
		}
		if rest == "" {
			reply = helpText()
		} else {
			b.s.AddNotification(rest, "", WithTTL(time.Duration(b.s.webhookDefaultTTL())*time.Second))
			reply = "Displayed"
		}
	case lower == "/next" || strings.HasPrefix(lower, "/next ") || strings.HasPrefix(lower, "/next@"):
		GlobalFeed.Next()
		reply = "Skipped to next"
	case lower == "/pause" || strings.HasPrefix(lower, "/pause ") || strings.HasPrefix(lower, "/pause@"):
		GlobalFeed.Pause()
		reply = "Feed paused"
	case lower == "/resume" || strings.HasPrefix(lower, "/resume ") || strings.HasPrefix(lower, "/resume@"):
		GlobalFeed.Resume()
		reply = "Feed resumed"
	case lower == "/status" || strings.HasPrefix(lower, "/status ") || strings.HasPrefix(lower, "/status@"):
		reply = b.buildStatusReply()
	case lower == "/sources" || strings.HasPrefix(lower, "/sources ") || strings.HasPrefix(lower, "/sources@"):
		reply = b.buildSourcesReply()
	default:
		if isSlash {
			reply = helpText()
		} else if text == "" {
			reply = helpText()
		} else {
			// Free-text: allowlist still gates before LLM cost
			if b.allowedChatID != 0 && msg.Chat.ID != b.allowedChatID {
				// already filtered in pollOnce, but keep safe
				reply = helpText()
			} else {
				reply = HandleNLText(context.Background(), b.s, msg.Chat.ID, text)
			}
		}
	}

	b.sendReply(msg.Chat.ID, reply)
}

func helpText() string {
	return "Commands:\n/display <text> - display text\n/next - next source\n/pause - pause feed\n/resume - resume feed\n/status - feed status\n/sources - list sources"
}

func (b *TelegramBot) buildStatusReply() string {
	st := GlobalFeed.Status()
	paused, _ := st["paused"].(bool)
	current, _ := st["current"].(string)
	next, _ := st["next"].(string)
	var sb strings.Builder
	fmt.Fprintf(&sb, "paused: %v", paused)
	if current != "" {
		fmt.Fprintf(&sb, "\ncurrent: %s", current)
	}
	if next != "" {
		fmt.Fprintf(&sb, "\nnext: %s", next)
	}
	if pk, ok := st["pinned_key"].(string); ok && pk != "" {
		fmt.Fprintf(&sb, "\npinned_key: %s", pk)
	}
	if pb, ok := st["pinned_by"].(string); ok && pb != "" {
		fmt.Fprintf(&sb, "\npinned_by: %s", pb)
	}
	// enabled device count
	devCount := 0
	if b.s != nil && b.s.DB != nil {
		ctx := b.s.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		c, err := b.s.DB.DeviceSettings.Query().Where(devicesettings.Enabled(true)).Count(ctx)
		if err == nil {
			devCount = c
		}
	}
	fmt.Fprintf(&sb, "\ndevices: %d", devCount)
	return sb.String()
}

func (b *TelegramBot) buildSourcesReply() string {
	if b.s == nil || b.s.WSHub == nil || b.s.DB == nil {
		return "No sources configured"
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	gs, err := b.s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().
		WithImages().WithVideos().WithCrypto().WithRssFeeds().WithCalendars().WithStocks().
		WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().
		WithMatrixLayouts().WithCountdowns().WithAiDigests().Only(ctx)
	if err != nil {
		return "No sources configured"
	}
	sources := b.s.WSHub.loadSources(gs)
	if len(sources) == 0 {
		return "No sources configured"
	}
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	return strings.Join(names, "\n")
}

func (b *TelegramBot) sendReply(chatID int64, text string) {
	url := fmt.Sprintf("%s/bot%s/sendMessage", b.apiBase, b.token)
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("telegram sendReply new request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpc.Do(req)
	if err != nil {
		slog.Warn("telegram sendMessage failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("telegram sendMessage non-200", "status", resp.StatusCode)
	}
}
