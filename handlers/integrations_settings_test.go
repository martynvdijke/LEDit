package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

func newIntegrationTestServer(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	srv := New(drv, nil)
	if _, err := srv.DB.GeneralSettings.Query().First(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	return srv
}

func TestIntegrations_AnonGET302(t *testing.T) {
	paths := []string{"/admin/webhook", "/admin/mqtt", "/admin/telegram"}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			srv := newIntegrationTestServer(t)
			w := doRequest(t, srv, http.MethodGet, p, "")
			if w.Code != http.StatusFound {
				t.Fatalf("anonymous GET %s expected 302 got %d body %s", p, w.Code, w.Body.String())
			}
		})
	}
}

func TestIntegrations_AuthedGET200(t *testing.T) {
	cases := []struct {
		path string
		id   string
	}{
		{"/admin/webhook", `id="api_key"`},
		{"/admin/webhook", `id="default_ttl"`},
		{"/admin/mqtt", `id="enabled"`},
		{"/admin/mqtt", `id="broker"`},
		{"/admin/mqtt", `id="control_topic"`},
		{"/admin/mqtt", `id="display_topic"`},
		{"/admin/mqtt", `id="password"`},
		{"/admin/telegram", `id="enabled"`},
		{"/admin/telegram", `id="bot_token"`},
		{"/admin/telegram", `id="allowed_chat_id"`},
	}
	srv := newIntegrationTestServer(t)
	session := loginAsAdmin(t, srv)
	for _, tc := range cases {
		t.Run(tc.path+" "+tc.id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.AddCookie(session)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s expected 200 got %d body %s", tc.path, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.id) {
				t.Fatalf("expected %s in body", tc.id)
			}
		})
	}
}

func TestWebhook_SavePersists(t *testing.T) {
	srv := newIntegrationTestServer(t)
	session := loginAsAdmin(t, srv)
	form := url.Values{}
	form.Set("api_key", "mykey123")
	form.Set("default_ttl", "42")
	req := httptest.NewRequest(http.MethodPost, "/admin/webhook", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 got %d %s", w.Code, w.Body.String())
	}
	ws, err := srv.DB.WebhookSettings.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query webhook settings: %v", err)
	}
	if ws.APIKey != "mykey123" || ws.DefaultTTL != 42 {
		t.Fatalf("unexpected saved %+v", ws)
	}
	// round-trip: GET should contain saved values
	req2 := httptest.NewRequest(http.MethodGet, "/admin/webhook", nil)
	req2.AddCookie(session)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	body := w2.Body.String()
	if !strings.Contains(body, "mykey123") || !strings.Contains(body, `value="42"`) {
		t.Fatalf("round-trip missing values %s", body[:500])
	}
	// empty key shows warning
	srv.DB.WebhookSettings.Update().SetAPIKey("").SaveX(context.Background())
	req3 := httptest.NewRequest(http.MethodGet, "/admin/webhook", nil)
	req3.AddCookie(session)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if !strings.Contains(w3.Body.String(), "API key is empty") {
		t.Fatalf("expected warning banner")
	}
}

func TestMQTT_EnabledWithoutBrokerRejected(t *testing.T) {
	srv := newIntegrationTestServer(t)
	session := loginAsAdmin(t, srv)
	form := url.Values{}
	form.Set("enabled", "on")
	form.Set("broker", "")
	form.Set("control_topic", "ledit/control")
	form.Set("display_topic", "ledit/display")
	req := httptest.NewRequest(http.MethodPost, "/admin/mqtt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validation re-render expected 200 got %d", w.Code)
	}
	count, _ := srv.DB.MQTTSettings.Query().Count(context.Background())
	if count != 0 {
		t.Fatalf("should not persist invalid mqtt, count %d", count)
	}
}

func TestTelegram_EnabledWithoutTokenRejected(t *testing.T) {
	srv := newIntegrationTestServer(t)
	session := loginAsAdmin(t, srv)
	form := url.Values{}
	form.Set("enabled", "on")
	form.Set("bot_token", "")
	form.Set("allowed_chat_id", "0")
	req := httptest.NewRequest(http.MethodPost, "/admin/telegram", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	count, _ := srv.DB.TelegramSettings.Query().Count(context.Background())
	if count != 0 {
		t.Fatalf("should not persist invalid telegram")
	}
}

func TestMQTT_ValidSavePersists(t *testing.T) {
	srv := newIntegrationTestServer(t)
	session := loginAsAdmin(t, srv)
	form := url.Values{}
	form.Set("enabled", "on")
	form.Set("broker", "tcp://broker:1883")
	form.Set("username", "u")
	form.Set("password", "p")
	form.Set("control_topic", "my/control")
	form.Set("display_topic", "my/display")
	req := httptest.NewRequest(http.MethodPost, "/admin/mqtt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 got %d %s", w.Code, w.Body.String())
	}
	ms, err := srv.DB.MQTTSettings.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if ms.Broker != "tcp://broker:1883" || ms.ControlTopic != "my/control" || ms.DisplayTopic != "my/display" {
		t.Fatalf("unexpected mqtt %+v", ms)
	}
}
