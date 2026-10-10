package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent/devicesettings"
)

func newTelegramTestServer(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:telegram_test.db?cache=shared&_fk=1&_busy_timeout=5000&mode=memory")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	return New(drv, nil)
}

func createTelegramSettings(t *testing.T, srv *Server, enabled bool, token string, allowedChatID int64) {
	t.Helper()
	_, err := srv.DB.TelegramSettings.Create().SetEnabled(enabled).SetBotToken(token).SetAllowedChatID(allowedChatID).Save(srv.Ctx)
	if err != nil {
		t.Fatalf("create telegram settings: %v", err)
	}
	if _, err := srv.DB.GeneralSettings.Query().First(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).Save(srv.Ctx)
	}
}

type tgStub struct {
	mu              sync.Mutex
	getUpdatesCalls []string
	sendMessages    []struct {
		ChatID      int64
		Text        string
		ReplyMarkup json.RawMessage
	}
	updates   [][]tgUpdate
	call      int
	status    int
	callbacks []string
}

func (s *tgStub) handler(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "getUpdates") {
		s.mu.Lock()
		s.getUpdatesCalls = append(s.getUpdatesCalls, r.URL.String())
		idx := s.call
		s.call++
		s.mu.Unlock()
		if s.status != 0 && s.status != http.StatusOK {
			w.WriteHeader(s.status)
			return
		}
		var res []tgUpdate
		if idx < len(s.updates) {
			res = s.updates[idx]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res})
		return
	}
	if strings.Contains(r.URL.Path, "sendMessage") {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		chatIDf, _ := body["chat_id"].(float64)
		text, _ := body["text"].(string)
		var rm json.RawMessage
		if v, ok := body["reply_markup"]; ok {
			b, _ := json.Marshal(v)
			rm = b
		}
		s.mu.Lock()
		s.sendMessages = append(s.sendMessages, struct {
			ChatID      int64
			Text        string
			ReplyMarkup json.RawMessage
		}{ChatID: int64(chatIDf), Text: text, ReplyMarkup: rm})
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	if strings.Contains(r.URL.Path, "answerCallbackQuery") {
		s.mu.Lock()
		s.callbacks = append(s.callbacks, r.URL.Path)
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	if strings.Contains(r.URL.Path, "setMyCommands") {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	w.WriteHeader(404)
}

func waitForSendMessages(t *testing.T, stub *tgStub, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stub.mu.Lock()
		got := len(stub.sendMessages)
		stub.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d sendMessages", n)
}

func TestTelegramBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{5, 32 * time.Second},
		{6, 60 * time.Second},
		{10, 60 * time.Second},
	}
	for _, c := range cases {
		got := BackoffDelay(c.attempt)
		if got != c.want {
			t.Errorf("BackoffDelay(%d)=%v want %v", c.attempt, got, c.want)
		}
		if got2 := backoffDelay(c.attempt); got2 != c.want {
			t.Errorf("backoffDelay(%d)=%v want %v", c.attempt, got2, c.want)
		}
	}
}

func TestTelegramGating(t *testing.T) {
	srv := newTelegramTestServer(t)
	if b := StartTelegram(srv); b != nil {
		t.Fatal("expected nil when no settings")
	}
	createTelegramSettings(t, srv, false, "tok", 0)
	if b := StartTelegram(srv); b != nil {
		t.Fatal("expected nil when disabled")
	}
	srv.DB.TelegramSettings.Delete().ExecX(srv.Ctx)
	createTelegramSettings(t, srv, true, "", 0)
	if b := StartTelegram(srv); b != nil {
		t.Fatal("expected nil when empty token")
	}
}

