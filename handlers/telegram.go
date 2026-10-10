package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
	// best-effort setMyCommands
	go b.setMyCommands()
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
	UpdateID      int64            `json:"update_id"`
	Message       *tgMessage       `json:"message"`
	CallbackQuery *tgCallbackQuery `json:"callback_query"`
}

type tgMessage struct {
	Text string `json:"text"`
	Chat tgChat `json:"chat"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
	ChatID  int64      `json:"-"` // helper not from json
}

type tgUser struct {
	ID int64 `json:"id"`
}

type tgGetUpdatesResp struct {
	Ok     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

// inline keyboard types
type tgInlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type tgInlineKeyboardMarkup struct {
	InlineKeyboard [][]tgInlineKeyboardButton `json:"inline_keyboard"`
}

func (b *TelegramBot) pollOnce() error {
	url := fmt.Sprintf("%s/bot%s/getUpdates?timeout=60&offset=%d&allowed_updates=[\"message\",\"callback_query\"]", b.apiBase, b.token, b.offset)

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
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
		if u.CallbackQuery != nil {
			// determine chatID for allowlist: from message chat if present else from user
			chatID := int64(0)
			if u.CallbackQuery.Message != nil {
				chatID = u.CallbackQuery.Message.Chat.ID
			}
			if chatID == 0 {
				chatID = u.CallbackQuery.From.ID
			}
			if b.allowedChatID != 0 && chatID != b.allowedChatID {
				slog.Debug("telegram callback from disallowed chat", "chat_id", chatID)
			} else {
				b.handleCallback(u.CallbackQuery, chatID)
			}
		} else if u.Message != nil {
			if b.allowedChatID != 0 && u.Message.Chat.ID != b.allowedChatID {
				slog.Debug("telegram message from disallowed chat", "chat_id", u.Message.Chat.ID)
			} else {
				b.handleMessage(u.Message)
			}
		}
		if u.UpdateID+1 > b.offset {
			b.offset = u.UpdateID + 1
		}
	}
	return nil
}

// --- command registry ---

type tgCommand struct {
	name    string
	usage   string
	desc    string
	aliases []string
	showNav bool
	run     func(b *TelegramBot, chatID int64, args string) string
}

var telegramCommands []tgCommand

func init() {
	telegramCommands = []tgCommand{
		{name: "help", usage: "/help", desc: "show help", aliases: []string{"start"}, showNav: true, run: func(b *TelegramBot, chatID int64, args string) string { return helpText() }},
		{name: "display", usage: "/display <text>", desc: "display text", aliases: nil, showNav: false, run: runDisplay},
		{name: "next", usage: "/next", desc: "next source", aliases: nil, showNav: false, run: func(b *TelegramBot, chatID int64, args string) string { GlobalFeed.Next(); return "Skipped to next" }},
		{name: "pause", usage: "/pause", desc: "pause feed", aliases: nil, showNav: false, run: func(b *TelegramBot, chatID int64, args string) string { GlobalFeed.Pause(); return "Feed paused" }},
		{name: "resume", usage: "/resume", desc: "resume feed", aliases: nil, showNav: false, run: func(b *TelegramBot, chatID int64, args string) string { GlobalFeed.Resume(); return "Feed resumed" }},
		{name: "status", usage: "/status", desc: "feed status", aliases: nil, showNav: true, run: func(b *TelegramBot, chatID int64, args string) string { return b.buildStatusReply() }},
		{name: "sources", usage: "/sources", desc: "list sources", aliases: nil, showNav: true, run: func(b *TelegramBot, chatID int64, args string) string { return b.buildSourcesReply() }},
		{name: "devices", usage: "/devices", desc: "list devices", aliases: nil, showNav: true, run: runDevices},
		{name: "playlists", usage: "/playlists", desc: "list playlists", aliases: nil, showNav: false, run: runPlaylists},
		{name: "playlist", usage: "/playlist <id>", desc: "apply playlist to all devices", aliases: nil, showNav: false, run: runPlaylistApply},
		{name: "scenes", usage: "/scenes", desc: "list scenes", aliases: nil, showNav: false, run: runScenes},
		{name: "scene", usage: "/scene <id>", desc: "apply scene to all devices", aliases: nil, showNav: false, run: runSceneApply},
		{name: "brightness", usage: "/brightness <0-100>", desc: "set brightness", aliases: nil, showNav: false, run: runBrightness},
		{name: "pin", usage: "/pin <type:id>", desc: "pin source", aliases: nil, showNav: false, run: runPin},
		{name: "unpin", usage: "/unpin", desc: "unpin source", aliases: nil, showNav: false, run: runUnpin},
	}
}

func helpText() string {
	var sb strings.Builder
	sb.WriteString("Commands:\n")
	for i, c := range telegramCommands {
		sb.WriteString(c.usage + " - " + c.desc)
		if i < len(telegramCommands)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func normalizeCommand(raw string) (name, args string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	// split command and args
	spaceIdx := strings.Index(raw, " ")
	cmdPart := raw
	if spaceIdx != -1 {
		cmdPart = raw[:spaceIdx]
		args = strings.TrimSpace(raw[spaceIdx+1:])
	}
	// strip leading /
	if strings.HasPrefix(cmdPart, "/") {
		cmdPart = cmdPart[1:]
	}
	// strip @botname
	if idx := strings.Index(cmdPart, "@"); idx != -1 {
		cmdPart = cmdPart[:idx]
	}
	cmdPart = strings.ToLower(cmdPart)
	return cmdPart, args
}

func findCommand(name string) *tgCommand {
	norm, _ := normalizeCommand("/" + name)
	for i := range telegramCommands {
		c := &telegramCommands[i]
		if c.name == norm {
			return c
		}
		for _, a := range c.aliases {
			if a == norm {
				return c
			}
		}
	}
	return nil
}

// command runners
func runDisplay(b *TelegramBot, chatID int64, args string) string {
	if strings.TrimSpace(args) == "" {
		return helpText()
	}
	b.s.AddNotification(strings.TrimSpace(args), "", WithTTL(time.Duration(b.s.webhookDefaultTTL())*time.Second))
	return "Displayed"
}

func runDevices(b *TelegramBot, chatID int64, args string) string {
	if b.s == nil || b.s.DB == nil {
		return "No devices configured"
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	devs, err := b.s.DB.DeviceSettings.Query().Where(devicesettings.Enabled(true)).All(ctx)
	if err != nil || len(devs) == 0 {
		return "No devices configured"
	}
	var sb strings.Builder
	for i, d := range devs {
		fmt.Fprintf(&sb, "#%d %s %dx%d", d.ID, d.Name, d.Width, d.Height)
		if i < len(devs)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func runPlaylists(b *TelegramBot, chatID int64, args string) string {
	if b.s == nil || b.s.DB == nil {
		return "No playlists configured"
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	pls, err := b.s.DB.Playlist.Query().All(ctx)
	if err != nil || len(pls) == 0 {
		return "No playlists configured"
	}
	var sb strings.Builder
	for i, p := range pls {
		enabledStr := "disabled"
		if p.Enabled {
			enabledStr = "enabled"
		}
		// count items
		n := 0
		if strings.TrimSpace(p.Items) != "" {
			var arr []any
			if err := json.Unmarshal([]byte(p.Items), &arr); err == nil {
				n = len(arr)
			}
		}
		fmt.Fprintf(&sb, "#%d %s [%s] - %d items", p.ID, p.Name, enabledStr, n)
		if i < len(pls)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func runPlaylistApply(b *TelegramBot, chatID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "/playlist <id> - apply playlist to all devices"
	}
	id, err := strconv.Atoi(args)
	if err != nil || id <= 0 {
		return "/playlist <id> - apply playlist to all devices"
	}
	if b.s == nil || b.s.DB == nil {
		return fmt.Sprintf("Applied playlist %d to 0 device(s)", id)
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	devs, err := b.s.DB.DeviceSettings.Query().Where(devicesettings.Enabled(true)).All(ctx)
	if err != nil {
		return fmt.Sprintf("Applied playlist %d to 0 device(s)", id)
	}
	var errs []string
	for _, d := range devs {
		if e := b.s.ApplyDeviceSelect(d.ID, fmt.Sprintf("playlist:%d", id)); e != nil {
			errs = append(errs, fmt.Sprintf("device %d: %v", d.ID, e))
		}
	}
	msg := fmt.Sprintf("Applied playlist %d to %d device(s)", id, len(devs))
	if len(errs) > 0 {
		msg += "\n" + strings.Join(errs, "\n")
	}
	return msg
}

func runScenes(b *TelegramBot, chatID int64, args string) string {
	if b.s == nil || b.s.DB == nil {
		return "No scenes configured"
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	scenes, err := b.s.DB.Scene.Query().All(ctx)
	if err != nil || len(scenes) == 0 {
		return "No scenes configured"
	}
	var sb strings.Builder
	for i, sc := range scenes {
		enabledStr := "disabled"
		if sc.Enabled {
			enabledStr = "enabled"
		}
		fmt.Fprintf(&sb, "#%d %s [%s]", sc.ID, sc.Name, enabledStr)
		if i < len(scenes)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func runSceneApply(b *TelegramBot, chatID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "/scene <id> - apply scene to all devices"
	}
	id, err := strconv.Atoi(args)
	if err != nil || id <= 0 {
		return "/scene <id> - apply scene to all devices"
	}
	if b.s == nil || b.s.DB == nil {
		return fmt.Sprintf("Applied scene %d to 0 device(s)", id)
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	devs, err := b.s.DB.DeviceSettings.Query().Where(devicesettings.Enabled(true)).All(ctx)
	if err != nil {
		return fmt.Sprintf("Applied scene %d to 0 device(s)", id)
	}
	var errs []string
	for _, d := range devs {
		if e := b.s.ApplyDeviceSelect(d.ID, fmt.Sprintf("scene:%d", id)); e != nil {
			errs = append(errs, fmt.Sprintf("device %d: %v", d.ID, e))
		}
	}
	msg := fmt.Sprintf("Applied scene %d to %d device(s)", id, len(devs))
	if len(errs) > 0 {
		msg += "\n" + strings.Join(errs, "\n")
	}
	return msg
}

func runBrightness(b *TelegramBot, chatID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "/brightness <0-100> - set brightness"
	}
	lvl, err := strconv.Atoi(args)
	if err != nil {
		return "/brightness <0-100> - set brightness"
	}
	if lvl < 0 || lvl > 100 {
		return "/brightness <0-100> - set brightness"
	}
	if b.s == nil || b.s.DB == nil {
		return fmt.Sprintf("Brightness set to %d%% on 0 device(s)", lvl)
	}
	ctx := b.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	devs, err := b.s.DB.DeviceSettings.Query().Where(devicesettings.Enabled(true)).All(ctx)
	if err != nil {
		return fmt.Sprintf("Brightness set to %d%% on 0 device(s)", lvl)
	}
	for _, d := range devs {
		b.s.ApplyDeviceBrightness(d.ID, lvl, "telegram")
	}
	return fmt.Sprintf("Brightness set to %d%% on %d device(s)", lvl, len(devs))
}

func runPin(b *TelegramBot, chatID int64, args string) string {
	args = strings.TrimSpace(args)
	if args == "" || !strings.Contains(args, ":") {
		return "/pin <type:id> - pin source"
	}
	GlobalFeed.Pin(args, "telegram")
	return fmt.Sprintf("Pinned %s", args)
}

func runUnpin(b *TelegramBot, chatID int64, args string) string {
	GlobalFeed.Unpin()
	return "Unpinned"
}

func (b *TelegramBot) handleMessage(msg *tgMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		b.sendReplyWithKeyboard(msg.Chat.ID, helpText(), navKeyboard())
		return
	}
	if strings.HasPrefix(text, "/") {
		name, args := normalizeCommand(text)
		if name == "" {
			b.sendReplyWithKeyboard(msg.Chat.ID, helpText(), navKeyboard())
			return
		}
		cmd := findCommand(name)
		if cmd == nil {
			b.sendReplyWithKeyboard(msg.Chat.ID, helpText(), navKeyboard())
			return
		}
		reply := cmd.run(b, msg.Chat.ID, args)
		var kb *tgInlineKeyboardMarkup
		if cmd.showNav {
			kb = navKeyboard()
		}
		b.sendReplyWithKeyboard(msg.Chat.ID, reply, kb)
		return
	}
	// Free-text: allowlist still gates before LLM cost
	reply := HandleNLText(context.Background(), b.s, msg.Chat.ID, text)
	b.sendReply(msg.Chat.ID, reply)
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
	b.sendReplyWithKeyboard(chatID, text, nil)
}

func (b *TelegramBot) sendReplyWithKeyboard(chatID int64, text string, markup *tgInlineKeyboardMarkup) {
	url := fmt.Sprintf("%s/bot%s/sendMessage", b.apiBase, b.token)
	payload := map[string]any{"chat_id": chatID, "text": text}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	body, _ := json.Marshal(payload)
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

func navKeyboard() *tgInlineKeyboardMarkup {
	paused := false
	if st := GlobalFeed.Status(); st != nil {
		if p, ok := st["paused"].(bool); ok {
			paused = p
		}
	}
	middleText := "⏸ Pause"
	middleData := "feed:pause"
	if paused {
		middleText = "▶ Resume"
		middleData = "feed:resume"
	}
	return &tgInlineKeyboardMarkup{
		InlineKeyboard: [][]tgInlineKeyboardButton{
			{
				{Text: "▶ Next", CallbackData: "nav:/next"},
				{Text: middleText, CallbackData: middleData},
				{Text: "📊 Status", CallbackData: "nav:/status"},
			},
			{
				{Text: "📺 Sources", CallbackData: "nav:/sources"},
				{Text: "🖥 Devices", CallbackData: "nav:/devices"},
				{Text: "❓ Help", CallbackData: "nav:/help"},
			},
		},
	}
}

func (b *TelegramBot) handleCallback(q *tgCallbackQuery, chatID int64) {
	_ = b.answerCallbackQuery(q.ID)
	data := strings.TrimSpace(q.Data)
	switch {
	case data == "feed:pause":
		GlobalFeed.Pause()
		b.sendReply(chatID, "Feed paused")
	case data == "feed:resume":
		GlobalFeed.Resume()
		b.sendReply(chatID, "Feed resumed")
	case strings.HasPrefix(data, "nav:/"):
		cmdName := strings.TrimPrefix(data, "nav:/")
		// normalize: cmdName may include extra?
		name, _ := normalizeCommand("/" + cmdName)
		cmd := findCommand(name)
		if cmd == nil {
			return
		}
		reply := cmd.run(b, chatID, "")
		var kb *tgInlineKeyboardMarkup
		if cmd.showNav {
			kb = navKeyboard()
		}
		b.sendReplyWithKeyboard(chatID, reply, kb)
	default:
		// unknown -> ignore (already answered)
	}
}

func (b *TelegramBot) answerCallbackQuery(callbackID string) error {
	url := fmt.Sprintf("%s/bot%s/answerCallbackQuery", b.apiBase, b.token)
	body, _ := json.Marshal(map[string]any{"callback_query_id": callbackID})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("answerCallbackQuery status %d", resp.StatusCode)
	}
	return nil
}

func (b *TelegramBot) setMyCommands() {
	url := fmt.Sprintf("%s/bot%s/setMyCommands", b.apiBase, b.token)
	// Build commands from registry
	var cmds []map[string]string
	for _, c := range telegramCommands {
		cmds = append(cmds, map[string]string{"command": c.name, "description": c.desc})
	}
	body, _ := json.Marshal(map[string]any{"commands": cmds})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("telegram setMyCommands failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("telegram setMyCommands non-200", "status", resp.StatusCode)
	}
}

// SendTelegramTestMessage sends a test message via Telegram Bot API.
func SendTelegramTestMessage(token string, chatID int64, text string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("empty token")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram sendMessage status %d", resp.StatusCode)
	}
	var r struct {
		Ok bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if !r.Ok {
		return fmt.Errorf("telegram not ok")
	}
	return nil
}
