package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func newInboundPushTestServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", name)
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
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New(), WSHub: NewWSHub(client)}
	// clear in-memory notifications between tests
	clearNotifications()
	return srv, client
}

func clearNotifications() {
	priorityMu.Lock()
	notifHistory = nil
	notifID = 0
	priorityMu.Unlock()
}

func seedInboundAdapter(t *testing.T, client *ent.Client, ctx context.Context, kind, secret string) {
	t.Helper()
	_, err := client.InboundAdapter.Create().SetKind(kind).SetEnabled(true).SetSecret(secret).SetAllowlist(`["*"]`).SetConfig("{}").Save(ctx)
	if err != nil {
		t.Fatalf("seed adapter %s: %v", kind, err)
	}
}

func doPush(t *testing.T, srv *Server, kind, secretHeader, secretQuery, body, contentType string, extraHeaders map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/inbound/" + kind
	if secretQuery != "" {
		path += "?secret=" + url.QueryEscape(secretQuery)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if secretHeader != "" {
		req.Header.Set("X-Inbound-Secret", secretHeader)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	// Ensure ClientIP returns something
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	srv.InboundPush(kind)(c)
	return w
}

func TestInboundPushNtfyDelivered(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "ntfy", "s3cr3t")
	body, _ := json.Marshal(map[string]any{"title": "Hello", "message": "world", "priority": 5})
	w := doPush(t, srv, "ntfy", "s3cr3t", "", string(body), "application/json", nil)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	msgs := ActiveMessages()
	found := false
	for _, m := range msgs {
		if m.Body == "world" && m.Title == "Hello" {
			found = true
			if m.Priority != inboundPriorityUrgent {
				t.Errorf("priority want urgent got %d", m.Priority)
			}
		}
	}
	if !found {
		t.Fatalf("notification not delivered ActiveMessages=%v", msgs)
	}
}

func TestInboundPushGotifyDelivered(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "gotify", "gsecret")
	body, _ := json.Marshal(map[string]any{"title": "GTitle", "message": "gbody", "priority": 9})
	w := doPush(t, srv, "gotify", "gsecret", "", string(body), "application/json", nil)
	if w.Code != 200 {
		t.Fatalf("gotify want 200 got %d %s", w.Code, w.Body.String())
	}
	msgs := ActiveMessages()
	found := false
	for _, m := range msgs {
		if m.Body == "gbody" {
			found = true
			if m.Priority != inboundPriorityUrgent {
				t.Errorf("gotify priority want urgent got %d", m.Priority)
			}
		}
	}
	if !found {
		t.Fatalf("gotify not delivered")
	}
}

func TestInboundPushPushoverDelivered(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "pushover", "psecret")
	vals := url.Values{}
	vals.Set("title", "PTitle")
	vals.Set("message", "pbody")
	vals.Set("priority", "1")
	w := doPush(t, srv, "pushover", "psecret", "", vals.Encode(), "application/x-www-form-urlencoded", nil)
	if w.Code != 200 {
		t.Fatalf("pushover want 200 got %d %s", w.Code, w.Body.String())
	}
	msgs := ActiveMessages()
	found := false
	for _, m := range msgs {
		if m.Body == "pbody" {
			found = true
			if m.Priority != inboundPriorityHigh {
				t.Errorf("pushover priority want high got %d", m.Priority)
			}
		}
	}
	if !found {
		t.Fatalf("pushover not delivered")
	}
}

func TestInboundPushBadSecret(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "ntfy", "s3cr3t")
	body, _ := json.Marshal(map[string]any{"message": "hi"})
	w := doPush(t, srv, "ntfy", "wrong", "", string(body), "application/json", nil)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("want no-store got %q", w.Header().Get("Cache-Control"))
	}
}

func TestInboundPushMissingSecret(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "gotify", "s3cr3t")
	body, _ := json.Marshal(map[string]any{"message": "hi"})
	w := doPush(t, srv, "gotify", "", "", string(body), "application/json", nil)
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestInboundPushEmptyBody(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "ntfy", "s3cr3t")
	body, _ := json.Marshal(map[string]any{"title": "t", "message": ""})
	w := doPush(t, srv, "ntfy", "s3cr3t", "", string(body), "application/json", nil)
	if w.Code != 400 {
		t.Fatalf("want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestInboundPushOversizedBody(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "ntfy", "s3cr3t")
	large := strings.Repeat("A", 70*1024)
	body, _ := json.Marshal(map[string]any{"title": "t", "message": large})
	w := doPush(t, srv, "ntfy", "s3cr3t", "", string(body), "application/json", nil)
	if w.Code != 400 {
		t.Fatalf("want 400 for oversized got %d %s", w.Code, w.Body.String())
	}
}

func TestInboundPushNtfyBearerAuth(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "ntfy", "bearersec")
	body, _ := json.Marshal(map[string]any{"message": "bearertest"})
	w := doPush(t, srv, "ntfy", "", "", string(body), "application/json", map[string]string{"Authorization": "Bearer bearersec"})
	if w.Code != 200 {
		t.Fatalf("bearer want 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestInboundPushQuerySecret(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "gotify", "qsecret")
	body, _ := json.Marshal(map[string]any{"message": "qbody"})
	w := doPush(t, srv, "gotify", "", "qsecret", string(body), "application/json", nil)
	if w.Code != 200 {
		t.Fatalf("query secret want 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestInboundPushTruncate(t *testing.T) {
	srv, client := newInboundPushTestServer(t)
	seedInboundAdapter(t, client, srv.Ctx, "ntfy", "s3cr3t")
	longTitle := strings.Repeat("T", 300)
	longBody := strings.Repeat("B", 3000)
	body, _ := json.Marshal(map[string]any{"title": longTitle, "message": longBody})
	w := doPush(t, srv, "ntfy", "s3cr3t", "", string(body), "application/json", nil)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	msgs := ActiveMessages()
	for _, m := range msgs {
		if m.Body == longBody[:2000] {
			if len(m.Title) != 200 {
				t.Errorf("title not truncated len=%d", len(m.Title))
			}
			return
		}
	}
	t.Fatalf("truncated message not found")
}

func TestInboundPushMissingAdapter404(t *testing.T) {
	srv, _ := newInboundPushTestServer(t)
	body, _ := json.Marshal(map[string]any{"message": "hi"})
	w := doPush(t, srv, "ntfy", "s3cr3t", "", string(body), "application/json", nil)
	if w.Code != 404 {
		t.Fatalf("want 404 got %d", w.Code)
	}
}

func TestInboundPushPriorityMapping(t *testing.T) {
	// Verify each mapper covers edge values
	if inboundPriorityFromNtfy(1) != inboundPriorityLow {
		t.Error("ntfy 1 low")
	}
	if inboundPriorityFromNtfy(5) != inboundPriorityUrgent {
		t.Error("ntfy 5 urgent")
	}
	if inboundPriorityFromGotify(0) != inboundPriorityLow {
		t.Error("gotify 0 low")
	}
	if inboundPriorityFromGotify(9) != inboundPriorityUrgent {
		t.Error("gotify 9 urgent")
	}
	if inboundPriorityFromPushover(-2) != inboundPriorityLow {
		t.Error("pushover -2 low")
	}
	if inboundPriorityFromPushover(2) != inboundPriorityUrgent {
		t.Error("pushover 2 urgent")
	}
	_ = bytes.MinRead
}
