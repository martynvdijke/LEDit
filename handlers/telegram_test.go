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
	// also ensure GeneralSettings exists
	if _, err := srv.DB.GeneralSettings.Query().First(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).Save(srv.Ctx)
	}
}

// stub captures getUpdates and sendMessage.
type tgStub struct {
	mu              sync.Mutex
	getUpdatesCalls []string
	sendMessages    []struct {
		ChatID int64
		Text   string
	}
	updates [][]tgUpdate
	call    int
	status  int // for error simulation
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
		s.mu.Lock()
		s.sendMessages = append(s.sendMessages, struct {
			ChatID int64
			Text   string
		}{ChatID: int64(chatIDf), Text: text})
		s.mu.Unlock()
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
		// also alias
		if got2 := backoffDelay(c.attempt); got2 != c.want {
			t.Errorf("backoffDelay(%d)=%v want %v", c.attempt, got2, c.want)
		}
	}
}

func TestTelegramGating(t *testing.T) {
	srv := newTelegramTestServer(t)
	// no settings -> nil
	if b := StartTelegram(srv); b != nil {
		t.Fatal("expected nil when no settings")
	}
	// disabled
	createTelegramSettings(t, srv, false, "tok", 0)
	if b := StartTelegram(srv); b != nil {
		t.Fatal("expected nil when disabled")
	}
	// enabled but empty token
	srv.DB.TelegramSettings.Delete().ExecX(srv.Ctx)
	createTelegramSettings(t, srv, true, "", 0)
	if b := StartTelegram(srv); b != nil {
		t.Fatal("expected nil when empty token")
	}
}

func TestTelegramCommands(t *testing.T) {
	// Table-driven command dispatch
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
				// check notification queued via NotificationsAfter or DB
				// Use memory queue via GetNotificationHistory suffix
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
				// GlobalFeed.Next sets Skip true; check via ShouldSkip
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
			// Reset global feed
			GlobalFeed = &FeedController{}
			if tc.name == "resume" {
				GlobalFeed.Pause()
			}
			// fresh server
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
			// Use short client timeout for test speed
			b.httpc = ts.Client()
			// Override timeout to prevent long poll blocking test
			// Need to set httpc timeout low but loop uses context timeout 70s; we use server that returns immediately so no delay

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
			// extra check for status: should contain devices count
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
	// create a device for status count maybe not needed
	srv.DB.DeviceSettings.Create().SetName("dev1").SetIP("1.1.1.1").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("t").SetRefreshInterval(60).SaveX(srv.Ctx)

	stub := &tgStub{
		updates: [][]tgUpdate{
			{{UpdateID: 1, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 111}}}},
			{{UpdateID: 2, Message: &tgMessage{Text: "/next", Chat: tgChat{ID: 999}}}},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Custom handler that serves updates sequentially per call
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

	// Wait a bit for polling to process both updates
	time.Sleep(500 * time.Millisecond)
	// allowlist: first update from 111 should be ignored, second from 999 processed
	// So only one sendMessage should have happened (for allowed chat)
	// Also GlobalFeed should have been Next once
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

	// Test allowedChatID 0 accepts anyone
	srv2 := newTelegramTestServer(t)
	// Need separate DB; reuse same logic with new server
	// Create settings with 0
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
	// Wait for both updates processed
	waitForSendMessages(t, stub, 2, 2*time.Second)
	// Poll again but ensure next getUpdates carries offset=12
	// Give time for next poll
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
	_ = fmt.Sprintf // avoid unused
}

func TestTelegramShutdown(t *testing.T) {
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	// Stub that blocks getUpdates for a bit
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
	// Give loop a moment to start
	time.Sleep(100 * time.Millisecond)
	b.Stop()
	select {
	case <-b.stopped:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("expected stopped channel closed after Stop")
	}
	// idempotent second Stop should not panic
	b.Stop()
}

func TestTelegramStatusDeviceCount(t *testing.T) {
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	// 2 enabled, 1 disabled
	srv.DB.DeviceSettings.Create().SetName("a").SetIP("1.1.1.1").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("t1").SetRefreshInterval(60).SaveX(srv.Ctx)
	srv.DB.DeviceSettings.Create().SetName("b").SetIP("1.1.1.2").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(true).SetToken("t2").SetRefreshInterval(60).SaveX(srv.Ctx)
	srv.DB.DeviceSettings.Create().SetName("c").SetIP("1.1.1.3").SetPort(80).SetWidth(64).SetHeight(64).SetEnabled(false).SetToken("t3").SetRefreshInterval(60).SaveX(srv.Ctx)
	// also check query directly
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
