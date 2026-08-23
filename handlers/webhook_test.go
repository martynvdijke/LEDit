package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func webhookTestServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed general: %v", err)
	}
	r := gin.New()
	srv := &Server{DB: client, Ctx: ctx, Router: r}
	// setup api routes like server.go for webhook tests
	api := r.Group("/api")
	api.POST("/feed/priority", srv.WebhookAuthMiddleware(), srv.APIFeedPriority)
	api.POST("/webhook/notify", srv.WebhookAuthMiddleware(), srv.APIWebhookNotify)
	api.GET("/display", srv.WebhookAuthMiddleware(), srv.APIDisplay)
	return srv, client
}

func clearNotifHistory() {
	priorityMu.Lock()
	notifHistory = nil
	notifID = 0
	priorityMu.Unlock()
}

func TestWebhookAuth_NoKeyNoop(t *testing.T) {
	srv, _ := webhookTestServer(t)
	clearNotifHistory()
	body := `{"title":"a","message":"b"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("no key should be no-op 200, got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookAuth_ValidHeader(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("secret123").SetDefaultTTL(30).SaveX(context.Background())
	// valid header
	body := `{"title":"a","message":"b"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret123")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid header expected 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookAuth_ValidQueryToken(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("secret123").SetDefaultTTL(30).SaveX(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify?token=secret123", bytes.NewBufferString(`{"title":"a","message":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid query token expected 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookAuth_WrongKey401(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("secret123").SetDefaultTTL(30).SaveX(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(`{"title":"a","message":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "wrong")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key expected 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookAuth_WhitespaceTrimmed(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("secret123").SetDefaultTTL(30).SaveX(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(`{"title":"a","message":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "  secret123  ")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("whitespace trimmed expected 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestDisplay_TTLClamp(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("").SetDefaultTTL(30).SaveX(context.Background())

	cases := []struct {
		ttl      string
		expected int
	}{
		{"0", 30},
		{"99999", 3600},
		{"-5", 1},
		{"10", 10},
	}
	for _, c := range cases {
		clearNotifHistory()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/display?text=hello&ttl="+c.ttl, nil)
		srv.Router.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("ttl %s expected 202 got %d %s", c.ttl, w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		got := int(resp["ttl"].(float64))
		if got != c.expected {
			t.Fatalf("ttl %s expected %d got %d resp %v", c.ttl, c.expected, got, resp)
		}
	}
}

func TestDisplay_MissingText400(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("").SetDefaultTTL(30).SaveX(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/display?ttl=10", nil)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing text expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestDisplay_ResponseShape(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("").SetDefaultTTL(30).SaveX(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/display?text=hello&color=%23ff0000", nil)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json %v", err)
	}
	if _, ok := resp["id"]; !ok {
		t.Fatalf("missing id")
	}
	if _, ok := resp["ttl"]; !ok {
		t.Fatalf("missing ttl")
	}
	ea, ok := resp["expires_at"].(string)
	if !ok {
		t.Fatalf("missing expires_at")
	}
	if _, err := time.Parse(time.RFC3339, ea); err != nil {
		t.Fatalf("expires_at not RFC3339: %v", err)
	}
	// color hint stored
	found := false
	for _, n := range getMemoryQueue() {
		if n.Title == "hello" && n.Color == "#ff0000" {
			found = true
		}
	}
	if !found {
		t.Fatalf("color hint not stored")
	}
}

func TestNotifExpiryPruning(t *testing.T) {
	clearNotifHistory()
	// Seed expired + live
	expired := time.Now().Add(-10 * time.Second)
	live := time.Now().Add(100 * time.Second)
	addToMemoryQueueWithOptions("expired", "msg", withExpiresAt(expired))
	addToMemoryQueueWithOptions("live", "msg", withExpiresAt(live))
	addToMemoryQueueWithOptions("never", "msg")

	// cursor 0 should drop expired
	out := NotificationsAfter(0)
	for _, n := range out {
		if n.Title == "expired" {
			t.Fatalf("expired entry should be pruned")
		}
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 live entries got %d %v", len(out), out)
	}
	// queue pruned
	q := getMemoryQueue()
	for _, n := range q {
		if n.Title == "expired" {
			t.Fatalf("queue still contains expired")
		}
	}
	// broadcast once per cursor
	cursor := 0
	first := NotificationsAfter(cursor)
	if len(first) != 2 {
		t.Fatalf("expected 2")
	}
	// advance cursor to last ID
	maxID := 0
	for _, n := range first {
		if n.ID > maxID {
			maxID = n.ID
		}
	}
	second := NotificationsAfter(maxID)
	if len(second) != 0 {
		t.Fatalf("second read should be empty, got %d", len(second))
	}
}

func TestDisplayBackwardCompatWebhookNoKey(t *testing.T) {
	srv, client := webhookTestServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("").SetDefaultTTL(30).SaveX(context.Background())
	// webhook notify without auth should still work
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(`{"title":"t","message":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("backward compat webhook notify expected 200 got %d %s", w.Code, w.Body.String())
	}
	// feed priority too
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/feed/priority", bytes.NewBufferString(`{"title":"t2","message":"m2"}`))
	req2.Header.Set("Content-Type", "application/json")
	srv.Router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("feed priority unauth expected 200 got %d %s", w2.Code, w2.Body.String())
	}
}
