package handlers

import (
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
)

func newGuestRemoteTestServer(t *testing.T) *Server {
	t.Helper()
	// Reset shared limiter and feed state so tests do not leak into each other.
	guestRateMu.Lock()
	guestRate = map[string][]time.Time{}
	guestRateMu.Unlock()
	GlobalFeed = &FeedController{}
	dsn := fmt.Sprintf("file:guestremote_%d.db?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", time.Now().UnixNano())
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	return New(drv, nil)
}

func createGuestToken(t *testing.T, srv *Server, scopes []string, expiresAt, revokedAt *time.Time) string {
	t.Helper()
	secret, prefix := generateGuestToken()
	b := srv.DB.GuestToken.Create().
		SetTokenHash(hashGuestToken(secret)).
		SetTokenPrefix(prefix).
		SetScopes(scopes).
		SetCreatedAt(time.Now())
	if expiresAt != nil {
		b.SetExpiresAt(*expiresAt)
	}
	if revokedAt != nil {
		b.SetRevokedAt(*revokedAt)
	}
	b.SaveX(srv.Ctx)
	return secret
}

// guestTestRouter mounts a single scope-protected probe route.
func guestTestRouter(srv *Server, scope string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/g/action", srv.GuestAuthMiddleware(scope), func(c *gin.Context) {
		if currentGuestToken(c) == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no token in context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func guestReq(r *gin.Engine, secret, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/g/action", nil)
	if secret != "" {
		req.Header.Set("X-Guest-Token", secret)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// 2.4 Middleware acceptance/rejection matrix
// ---------------------------------------------------------------------------

func TestGuestAuthValidTokenAccepted(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"next"}, nil, nil)
	if w := guestReq(guestTestRouter(srv, "next"), secret, ""); w.Code != http.StatusOK {
		t.Fatalf("valid token: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestGuestAuthBearerGuestTokenAccepted(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"next"}, nil, nil)
	if w := guestReq(guestTestRouter(srv, "next"), "", secret); w.Code != http.StatusOK {
		t.Fatalf("bearer guest token: expected 200, got %d", w.Code)
	}
}

func TestGuestAuthUnknownToken401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	if w := guestReq(guestTestRouter(srv, "next"), "not-a-real-token", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthRevoked401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	now := time.Now()
	secret := createGuestToken(t, srv, []string{"next"}, nil, &now)
	if w := guestReq(guestTestRouter(srv, "next"), secret, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthExpired401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	past := time.Now().Add(-time.Hour)
	secret := createGuestToken(t, srv, []string{"next"}, &past, nil)
	if w := guestReq(guestTestRouter(srv, "next"), secret, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthMissingScope403(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"pause", "next"}, nil, nil)
	w := guestReq(guestTestRouter(srv, "message"), secret, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("out-of-scope: expected 403, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "insufficient_scope") {
		t.Fatalf("expected insufficient_scope code, got %s", w.Body.String())
	}
}

func TestGuestAuthMissingHeader401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	if w := guestReq(guestTestRouter(srv, "next"), "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing header: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthSessionCookieAlone401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/g/action", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	guestTestRouter(srv, "next").ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("admin session cookie alone: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthAPITokenBearer401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	apiSecret := createTestToken(t, srv, owner.ID, nil, nil)
	if w := guestReq(guestTestRouter(srv, "next"), "", apiSecret); w.Code != http.StatusUnauthorized {
		t.Fatalf("admin API token bearer: expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// 2.3 Limiter window rollover and per-key isolation
// ---------------------------------------------------------------------------

func TestGuestRateLimitWindowRollover(t *testing.T) {
	key := "test:rollover:" + t.Name()
	for i := 0; i < 3; i++ {
		if ok, _ := checkGuestRateLimit(key, 3); !ok {
			t.Fatalf("request %d within limit should be allowed", i+1)
		}
	}
	if ok, retry := checkGuestRateLimit(key, 3); ok || retry < 1 {
		t.Fatalf("4th request should be throttled with positive retry, got ok=%v retry=%d", ok, retry)
	}
	// Age the recorded hits out of the one-minute window.
	guestRateMu.Lock()
	old := time.Now().Add(-2 * time.Minute)
	for i := range guestRate[key] {
		guestRate[key][i] = old
	}
	guestRateMu.Unlock()
	if ok, _ := checkGuestRateLimit(key, 3); !ok {
		t.Fatal("after window rollover a new request should be allowed")
	}
}

func TestGuestRateLimitPerKeyIsolation(t *testing.T) {
	a := "test:isolation:a:" + t.Name()
	b := "test:isolation:b:" + t.Name()
	for i := 0; i < 2; i++ {
		checkGuestRateLimit(a, 2)
	}
	if ok, _ := checkGuestRateLimit(a, 2); ok {
		t.Fatal("key a should be throttled")
	}
	if ok, _ := checkGuestRateLimit(b, 2); !ok {
		t.Fatal("key b should not be affected by key a's usage")
	}
}

// ---------------------------------------------------------------------------
// 3.5 Guest endpoint behavior
// ---------------------------------------------------------------------------

func guestAPI(srv *Server, method, path, secret, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if secret != "" {
		req.Header.Set("X-Guest-Token", secret)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestGuestStatusMinimalJSON(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"pause", "message"}, nil, nil)
	w := guestAPI(srv, http.MethodGet, "/api/guest/status", secret, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("status Cache-Control: expected no-store, got %q", got)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(payload) != 3 {
		t.Fatalf("status must expose exactly 3 keys, got %v", payload)
	}
	for _, key := range []string{"paused", "scopes", "expires_at"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("status missing %q: %v", key, payload)
		}
	}
	if _, leaked := payload["current"]; leaked {
		t.Fatal("status leaked current source")
	}
}

func TestGuestControlsMutateFeed(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"pause", "next"}, nil, nil)

	if w := guestAPI(srv, http.MethodPost, "/api/guest/pause", secret, ""); w.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d", w.Code)
	}
	if !GlobalFeed.IsPaused() {
		t.Fatal("pause did not mutate the feed")
	}
	// resume is gated by the same pause scope.
	if w := guestAPI(srv, http.MethodPost, "/api/guest/resume", secret, ""); w.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d", w.Code)
	}
	if GlobalFeed.IsPaused() {
		t.Fatal("resume did not mutate the feed")
	}
	if w := guestAPI(srv, http.MethodPost, "/api/guest/next", secret, ""); w.Code != http.StatusOK {
		t.Fatalf("next: expected 200, got %d", w.Code)
	}
	if !GlobalFeed.ShouldSkip() {
		t.Fatal("next did not set the skip signal")
	}
}

func TestGuestMessageAccepted(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"message"}, nil, nil)
	w := guestAPI(srv, http.MethodPost, "/api/guest/message", secret, `{"text":"Dinner is ready"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("message: expected 202, got %d (%s)", w.Code, w.Body.String())
	}
	var payload struct {
		ID  int `json:"id"`
		TTL int `json:"ttl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload.ID <= 0 || payload.TTL < 1 || payload.TTL > 3600 {
		t.Fatalf("unexpected message response: %+v", payload)
	}
	history := srv.GetNotificationHistory()
	found := false
	for _, n := range history {
		if n.Title == "Guest message" && n.Message == "Dinner is ready" {
			found = true
		}
	}
	if !found {
		t.Fatalf("notification not created: %+v", history)
	}
}

func TestGuestMessageRejectsOverlong(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"message"}, nil, nil)
	body, _ := json.Marshal(map[string]string{"text": strings.Repeat("a", 141)})
	w := guestAPI(srv, http.MethodPost, "/api/guest/message", secret, string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("141 chars: expected 400, got %d", w.Code)
	}
}

func TestGuestMessageRejectsWhitespace(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"message"}, nil, nil)
	w := guestAPI(srv, http.MethodPost, "/api/guest/message", secret, `{"text":"   "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("whitespace: expected 400, got %d", w.Code)
	}
}

func TestGuestMessageRateLimited(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"message"}, nil, nil)
	for i := 0; i < 5; i++ {
		if w := guestAPI(srv, http.MethodPost, "/api/guest/message", secret, `{"text":"hi"}`); w.Code != http.StatusAccepted {
			t.Fatalf("message %d: expected 202, got %d", i+1, w.Code)
		}
	}
	w := guestAPI(srv, http.MethodPost, "/api/guest/message", secret, `{"text":"hi"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th message: expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
}
