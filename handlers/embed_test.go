package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// embedTestServer builds a full server plus a router mounting the embed page,
// session, WebSocket and snapshot routes, mirroring how handlers/server.go
// wires them. It reuses the guest-remote harness so rate buckets and GlobalFeed
// are reset per test.
func embedTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv := newGuestRemoteTestServer(t)
	ctx := context.Background()
	if n, _ := srv.DB.GeneralSettings.Query().Count(ctx); n == 0 {
		srv.DB.GeneralSettings.Create().
			SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(ctx)
	}
	clearFrameRegistry()

	gin.SetMode(gin.TestMode)
	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return srv, ts
}

func embedRequest(t *testing.T, ts *httptest.Server, method, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func seedEmbedDevice(t *testing.T, srv *Server) int {
	t.Helper()
	ctx := context.Background()
	dev := srv.DB.DeviceSettings.Create().
		SetName("Embed Panel").
		SetEnabled(true).
		SetWidth(32).SetHeight(64).
		SetRefreshInterval(1).
		SetToken("embed-hw-token").
		SaveX(ctx)
	return dev.ID
}

// ---------------------------------------------------------------------------
// 1.2 guest scope validation

func TestGuestRemotesEmbedScopeCreate(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/guest-remotes",
		strings.NewReader(`{"label":"obs","scopes":["embed"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	srv.APIGuestRemotesCreate(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("embed scope create = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Scopes []string `json:"scopes"`
		Secret string   `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !hasGuestScope(resp.Scopes, "embed") {
		t.Fatalf("scopes = %v, want embed", resp.Scopes)
	}
	if resp.Secret == "" {
		t.Fatal("expected a one-time secret in the create response")
	}
}

func TestGuestRemotesUnknownScopeStillRejected(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/guest-remotes",
		strings.NewReader(`{"label":"bad","scopes":["embed","bogus"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	srv.APIGuestRemotesCreate(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown scope = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "embed") {
		t.Fatalf("scope error should list embed, got %s", w.Body.String())
	}
}

// A token created before embed existed (pause-only) must not gain embed access.
func TestLegacyGuestTokenLacksEmbedScope(t *testing.T) {
	srv, _ := embedTestServer(t)
	secret := createGuestToken(t, srv, []string{"pause"}, nil, nil)

	tok, ok := srv.lookupGuestToken(context.Background(), secret)
	if !ok {
		t.Fatal("expected the legacy token to resolve")
	}
	if hasGuestScope(tok.Scopes, "embed") {
		t.Fatalf("legacy token unexpectedly has embed scope: %v", tok.Scopes)
	}

	// The embed middleware must reject it with 403 insufficient_scope.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/probe", srv.EmbedAuthMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("X-Guest-Token", secret)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("embed request with pause-only token = %d, want 403", w.Code)
	}
	if !strings.Contains(w.Body.String(), "insufficient_scope") {
		t.Fatalf("expected insufficient_scope, got %s", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 1.5 / 1.6 embed pages and session exchange

func TestEmbedPageAnonymous(t *testing.T) {
	_, ts := embedTestServer(t)
	resp := embedRequest(t, ts, http.MethodGet, "/embed", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /embed = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := resp.Header.Get("Vary"); !strings.Contains(got, "Cookie") || !strings.Contains(got, "Authorization") {
		t.Fatalf("Vary = %q, want Cookie, Authorization", got)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `data-embed-target="shared"`) {
		t.Fatalf("embed page should carry the shared target: %s", body)
	}
	if strings.Contains(body, "/login") {
		t.Fatalf("embed page must never link to /login: %s", body)
	}
}

func TestEmbedDevicePageTarget(t *testing.T) {
	_, ts := embedTestServer(t)
	resp := embedRequest(t, ts, http.MethodGet, "/embed/device/7", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /embed/device/7 = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `data-embed-target="device"`) || !strings.Contains(body, `data-device-id="7"`) {
		t.Fatalf("device embed page missing target data: %s", body)
	}
}

func TestEmbedDevicePageInvalidID(t *testing.T) {
	_, ts := embedTestServer(t)
	resp := embedRequest(t, ts, http.MethodGet, "/embed/device/nope", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid device id = %d, want 404", resp.StatusCode)
	}
}

func TestEmbedSessionExchange(t *testing.T) {
	srv, ts := embedTestServer(t)
	secret := createGuestToken(t, srv, []string{"embed"}, nil, nil)

	resp := embedRequest(t, ts, http.MethodPost, "/embed/session",
		fmt.Sprintf(`{"token":%q}`, secret), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid session exchange = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("session Cache-Control = %q, want no-store", got)
	}
	var cookie *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == embedCookieName {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatalf("expected %s cookie, got %v", embedCookieName, resp.Cookies())
	}
	if !cookie.HttpOnly {
		t.Fatal("embed cookie must be HttpOnly")
	}
	if cookie.Value != secret {
		t.Fatal("embed cookie should carry the exchanged secret")
	}
}

func TestEmbedSessionRejections(t *testing.T) {
	srv, ts := embedTestServer(t)
	revokedAt := time.Now()
	expiredAt := time.Now().Add(-time.Hour)
	cases := []struct {
		name   string
		token  string
		status int
	}{
		{"missing", "", http.StatusBadRequest},
		{"unknown", "not-a-real-secret", http.StatusUnauthorized},
		{"revoked", createGuestToken(t, srv, []string{"embed"}, nil, &revokedAt), http.StatusUnauthorized},
		{"expired", createGuestToken(t, srv, []string{"embed"}, &expiredAt, nil), http.StatusUnauthorized},
		{"missing scope", createGuestToken(t, srv, []string{"pause"}, nil, nil), http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := ""
			if tc.token != "" {
				body = fmt.Sprintf(`{"token":%q}`, tc.token)
			}
			resp := embedRequest(t, ts, http.MethodPost, "/embed/session", body, nil)
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.status, readBody(t, resp))
			}
			for _, ck := range resp.Cookies() {
				if ck.Name == embedCookieName {
					t.Fatal("rejected exchange must not set the embed cookie")
				}
			}
		})
	}
}

// The secret must be posted in the body, never accepted from the query string.
func TestEmbedSessionRejectsSecretInQuery(t *testing.T) {
	srv, ts := embedTestServer(t)
	secret := createGuestToken(t, srv, []string{"embed"}, nil, nil)
	resp := embedRequest(t, ts, http.MethodPost, "/embed/session?token="+secret, "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("query-string secret = %d, want 400", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 2.5 embed WebSocket behaviour

func TestEmbedSharedWSStreamsAtRequestedSize(t *testing.T) {
	_, ts := embedTestServer(t)
	conn := dialWS(t, ts, "/ws/embed?w=256&h=128")
	msg := readFrame(t, conn)
	if _, ok := msg["image"]; !ok {
		t.Fatalf("expected an image frame, got %v", msg)
	}
	if _, ok := lookupFrame("shared", 256, 128); !ok {
		t.Fatal("expected the emitted frame to be buffered at the requested size")
	}
}

func TestEmbedSharedWSClampsSize(t *testing.T) {
	_, ts := embedTestServer(t)
	// clampPreviewSize maps below 8 to 64 and above 1024 to 1024.
	conn := dialWS(t, ts, "/ws/embed?w=2&h=4096")
	readFrame(t, conn)
	if _, ok := lookupFrame("shared", 64, 1024); !ok {
		t.Fatal("expected the frame to be buffered at the clamped size 64x1024")
	}
}

func TestEmbedDeviceWSRequiresToken(t *testing.T) {
	srv, ts := embedTestServer(t)
	devID := seedEmbedDevice(t, srv)
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/embed/device/%d", devID)
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		conn.Close()
		t.Fatal("device embed upgrade without a token must fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 handshake, got %+v", resp)
	}
}

func TestEmbedDeviceWSWithTokenIsReadOnly(t *testing.T) {
	srv, ts := embedTestServer(t)
	devID := seedEmbedDevice(t, srv)
	secret := createGuestToken(t, srv, []string{"embed"}, nil, nil)

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/embed/device/%d", devID)
	conn, _, err := websocket.DefaultDialer.Dial(u, http.Header{"X-Guest-Token": []string{secret}})
	if err != nil {
		t.Fatalf("dial with embed token: %v", err)
	}
	msg := readFrame(t, conn)
	if _, ok := msg["image"]; !ok {
		t.Fatalf("expected an image frame, got %v", msg)
	}
	if _, ok := lookupFrame(fmt.Sprintf("device:%d", devID), 32, 64); !ok {
		t.Fatal("expected a frame buffered at the device resolution")
	}
	conn.Close()
	time.Sleep(200 * time.Millisecond)

	fresh, err := srv.DB.DeviceSettings.Get(context.Background(), devID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if fresh.LastSeenAt != nil {
		t.Fatalf("embed must NOT mark last_seen_at, got %v", *fresh.LastSeenAt)
	}
	if fresh.FramesServed != 0 {
		t.Fatalf("embed must NOT increment frames_served, got %d", fresh.FramesServed)
	}
}

func TestEmbedDeviceWSScopeRequired(t *testing.T) {
	srv, ts := embedTestServer(t)
	devID := seedEmbedDevice(t, srv)
	secret := createGuestToken(t, srv, []string{"photo"}, nil, nil)

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/embed/device/%d", devID)
	conn, resp, err := websocket.DefaultDialer.Dial(u, http.Header{"X-Guest-Token": []string{secret}})
	if err == nil {
		conn.Close()
		t.Fatal("token without the embed scope must not upgrade")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 handshake, got %+v", resp)
	}
}

func TestEmbedUpgradeRateLimited(t *testing.T) {
	_, ts := embedTestServer(t)
	rateMu.Lock()
	bucket := make([]time.Time, embedUpgradeIPLimit)
	for i := range bucket {
		bucket[i] = time.Now()
	}
	rateBuckets[embedUpgradeKeyIP+"127.0.0.1"] = bucket
	rateMu.Unlock()

	resp := embedRequest(t, ts, http.MethodGet, "/ws/embed", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-limit upgrade = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header on 429")
	}
}

// ---------------------------------------------------------------------------
// 4.4 snapshot endpoint

func TestAPIFrameSharedAnonymous(t *testing.T) {
	_, ts := embedTestServer(t)
	resp := embedRequest(t, ts, http.MethodGet, "/api/frame", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/frame = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "image/png") {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := resp.Header.Get("Vary"); !strings.Contains(got, "Cookie") {
		t.Fatalf("Vary = %q, want Cookie, Authorization", got)
	}
	if resp.Header.Get("X-LEDit-Source") == "" {
		t.Fatal("expected X-LEDit-Source on the fallback render")
	}
}

func TestAPIFrameBufferedByteIdentical(t *testing.T) {
	_, ts := embedTestServer(t)
	payload := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	storeFrame("shared", 400, 400, &lastFrame{
		Data: payload, Format: "PNG", Source: "Unit Test",
	})

	resp := embedRequest(t, ts, http.MethodGet, "/api/frame", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-LEDit-Source"); got != "Unit Test" {
		t.Fatalf("X-LEDit-Source = %q, want Unit Test", got)
	}
	if resp.Header.Get("X-LEDit-Frame-Age") == "" {
		t.Fatal("expected X-LEDit-Frame-Age for a buffered frame")
	}
	got := []byte(readBody(t, resp))
	if string(got) != string(payload) {
		t.Fatalf("buffered frame not byte-identical: %v vs %v", got, payload)
	}
}

func TestAPIFrameFormatWebP(t *testing.T) {
	_, ts := embedTestServer(t)
	// No buffered frame: the fallback renders a real PNG which webp can encode.
	resp := embedRequest(t, ts, http.MethodGet, "/api/frame?format=webp", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("format=webp status = %d, want 200 (%s)", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "image/webp") {
		t.Fatalf("Content-Type = %q, want image/webp", got)
	}
}

func TestAPIFrameVideoCurrentIs415(t *testing.T) {
	_, ts := embedTestServer(t)
	storeFrame("shared", 400, 400, &lastFrame{Data: []byte("junk"), Format: "MP4", Video: true, Source: "Video"})
	resp := embedRequest(t, ts, http.MethodGet, "/api/frame", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("video current = %d, want 415", resp.StatusCode)
	}
	if !strings.Contains(readBody(t, resp), "video") {
		t.Fatal("expected a video error body")
	}
}

func TestAPIFrameDeviceRequiresAuth(t *testing.T) {
	srv, ts := embedTestServer(t)
	devID := seedEmbedDevice(t, srv)
	resp := embedRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/frame?device=%d", devID), "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous device frame = %d, want 401", resp.StatusCode)
	}
}

func TestAPIFrameDeviceWithEmbedToken(t *testing.T) {
	srv, ts := embedTestServer(t)
	devID := seedEmbedDevice(t, srv)
	secret := createGuestToken(t, srv, []string{"embed"}, nil, nil)
	resp := embedRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/frame?device=%d", devID), "",
		map[string]string{"X-Guest-Token": secret})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device frame with token = %d, want 200 (%s)", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "image/png") {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
}

func TestAPIFrameRateLimited(t *testing.T) {
	_, ts := embedTestServer(t)
	rateMu.Lock()
	bucket := make([]time.Time, embedFrameIPLimit)
	for i := range bucket {
		bucket[i] = time.Now()
	}
	rateBuckets["embed:frame:ip:127.0.0.1"] = bucket
	rateMu.Unlock()

	resp := embedRequest(t, ts, http.MethodGet, "/api/frame", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-limit frame = %d, want 429", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("429 Cache-Control = %q, want no-store", got)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