func TestTelegramCommands(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		check      func(t *testing.T, srv *Server, reply string)
		wantSubstr string
	}{
		{
			name:       "display",
			text:       "/display hello world",
			wantSubstr: "Displayed",
			check: func(t *testing.T, srv *Server, reply string) {
				h := srv.GetNotificationHistory()
				found := false
				for _, n := range h {
					if n.Title == "hello world" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected notification with title hello world")
				}
			},
		},
		{
			name:       "display case insensitive",
			text:       "/DISPLAY upper",
			wantSubstr: "Displayed",
			check: func(t *testing.T, srv *Server, reply string) {
				h := srv.GetNotificationHistory()
				found := false
				for _, n := range h {
					if n.Title == "upper" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected upper notification")
				}
			},
		},
		{
			name:       "next",
			text:       "/next",
			wantSubstr: "Skipped",
			check: func(t *testing.T, srv *Server, reply string) {
				if !GlobalFeed.ShouldSkip() {
					t.Error("expected ShouldSkip true after /next")
				}
			},
		},
		{
			name:       "pause",
			text:       "/pause",
			wantSubstr: "paused",
			check: func(t *testing.T, srv *Server, reply string) {
				if !GlobalFeed.IsPaused() {
					t.Error("expected paused")
				}
			},
		},
		{
			name:       "resume",
			text:       "/resume",
			wantSubstr: "resumed",
			check: func(t *testing.T, srv *Server, reply string) {
				if GlobalFeed.IsPaused() {
					t.Error("expected not paused after resume")
				}
			},
		},
		{
			name:       "status",
			text:       "/status",
			wantSubstr: "paused",
			check:      nil,
		},
		{
			name:       "sources",
			text:       "/sources",
			wantSubstr: "System Stats",
			check:      nil,
		},
		{
			name:       "unknown",
			text:       "/unknown",
			wantSubstr: "Commands",
			check:      nil,
		},
		{
			name:       "empty",
			text:       "",
			wantSubstr: "Commands",
			check:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			GlobalFeed = &FeedController{}
			if tc.name == "resume" {
				GlobalFeed.Pause()
			}
			srv := newTelegramTestServer(t)
			srv.DB.TelegramSettings.Delete().ExecX(srv.Ctx)
			createTelegramSettings(t, srv, true, "testtoken", 0)

			stub := &tgStub{
				updates: [][]tgUpdate{
					{
						{UpdateID: 1, Message: &tgMessage{Text: tc.text, Chat: tgChat{ID: 123}}},
					},
				},
			}
			ts := httptest.NewServer(http.HandlerFunc(stub.handler))
			defer ts.Close()

			b := StartTelegram(srv)
			if b == nil {
				t.Fatal("expected bot")
			}
			defer b.Stop()
			b.apiBase = ts.URL
			b.httpc = ts.Client()

			waitForSendMessages(t, stub, 1, 2*time.Second)
			stub.mu.Lock()
			reply := stub.sendMessages[0].Text
			stub.mu.Unlock()
			if !strings.Contains(strings.ToLower(reply), strings.ToLower(tc.wantSubstr)) {
				t.Errorf("reply %q does not contain %q", reply, tc.wantSubstr)
			}
			if tc.check != nil {
				tc.check(t, srv, reply)
			}
			if tc.name == "status" {
				if !strings.Contains(reply, "devices:") {
					t.Errorf("status reply missing devices count: %q", reply)
				}
			}
			b.Stop()
		})
	}
}

func TestTelegramAllowlist(t *testing.T) {
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 999)
	srv.DB.DeviceSettings.Create().SetName("dev1").SetIP("1.1.1.1").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("t").SetRefreshInterval(60).SaveX(srv.Ctx)

	stub := &tgStub{
		updates: [][]tgUpdate{
			{{UpdateID: 1, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 111}}}},
			{{UpdateID: 2, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 999}}}},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.handler(w, r)
	}))
	defer ts.Close()

	GlobalFeed = &FeedController{}
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	defer b.Stop()
	b.apiBase = ts.URL
	b.httpc = ts.Client()

	time.Sleep(500 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stub.mu.Lock()
		n := len(stub.sendMessages)
		stub.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stub.mu.Lock()
	n := len(stub.sendMessages)
	var chatID int64
	if n > 0 {
		chatID = stub.sendMessages[0].ChatID
	}
	stub.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 reply (allowlist), got %d", n)
	}
	if chatID != 999 {
		t.Fatalf("reply chat_id = %d want 999", chatID)
	}
	if !GlobalFeed.ShouldSkip() {
		t.Error("expected feed Next to have been triggered once")
	}

	srv2 := newTelegramTestServer(t)
	srv2.DB.TelegramSettings.Delete().ExecX(srv2.Ctx)
	createTelegramSettings(t, srv2, true, "tok2", 0)
	GlobalFeed = &FeedController{}
	stub2 := &tgStub{
		updates: [][]tgUpdate{
			{{UpdateID: 1, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 555}}}},
		},
	}
	ts2 := httptest.NewServer(http.HandlerFunc(stub2.handler))
	defer ts2.Close()
	b2 := StartTelegram(srv2)
	if b2 == nil {
		t.Fatal("expected bot2")
	}
	defer b2.Stop()
	b2.apiBase = ts2.URL
	b2.httpc = ts2.Client()
	waitForSendMessages(t, stub2, 1, 2*time.Second)
	if !GlobalFeed.ShouldSkip() {
		t.Error("expected allow all (0) to process")
	}
}

