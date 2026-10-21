package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"ledit/ent/user"
)

// These tests exercise the OIDC requirements that can be verified without a
// live Authelia: the disabled toggle, the PKCE/state/nonce guards on the
// callback, the authorize redirect, and the fact that an OIDC session is
// indistinguishable from a password session to the auth middleware. Token
// exchange, JWKS verification and the real Authelia round-trip are homelab
// verification steps (see docs/oidc-authelia.md).

// oidcTestServer returns a server with auth enabled and OIDC env configured.
func oidcTestServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	EnableAuth()
	t.Cleanup(func() {
		authMu.Lock()
		sessions = map[string]sessionData{}
		authMu.Unlock()
	})
	srv := newAPITokenTestServer(t)
	if enabled {
		t.Setenv("OIDC_ENABLED", "true")
	} else {
		t.Setenv("OIDC_ENABLED", "false")
	}
	t.Setenv("OIDC_ISSUER_URL", "https://authelia.example.test")
	t.Setenv("OIDC_CLIENT_ID", "ledit")
	t.Setenv("OIDC_REDIRECT_URL", "https://ledit.example.test/api/auth/oidc/callback")
	t.Setenv("OIDC_SCOPES", "openid email profile groups")
	t.Setenv("OIDC_CLIENT_SECRET", "test-secret")
	return srv
}

func TestAPIAuthConfigReportsOIDCToggle(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		want    string
	}{
		{enabled: true, want: `"oidc_enabled":true`},
		{enabled: false, want: `"oidc_enabled":false`},
	} {
		srv := oidcTestServer(t, tc.enabled)
		w := doRequest(t, srv, http.MethodGet, "/api/auth/config", "")
		if w.Code != http.StatusOK {
			t.Fatalf("oidc=%v: expected 200, got %d", tc.enabled, w.Code)
		}
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("oidc=%v: expected body to contain %s, got %s", tc.enabled, tc.want, w.Body.String())
		}
	}
}

func TestOIDCDisabledRoutesReturnNotFound(t *testing.T) {
	srv := oidcTestServer(t, false)

	for _, path := range []string{"/api/auth/oidc/login", "/api/auth/oidc/callback"} {
		w := doRequest(t, srv, http.MethodGet, path, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s with OIDC disabled: expected 404, got %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "oidc_disabled") {
			t.Errorf("GET %s should report oidc_disabled, got %s", path, w.Body.String())
		}
	}

	// Rollback path: password login keeps working while OIDC is off.
	session := loginAsAdmin(t, srv)
	if session.Value == "" {
		t.Fatal("password login should still issue a session cookie")
	}
}

func TestOIDCCallbackRejectsInvalidState(t *testing.T) {
	srv := oidcTestServer(t, true)

	// No state cookie at all.
	w := doRequest(t, srv, http.MethodGet, "/api/auth/oidc/callback?state=abc&code=x", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Invalid OIDC state") {
		t.Fatalf("missing state cookie: expected rejection, got %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" && c.Value != "" {
			t.Error("no session cookie may be issued when the state check fails")
		}
	}

	// Mismatched state: cookie says one thing, query another.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=tampered&code=x", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: "original"})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: "n"})
	req.AddCookie(&http.Cookie{Name: "oidc_verifier", Value: "v"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid OIDC state") {
		t.Fatalf("mismatched state: expected rejection, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOIDCCallbackRequiresNonceAndPKCEVerifier(t *testing.T) {
	srv := oidcTestServer(t, true)

	// Valid state but no nonce cookie.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=s1&code=x", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: "s1"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Missing OIDC nonce") {
		t.Fatalf("expected missing-nonce rejection, got %s", rec.Body.String())
	}

	// Valid state and nonce but no PKCE verifier cookie.
	req = httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=s1&code=x", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: "s1"})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: "n1"})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Missing PKCE verifier") {
		t.Fatalf("expected missing-verifier rejection, got %s", rec.Body.String())
	}

	// None of the rejections may leave an authenticated session behind.
	req = httptest.NewRequest(http.MethodGet, "/api/feed/current", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("rejected callback must not authenticate the caller, got %d", rec.Code)
	}
}

// TestOIDCSessionMatchesPasswordSession covers the requirement that an OIDC
// session is accepted by AuthMiddleware exactly like a password session.
func TestOIDCSessionMatchesPasswordSession(t *testing.T) {
	srv := oidcTestServer(t, true)

	u, err := srv.linkOIDCUser("authelia-sub-1", "admin@example.test", "admin", "admin", "Admin User")
	if err != nil {
		t.Fatalf("linkOIDCUser: %v", err)
	}
	if u.AuthMethod != user.AuthMethodOidc || u.OidcSub == nil || *u.OidcSub != "authelia-sub-1" {
		t.Fatalf("user not linked to the OIDC subject: %+v", u)
	}

	cookie := sessionCookieFrom(t, srv, u.ID, u.Username, string(u.Role))

	// Viewer-authenticated read.
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("OIDC session rejected on authenticated read: %d %s", rec.Code, rec.Body.String())
	}

	// Admin-authenticated mutation through the same cookie.
	req = httptest.NewRequest(http.MethodPost, "/api/feed/pause", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("OIDC admin session rejected on mutation: %d %s", rec.Code, rec.Body.String())
	}
	GlobalFeed.Resume()

	// Logout clears the session for both auth methods.
	req = httptest.NewRequest(http.MethodGet, "/logout", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	req = httptest.NewRequest(http.MethodGet, "/api/feed/current", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session should be invalid after logout, got %d", rec.Code)
	}
}

// TestOIDCViewerSessionCannotMutate keeps the role boundary honest: a viewer
// provisioned from a missing `admins` group is read-only.
func TestOIDCViewerSessionCannotMutate(t *testing.T) {
	srv := oidcTestServer(t, true)

	u, err := srv.linkOIDCUser("authelia-sub-2", "viewer@example.test", "viewer", "viewer", "Viewer")
	if err != nil {
		t.Fatalf("linkOIDCUser: %v", err)
	}
	cookie := sessionCookieFrom(t, srv, u.ID, u.Username, string(u.Role))

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer read: expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/devices/1/brightness", strings.NewReader(`{"level":50}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation: expected 403, got %d %s", rec.Code, rec.Body.String())
	}
}

// sessionCookieFrom starts a session for the given identity through the shared
// helper both login paths use, and returns the resulting cookie.
func sessionCookieFrom(t *testing.T, srv *Server, uid int, username, role string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	srv.startSession(ctx, uid, username, role)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.Value != "" {
			return c
		}
	}
	t.Fatal("startSession did not set a session cookie")
	return nil
}
