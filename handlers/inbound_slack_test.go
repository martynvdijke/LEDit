package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func newSlackTestServer(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", name)
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	srv := New(drv, nil)
	// reset global feed for isolation
	GlobalFeed = &FeedController{}
	// seed slack adapter
	_, err = srv.DB.InboundAdapter.Create().SetKind("slack").SetEnabled(true).SetSecret("s3cret").SetAllowlist(`["C1"]`).SetConfig("{}").Save(srv.Ctx)
	if err != nil {
		t.Fatalf("seed slack adapter: %v", err)
	}
	return srv
}

func slackSign(secret, ts, body string) string {
	base := "v0:" + ts + ":" + body
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(base))
	return "v0=" + hex.EncodeToString(m.Sum(nil))
}

func doSlackJSON(t *testing.T, srv *Server, body string, ts string, sig string, ct string) *httptest.ResponseRecorder {
	t.Helper()
	if ct == "" {
		ct = "application/json"
	}
	req := httptest.NewRequest(http.MethodPost, "/api/inbound/slack", strings.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if ts != "" {
		req.Header.Set("X-Slack-Request-Timestamp", ts)
	}
	if sig != "" {
		req.Header.Set("X-Slack-Signature", sig)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	srv.InboundSlack(c)
	return w
}

func TestSlackValidSignedMessageDelivered(t *testing.T) {
	srv := newSlackTestServer(t)
	body := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":"hello slack"}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := slackSign("s3cret", ts, body)
	w := doSlackJSON(t, srv, body, ts, sig, "")
	if w.Code != 200 {
		t.Fatalf("want 200 got %d body %s", w.Code, w.Body.String())
	}
	hist := srv.GetNotificationHistory()
	found := false
	for _, n := range hist {
		if n.Message == "hello slack" {
			found = true
		}
	}
	if !found {
		t.Fatalf("message not delivered, history %v", hist)
	}
}

func TestSlackStaleTimestamp(t *testing.T) {
	srv := newSlackTestServer(t)
	body := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":"old"}}`
	ts := strconv.FormatInt(time.Now().Add(-400*time.Second).Unix(), 10)
	sig := slackSign("s3cret", ts, body)
	w := doSlackJSON(t, srv, body, ts, sig, "")
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestSlackBadSignature(t *testing.T) {
	srv := newSlackTestServer(t)
	body := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":"hi"}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	w := doSlackJSON(t, srv, body, ts, "v0=badbadbad", "")
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}

func TestSlackURLVerification(t *testing.T) {
	srv := newSlackTestServer(t)
	body := `{"type":"url_verification","challenge":"abc123"}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := slackSign("s3cret", ts, body)
	w := doSlackJSON(t, srv, body, ts, sig, "")
	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json %v body %s", err, w.Body.String())
	}
	if resp["challenge"] != "abc123" {
		t.Fatalf("want challenge abc123 got %v", resp)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("want json content type got %q", ct)
	}
	// should deliver nothing
	hist := srv.GetNotificationHistory()
	for _, n := range hist {
		if n.Message == "abc123" {
			t.Fatalf("challenge should not be delivered")
		}
	}
}

func TestSlackNonAllowlistedChannelIgnored(t *testing.T) {
	srv := newSlackTestServer(t)
	body := `{"type":"event_callback","event":{"type":"message","channel":"C9","user":"U9","text":"blocked"}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := slackSign("s3cret", ts, body)
	w := doSlackJSON(t, srv, body, ts, sig, "")
	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	hist := srv.GetNotificationHistory()
	for _, n := range hist {
		if n.Message == "blocked" {
			t.Fatalf("blocked message should not be delivered")
		}
	}
}

func TestSlackSlashNext(t *testing.T) {
	srv := newSlackTestServer(t)
	// Ensure GlobalFeed not already skipping
	GlobalFeed = &FeedController{}
	vals := url.Values{}
	vals.Set("command", "/next")
	vals.Set("channel_id", "C1")
	vals.Set("user_id", "U1")
	vals.Set("text", "")
	body := vals.Encode()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := slackSign("s3cret", ts, body)
	req := httptest.NewRequest(http.MethodPost, "/api/inbound/slack", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	srv.InboundSlack(c)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json %v", err)
	}
	if resp["response_type"] != "ephemeral" {
		t.Fatalf("want ephemeral got %v", resp)
	}
	if !GlobalFeed.ShouldSkip() {
		t.Fatalf("GlobalFeed.Next not invoked")
	}
}

func TestSlackMissingSignatureHeaders(t *testing.T) {
	srv := newSlackTestServer(t)
	body := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":"hi"}}`
	// no headers
	w := doSlackJSON(t, srv, body, "", "", "")
	if w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
}