func TestTelegramCursorAdvance(t *testing.T) {
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	stub := &tgStub{
		updates: [][]tgUpdate{
			{{UpdateID: 10, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 1}}}},
			{{UpdateID: 11, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 1}}}},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer ts.Close()
	GlobalFeed = &FeedController{}
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	defer b.Stop()
	b.apiBase = ts.URL
	b.httpc = ts.Client()
	waitForSendMessages(t, stub, 2, 2*time.Second)
	time.Sleep(200 * time.Millisecond)
	stub.mu.Lock()
	calls := append([]string{}, stub.getUpdatesCalls...)
	stub.mu.Unlock()
	found := false
	for _, c := range calls {
		u, _ := url.Parse(c)
		off := u.Query().Get("offset")
		if off == "12" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected getUpdates with offset=12, got calls %v", calls)
	}
	_ = fmt.Sprintf
}

func TestTelegramShutdown(t *testing.T) {
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	stub := &tgStub{
		updates: [][]tgUpdate{{{UpdateID: 1, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 1}}}}},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			time.Sleep(50 * time.Millisecond)
		}
		stub.handler(w, r)
	}))
	defer ts.Close()
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	b.apiBase = ts.URL
	b.httpc = ts.Client()
	time.Sleep(100 * time.Millisecond)
	b.Stop()
	select {
	case <-b.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("expected stopped channel closed after Stop")
	}
	b.Stop()
}

