package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// --- signing helpers ---

func signBody(secret, tsStr string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(tsStr))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookSigningServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	srv, client := webhookTestServer(t)
	srv.Router.POST("/api/incident", srv.WebhookAuthMiddleware(), srv.APIIncidentIngest)
	return srv, client
}

func TestWebhookSigning_NoSecretBackwardCompat(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret("").SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("no signing secret with api key expected 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_NoKeyNoSecretNoop(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("").SetDefaultTTL(30).SetSigningSecret("").SetSigningWindowSeconds(300).SaveX(context.Background())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(`{"title":"x","message":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("no key no secret no-op expected 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_ValidSignatureBodyPreserved(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	secret := "s3cr3t"
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret(secret).SetSigningWindowSeconds(300).SaveX(context.Background())
	// handler that echoes body
	srv.Router.POST("/api/echo", srv.WebhookAuthMiddleware(), func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"body": string(b)})
	})
	body := `{"title":"hello","message":"world"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signBody(secret, ts, []byte(body))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	req.Header.Set("X-LEDit-Timestamp", ts)
	req.Header.Set("X-LEDit-Signature", sig)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid signed expected 200 got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json %v", err)
	}
	if resp["body"] != body {
		t.Fatalf("body not preserved got %q want %q", resp["body"], body)
	}
}

func TestWebhookSigning_MissingSignature401(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret("s3cr3t").SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	req.Header.Set("X-LEDit-Timestamp", ts)
	// no signature header
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature expected 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_MalformedSignature401(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret("s3cr3t").SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for _, bad := range []string{"sha256=zzzz", "sha256=", "notsha256=abc", "sha256=abc"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "k123")
		req.Header.Set("X-LEDit-Timestamp", ts)
		req.Header.Set("X-LEDit-Signature", bad)
		srv.Router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("malformed %q expected 401 got %d %s", bad, w.Code, w.Body.String())
		}
	}
}

func TestWebhookSigning_WrongSignature401(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret("s3cr3t").SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signBody("wrong", ts, []byte(body))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	req.Header.Set("X-LEDit-Timestamp", ts)
	req.Header.Set("X-LEDit-Signature", sig)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong signature expected 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_StaleTimestamp401(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	secret := "s3cr3t"
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret(secret).SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	ts := strconv.FormatInt(time.Now().Unix()-600, 10)
	sig := signBody(secret, ts, []byte(body))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	req.Header.Set("X-LEDit-Timestamp", ts)
	req.Header.Set("X-LEDit-Signature", sig)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("stale timestamp expected 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_FutureTimestamp401(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	secret := "s3cr3t"
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret(secret).SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	ts := strconv.FormatInt(time.Now().Unix()+600, 10)
	sig := signBody(secret, ts, []byte(body))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	req.Header.Set("X-LEDit-Timestamp", ts)
	req.Header.Set("X-LEDit-Signature", sig)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("future timestamp expected 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_MissingAPIKeyEvenIfSigned401(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	secret := "s3cr3t"
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret(secret).SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"a","message":"b"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signBody(secret, ts, []byte(body))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/notify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	// no X-API-Key
	req.Header.Set("X-LEDit-Timestamp", ts)
	req.Header.Set("X-LEDit-Signature", sig)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing api key even if signed expected 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestWebhookSigning_UniformEnforcement_IncidentAndDisplay(t *testing.T) {
	srv, client := webhookSigningServer(t)
	clearNotifHistory()
	// Ingesting a valid incident writes the package-global incidentCache; clear it
	// so later websocket tests don't see a phantom active incident.
	t.Cleanup(func() { setIncidentCache(nil) })
	secret := "s3cr3t"
	client.WebhookSettings.Create().SetAPIKey("k123").SetDefaultTTL(30).SetSigningSecret(secret).SetSigningWindowSeconds(300).SaveX(context.Background())
	body := `{"title":"t","message":"m"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signBody(secret, ts, []byte(body))
	// incident without sig -> 401
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/incident", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "k123")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("incident without sig expected 401 got %d %s", w.Code, w.Body.String())
	}
	// display without sig -> 401
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/display?text=hello", nil)
	req2.Header.Set("X-API-Key", "k123")
	srv.Router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("display without sig expected 401 got %d %s", w2.Code, w2.Body.String())
	}
	// incident with valid sig -> 200
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/incident", bytes.NewBufferString(body))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("X-API-Key", "k123")
	req3.Header.Set("X-LEDit-Timestamp", ts)
	req3.Header.Set("X-LEDit-Signature", signBody(secret, ts, []byte(body)))
	srv.Router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("incident with valid sig expected 200 got %d %s", w3.Code, w3.Body.String())
	}
	_ = sig
}
