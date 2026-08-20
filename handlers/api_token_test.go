package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

// newAPITokenTestServer builds a full Server against an in-memory SQLite DB.
func newAPITokenTestServer(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:apitoken_test.db?cache=shared&_fk=1&mode=memory")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	return New(drv, nil)
}

// createTestToken inserts a token row directly and returns its raw secret.
// opts may set expiresAt/revokedAt to exercise expiry and revocation.
func createTestToken(t *testing.T, srv *Server, ownerID int, expiresAt, revokedAt *time.Time) string {
	t.Helper()
	secret, prefix := generateAPIToken()
	b := srv.DB.ApiToken.Create().
		SetName("test-token").
		SetTokenHash(hashAPIToken(secret)).
		SetTokenPrefix(prefix).
		SetOwnerID(ownerID).
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

// loginAsAdmin authenticates a session cookie as the default admin user.
func loginAsAdmin(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	w := doRequest(t, srv, http.MethodPost, "/login", "username=admin&password=ledit")
	if w.Code != http.StatusFound {
		t.Fatalf("login failed: %d (body %s)", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("no session cookie set after login")
	return nil
}

// authedRequest performs a request with a bearer token and returns the recorder.
func bearerRequest(srv *Server, method, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"title":"t","message":"m"}`))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// 3.1 Public reads and write authorization
// ---------------------------------------------------------------------------

func TestAPIPublicReadsUnauthenticated(t *testing.T) {
	srv := newAPITokenTestServer(t)

	public := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/trmnl/stats"},
		{http.MethodGet, "/api/health"},
	}
	for _, tc := range public {
		w := doRequest(t, srv, tc.method, tc.path, "")
		if w.Code != http.StatusOK {
			t.Errorf("%s %s: expected 200, got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestAPIFeedCurrentRequiresAuth(t *testing.T) {
	srv := newAPITokenTestServer(t)
	// Anonymous → 401
	w := doRequest(t, srv, http.MethodGet, "/api/feed/current", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous feed/current: expected 401, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("anonymous feed/current should set WWW-Authenticate")
	}
	// Session → 200
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/api/feed/current", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session feed/current: expected 200, got %d", rec.Code)
	}
	// Bearer → 200
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	secret := createTestToken(t, srv, owner.ID, nil, nil)
	w2 := bearerRequest(srv, http.MethodGet, "/api/feed/current", secret)
	if w2.Code != http.StatusOK {
		t.Fatalf("bearer feed/current: expected 200, got %d", w2.Code)
	}
	// No-store on success
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("feed/current should set Cache-Control: no-store")
	}
}

func TestAPINotificationsRequiresAuth(t *testing.T) {
	srv := newAPITokenTestServer(t)
	w := doRequest(t, srv, http.MethodGet, "/api/notifications", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous notifications: expected 401, got %d", w.Code)
	}
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session notifications: expected 200, got %d", rec.Code)
	}
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	secret := createTestToken(t, srv, owner.ID, nil, nil)
	w2 := bearerRequest(srv, http.MethodGet, "/api/notifications", secret)
	if w2.Code != http.StatusOK {
		t.Fatalf("bearer notifications: expected 200, got %d", w2.Code)
	}
}

func TestAPIAnonymousWriteRejected(t *testing.T) {
	srv := newAPITokenTestServer(t)

	writes := []string{
		"/api/feed/next",
		"/api/feed/pause",
		"/api/feed/resume",
		"/api/feed/priority",
		"/api/webhook/notify",
	}
	for _, path := range writes {
		w := doRequest(t, srv, http.MethodPost, path, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without token: expected 401, got %d", path, w.Code)
		}
	}
}

func TestAPITokenOnlyWriteSucceeds(t *testing.T) {
	srv := newAPITokenTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	secret := createTestToken(t, srv, owner.ID, nil, nil)

	w := bearerRequest(srv, http.MethodPost, "/api/feed/pause", secret)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/feed/pause with valid token: expected 200, got %d (body %s)", w.Code, w.Body.String())
	}
	if !GlobalFeed.IsPaused() {
		t.Error("expected feed to be paused after authorized call")
	}
	GlobalFeed.Resume()
}

func TestAPIMalformedTokenRejected(t *testing.T) {
	srv := newAPITokenTestServer(t)

	cases := []string{
		"not-a-bearer-token", // wrong scheme
		"Bearer ",            // empty secret
		"Bearer garbage",     // unknown secret
	}
	for _, auth := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/feed/pause", strings.NewReader(""))
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: expected 401, got %d", auth, w.Code)
		}
	}
}

func TestAPIExpiredTokenRejected(t *testing.T) {
	srv := newAPITokenTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	past := time.Now().Add(-time.Hour)
	secret := createTestToken(t, srv, owner.ID, &past, nil)

	w := bearerRequest(srv, http.MethodPost, "/api/feed/pause", secret)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: expected 401, got %d", w.Code)
	}
}

func TestAPIRevokedTokenRejected(t *testing.T) {
	srv := newAPITokenTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	now := time.Now()
	secret := createTestToken(t, srv, owner.ID, nil, &now)

	w := bearerRequest(srv, http.MethodPost, "/api/feed/pause", secret)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// 3.2 Ownership and secret non-disclosure
// ---------------------------------------------------------------------------

func TestTokenLifecycleRequiresSession(t *testing.T) {
	srv := newAPITokenTestServer(t)

	// Anonymous access to lifecycle endpoints must be redirected to login.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/admin/api-tokens"},
		{http.MethodPost, "/admin/api-tokens"},
		{http.MethodPost, "/admin/api-tokens/1/revoke"},
		{http.MethodPost, "/admin/api-tokens/1/rotate"},
	} {
		w := doRequest(t, srv, tc.method, tc.path, "")
		if w.Code != http.StatusFound {
			t.Errorf("%s %s anonymous: expected 302 redirect, got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestTokenCreateBindsOwnerAndShowsSecretOnce(t *testing.T) {
	srv := newAPITokenTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	session := loginAsAdmin(t, srv)

	// Create a token via the lifecycle endpoint.
	req := httptest.NewRequest(http.MethodPost, "/admin/api-tokens", strings.NewReader("name=ci-client"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create token: expected 201, got %d (body %s)", w.Code, w.Body.String())
	}

	var created struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("bad create response: %v", err)
	}
	if created.Secret == "" {
		t.Fatal("create response must include the one-time secret")
	}

	// The stored row must hold the hash, never the secret, and bind the owner.
	row := srv.DB.ApiToken.GetX(srv.Ctx, created.ID)
	if row.TokenHash == created.Secret {
		t.Fatal("stored token_hash must not equal the raw secret")
	}
	if row.TokenHash != hashAPIToken(created.Secret) {
		t.Fatal("stored token_hash must equal the SHA-256 of the secret")
	}
	if row.OwnerID != owner.ID {
		t.Fatalf("token owner_id = %d, want %d", row.OwnerID, owner.ID)
	}

	// The secret must authenticate API mutations.
	w2 := bearerRequest(srv, http.MethodPost, "/api/feed/pause", created.Secret)
	if w2.Code != http.StatusOK {
		t.Fatalf("created token should authorize writes, got %d", w2.Code)
	}
	GlobalFeed.Resume()
}

func TestTokenListNeverDisclosesSecret(t *testing.T) {
	srv := newAPITokenTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	secret := createTestToken(t, srv, owner.ID, nil, nil)

	// The JSON view used by the list page must never include the secret.
	row := srv.DB.ApiToken.Query().FirstX(srv.Ctx)
	view := toAPITokenView(row)
	if view.Prefix == secret {
		t.Fatal("view prefix must not equal the full secret")
	}
	if !strings.HasPrefix(secret, view.Prefix) {
		t.Fatalf("view prefix %q should be a prefix of the secret", view.Prefix)
	}
}

func TestTokenRevokeAndRotateNeverDiscloseSecret(t *testing.T) {
	srv := newAPITokenTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	secret := createTestToken(t, srv, owner.ID, nil, nil)
	session := loginAsAdmin(t, srv)

	row := srv.DB.ApiToken.Query().FirstX(srv.Ctx)

	// Revoke: response must not contain the secret.
	req := httptest.NewRequest(http.MethodPost, "/admin/api-tokens/"+strconv.Itoa(row.ID)+"/revoke", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("revoke response leaked the secret")
	}

	// The revoked token must no longer authenticate.
	w2 := bearerRequest(srv, http.MethodPost, "/api/feed/pause", secret)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token should be rejected, got %d", w2.Code)
	}

	// Rotate: response contains a NEW secret, never the old one.
	req = httptest.NewRequest(http.MethodPost, "/admin/api-tokens/"+strconv.Itoa(row.ID)+"/rotate", nil)
	req.AddCookie(session)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("rotate: expected 201, got %d (body %s)", w.Code, w.Body.String())
	}
	var rotated struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("bad rotate response: %v", err)
	}
	if rotated.Secret == "" {
		t.Fatal("rotate response must include the new one-time secret")
	}
	if rotated.Secret == secret {
		t.Fatal("rotate must issue a new secret, not reuse the old one")
	}
}

// ---------------------------------------------------------------------------
// 3.3 Public landing gating (GET /)
// ---------------------------------------------------------------------------

func completeSetupForTest(t *testing.T, srv *Server) {
	t.Helper()
	if _, err := srv.DB.GeneralSettings.Query().First(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	admin, err := srv.DB.AdminSettings.Query().First(srv.Ctx)
	if err == nil {
		srv.DB.AdminSettings.UpdateOne(admin).SetPasswordHash("not-default-hash-for-test").SaveX(srv.Ctx)
	}
}

func TestIndexPublicLanding(t *testing.T) {
	srv := newAPITokenTestServer(t)
	completeSetupForTest(t, srv)
	w := doRequest(t, srv, http.MethodGet, "/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("anonymous GET /: expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Public information") {
		t.Error("anonymous landing should contain Public information block")
	}
	if !strings.Contains(body, "Login to view feed") {
		t.Error("anonymous landing should contain login CTA")
	}
	if strings.Contains(body, `id="source-label"`) {
		t.Error("anonymous landing must not expose source-label telemetry")
	}
	if strings.Contains(body, `id="media-display"`) {
		t.Error("anonymous landing must not expose media-display")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("anonymous landing should set Cache-Control: no-store")
	}
	if !strings.Contains(w.Header().Get("Vary"), "Cookie") {
		t.Error("anonymous landing should set Vary: Cookie, Authorization")
	}
}

func TestIndexAuthenticatedShowsFeed(t *testing.T) {
	srv := newAPITokenTestServer(t)
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated GET /: expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="source-label"`) {
		t.Error("authenticated landing should contain source-label telemetry")
	}
	if !strings.Contains(body, `id="media-display"`) {
		t.Error("authenticated landing should contain media-display")
	}
	if strings.Contains(body, "Public information") {
		t.Error("authenticated landing should not show public-only block")
	}
}