func TestTelegramStatusDeviceCount(t *testing.T) {
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	srv.DB.DeviceSettings.Create().SetName("a").SetIP("1.1.1.1").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("t1").SetRefreshInterval(60).SaveX(srv.Ctx)
	srv.DB.DeviceSettings.Create().SetName("b").SetIP("1.1.1.2").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("t2").SetRefreshInterval(60).SaveX(srv.Ctx)
	srv.DB.DeviceSettings.Create().SetName("c").SetIP("1.1.1.3").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(false).SetToken("t3").SetRefreshInterval(60).SaveX(srv.Ctx)
	c, _ := srv.DB.DeviceSettings.Query().Where(devicesettings.Enabled(true)).Count(srv.Ctx)
	if c != 2 {
		t.Fatalf("device count %d want 2", c)
	}
	stub := &tgStub{
		updates: [][]tgUpdate{
			{{UpdateID: 1, Message: &tgMessage{Text: "/status", Chat: tgChat{ID: 1}}}},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer ts.Close()
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	defer b.Stop()
	b.apiBase = ts.URL
	b.httpc = ts.Client()
	waitForSendMessages(t, stub, 1, 2*time.Second)
	stub.mu.Lock()
	reply := stub.sendMessages[0].Text
	stub.mu.Unlock()
	if !strings.Contains(reply, "devices: 2") {
		t.Errorf("status reply %q should contain devices: 2", reply)
	}
}

func TestTelegramRegistryHelp(t *testing.T) {
	ht := helpText()
	for _, want := range []string{"/help", "/display", "/next", "/pause", "/resume", "/status", "/sources", "/devices", "/playlists", "/playlist", "/scenes", "/scene", "/brightness", "/pin", "/unpin"} {
		if !strings.Contains(ht, want) {
			t.Errorf("help missing %q in %q", want, ht)
		}
	}
}

func TestTelegramBrightness(t *testing.T) {
	GlobalFeed = &FeedController{}
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	dev := srv.DB.DeviceSettings.Create().SetName("d1").SetIP("1.1.1.1").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("tok1").SetRefreshInterval(60).SaveX(srv.Ctx)
	stub := &tgStub{updates: [][]tgUpdate{{{UpdateID: 1, Message: &tgMessage{Text: "/brightness 77", Chat: tgChat{ID: 1}}}}}}
	ts := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer ts.Close()
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	defer b.Stop()
	b.apiBase = ts.URL
	b.httpc = ts.Client()
	waitForSendMessages(t, stub, 1, 2*time.Second)
	stub.mu.Lock()
	reply := stub.sendMessages[0].Text
	stub.mu.Unlock()
	if !strings.Contains(reply, "Brightness set to 77%") {
		t.Fatalf("reply %q missing brightness", reply)
	}
	got, err := srv.DB.DeviceSettings.Get(srv.Ctx, dev.ID)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if got.BrightnessOverride == nil || *got.BrightnessOverride != 77 {
		t.Fatalf("brightness override not set, got %+v", got.BrightnessOverride)
	}
	// invalid brightness
	stub2 := &tgStub{updates: [][]tgUpdate{{{UpdateID: 1, Message: &tgMessage{Text: "/brightness 999", Chat: tgChat{ID: 1}}}}}}
	ts2 := httptest.NewServer(http.HandlerFunc(stub2.handler))
	defer ts2.Close()
	srv2 := newTelegramTestServer(t)
	createTelegramSettings(t, srv2, true, "tok", 0)
	b2 := StartTelegram(srv2)
	if b2 == nil {
		t.Fatal("expected bot2")
	}
	defer b2.Stop()
	b2.apiBase = ts2.URL
	b2.httpc = ts2.Client()
	waitForSendMessages(t, stub2, 1, 2*time.Second)
	if !strings.Contains(stub2.sendMessages[0].Text, "/brightness") {
		t.Errorf("invalid brightness should return usage, got %q", stub2.sendMessages[0].Text)
	}
}

func TestTelegramPlaylistAndScene(t *testing.T) {
	GlobalFeed = &FeedController{}
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	srv.DB.DeviceSettings.Create().SetName("d1").SetIP("1.1.1.1").SetPort(80).SetWidth(32).SetHeight(32).SetEnabled(true).SetToken("t1").SetRefreshInterval(60).SaveX(srv.Ctx)
	pl := srv.DB.Playlist.Create().SetName("pl1").SetEnabled(true).SetItems(`[{"source_type":"weather","source_id":1}]`).SetScheduleWindows("[]").SaveX(srv.Ctx)
	sc := srv.DB.Scene.Create().SetName("sc1").SetEnabled(true).SetTriggers("[]").SetActions("{}").SetPriority(0).SaveX(srv.Ctx)
	// also need GeneralSettings for apply? not needed for summary; ApplyDeviceSelect may fail but still returns summary
	stub := &tgStub{updates: [][]tgUpdate{
		{{UpdateID: 1, Message: &tgMessage{Text: "/playlists", Chat: tgChat{ID: 1}}}},
		{{UpdateID: 2, Message: &tgMessage{Text: fmt.Sprintf("/playlist %d", pl.ID), Chat: tgChat{ID: 1}}}},
		{{UpdateID: 3, Message: &tgMessage{Text: "/scenes", Chat: tgChat{ID: 1}}}},
		{{UpdateID: 4, Message: &tgMessage{Text: fmt.Sprintf("/scene %d", sc.ID), Chat: tgChat{ID: 1}}}},
	}}
	ts := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer ts.Close()
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	defer b.Stop()
	b.apiBase = ts.URL
	b.httpc = ts.Client()
	waitForSendMessages(t, stub, 4, 3*time.Second)
	// check replies contain expected
	if !strings.Contains(stub.sendMessages[0].Text, pl.Name) {
		t.Errorf("playlists reply %q missing %q", stub.sendMessages[0].Text, pl.Name)
	}
	if !strings.Contains(stub.sendMessages[1].Text, "Applied playlist") {
		t.Errorf("playlist apply reply %q", stub.sendMessages[1].Text)
	}
	if !strings.Contains(stub.sendMessages[2].Text, sc.Name) {
		t.Errorf("scenes reply %q missing %q", stub.sendMessages[2].Text, sc.Name)
	}
	if !strings.Contains(stub.sendMessages[3].Text, "Applied scene") {
		t.Errorf("scene apply reply %q", stub.sendMessages[3].Text)
	}
}

func TestTelegramCallback(t *testing.T) {
	GlobalFeed = &FeedController{}
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	// nav:/status
	stub := &tgStub{updates: [][]tgUpdate{
		{{UpdateID: 1, CallbackQuery: &tgCallbackQuery{ID: "cb1", From: tgUser{ID: 123}, Message: &tgMessage{Chat: tgChat{ID: 123}}, Data: "nav:/status"}}},
	}}
	ts := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer ts.Close()
	b := StartTelegram(srv)
	if b == nil {
		t.Fatal("expected bot")
	}
	defer b.Stop()
	b.apiBase = ts.URL
	b.httpc = ts.Client()
	waitForSendMessages(t, stub, 1, 2*time.Second)
	if !strings.Contains(stub.sendMessages[0].Text, "devices:") {
		t.Errorf("callback nav:/status reply %q missing devices", stub.sendMessages[0].Text)
	}
	// feed:pause via callback pauses feed
	GlobalFeed = &FeedController{}
	stub2 := &tgStub{updates: [][]tgUpdate{
		{{UpdateID: 1, CallbackQuery: &tgCallbackQuery{ID: "cb2", From: tgUser{ID: 123}, Message: &tgMessage{Chat: tgChat{ID: 123}}, Data: "feed:pause"}}},
	}}
	ts2 := httptest.NewServer(http.HandlerFunc(stub2.handler))
	defer ts2.Close()
	srv2 := newTelegramTestServer(t)
	createTelegramSettings(t, srv2, true, "tok", 0)
	b2 := StartTelegram(srv2)
	if b2 == nil {
		t.Fatal("expected bot2")
	}
	defer b2.Stop()
	b2.apiBase = ts2.URL
	b2.httpc = ts2.Client()
	waitForSendMessages(t, stub2, 1, 2*time.Second)
	if !GlobalFeed.IsPaused() {
		t.Error("expected paused after feed:pause callback")
	}
	if !strings.Contains(stub2.sendMessages[0].Text, "Feed paused") {
		t.Errorf("reply %q", stub2.sendMessages[0].Text)
	}
	// callback from disallowed chat ignored
	GlobalFeed = &FeedController{}
	srv3 := newTelegramTestServer(t)
	srv3.DB.TelegramSettings.Delete().ExecX(srv3.Ctx)
	createTelegramSettings(t, srv3, true, "tok", 999)
	stub3 := &tgStub{updates: [][]tgUpdate{
		{{UpdateID: 1, CallbackQuery: &tgCallbackQuery{ID: "cb3", From: tgUser{ID: 111}, Message: &tgMessage{Chat: tgChat{ID: 111}}, Data: "feed:pause"}}},
	}}
	ts3 := httptest.NewServer(http.HandlerFunc(stub3.handler))
	defer ts3.Close()
	b3 := StartTelegram(srv3)
	if b3 == nil {
		t.Fatal("expected bot3")
	}
	defer b3.Stop()
	b3.apiBase = ts3.URL
	b3.httpc = ts3.Client()
	time.Sleep(500 * time.Millisecond)
	stub3.mu.Lock()
	n := len(stub3.sendMessages)
	stub3.mu.Unlock()
	if n != 0 {
		t.Fatalf("disallowed callback should be ignored, got %d messages", n)
	}
	if GlobalFeed.IsPaused() {
		t.Error("disallowed callback should not pause")
	}
}
