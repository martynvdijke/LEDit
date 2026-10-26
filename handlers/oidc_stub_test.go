package handlers

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ledit/ent/user"
)

// stubIdP is an in-process OpenID Provider good enough to drive the real
// Authorization Code + PKCE flow: discovery document, JWKS and a token
// endpoint that mints an RS256 ID token. It replaces Authelia for tests, so
// the whole callback path (state/nonce/PKCE checks, JWKS verification, claim
// extraction, provisioning, session creation) runs for real.
type stubIdP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string

	// Token endpoint behaviour; mutated between steps by the test.
	claims      map[string]any
	rejectToken bool
	lastForm    url.Values
}

func newStubIdP(t *testing.T) *stubIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &stubIdP{key: key, kid: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := idp.base()
		writeJSON(w, map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"scopes_supported":                      []string{"openid", "email", "profile", "groups"},
			"grant_types_supported":                 []string{"authorization_code"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": idp.kid,
				"n":   base64.RawURLEncoding.EncodeToString(idp.key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(idp.key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.lastForm = r.PostForm
		if idp.rejectToken {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": "invalid_grant"})
			return
		}
		token, err := idp.idToken()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
			"expires_in":   300,
			"id_token":     token,
		})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	t.Cleanup(func() {
		oidcMu.Lock()
		oidcProvider, oidcVerifier, oidcIssuer = nil, nil, ""
		oidcMu.Unlock()
	})
	return idp
}

func (s *stubIdP) base() string { return s.server.URL }

// idToken signs the configured claims with RS256.
func (s *stubIdP) idToken() (string, error) {
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": s.kid}
	claims := map[string]any{
		"iss": s.base(),
		"aud": "ledit",
		"iat": time.Now().Add(-time.Minute).Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range s.claims {
		claims[k] = v
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// oidcSDKServer starts a server pointed at the stub IdP.
func oidcSDKServer(t *testing.T, idp *stubIdP) *Server {
	t.Helper()
	EnableAuth()
	t.Cleanup(func() {
		authMu.Lock()
		sessions = map[string]sessionData{}
		authMu.Unlock()
	})
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", idp.base())
	t.Setenv("OIDC_CLIENT_ID", "ledit")
	t.Setenv("OIDC_CLIENT_SECRET", "test-secret")
	t.Setenv("OIDC_REDIRECT_URL", "http://ledit.example.test/api/auth/oidc/callback")
	t.Setenv("OIDC_SCOPES", "openid email profile groups")
	return newAPITokenTestServer(t)
}

// oidcLoginRedirect performs the login leg and returns the authorize URL plus
// the state/nonce/verifier the server stored in cookies.
func oidcLoginRedirect(t *testing.T, srv *Server) (loc *url.URL, state, nonce, verifier string) {
	t.Helper()
	w := doRequest(t, srv, http.MethodGet, "/api/auth/oidc/login", "")
	if w.Code != http.StatusFound {
		t.Fatalf("oidc login: expected 302, got %d (%s)", w.Code, w.Body.String())
	}
	parsed, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("bad redirect location: %v", err)
	}
	cookies := map[string]string{}
	for _, c := range w.Result().Cookies() {
		cookies[c.Name] = c.Value
	}
	return parsed, cookies["oidc_state"], cookies["oidc_nonce"], cookies["oidc_verifier"]
}

func TestOIDCLoginRedirectsWithPKCEAgainstDiscovery(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	loc, state, nonce, verifier := oidcLoginRedirect(t, srv)

	if loc.Scheme+"://"+loc.Host != idp.base() {
		t.Fatalf("authorize endpoint should come from discovery, got %q", loc.String())
	}
	q := loc.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type: got %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "ledit" {
		t.Errorf("client_id: got %q", q.Get("client_id"))
	}
	if q.Get("state") != state || q.Get("nonce") != nonce {
		t.Errorf("state/nonce must match the stored cookies")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("PKCE method: got %q", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") != pkceChallenge(verifier) {
		t.Errorf("code_challenge is not S256(verifier)")
	}
	if !strings.Contains(q.Get("scope"), "groups") {
		t.Errorf("scopes should include groups, got %q", q.Get("scope"))
	}
	if q.Get("redirect_uri") != "http://ledit.example.test/api/auth/oidc/callback" {
		t.Errorf("redirect_uri: got %q", q.Get("redirect_uri"))
	}
}

func TestOIDCCallbackHappyPathProvisionsAndSessions(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	_, state, nonce, verifier := oidcLoginRedirect(t, srv)
	idp.claims = map[string]any{
		"sub":                "authelia-sub-happy",
		"email":              "Martyn@Example.test",
		"email_verified":     true,
		"preferred_username": "martyn",
		"name":               "Martyn van Dijke",
		"groups":             []string{"users", "admins"},
		"nonce":              nonce,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state="+state+"&code=stub-code", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: nonce})
	req.AddCookie(&http.Cookie{Name: "oidc_verifier", Value: verifier})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/" {
		t.Fatalf("callback should redirect to the app, got %d %q (%s)", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	// The PKCE verifier and the code must have been exchanged, not skipped.
	if got := idp.lastForm.Get("code_verifier"); got != verifier {
		t.Errorf("code_verifier: got %q want %q", got, verifier)
	}
	if got := idp.lastForm.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type: got %q", got)
	}
	if idp.lastForm.Get("client_secret") != "test-secret" {
		t.Errorf("client_secret_post should be used, got %q", idp.lastForm.Get("client_secret"))
	}

	provisioned, err := srv.DB.User.Query().Where(user.OidcSubEQ("authelia-sub-happy")).Only(srv.Ctx)
	if err != nil {
		t.Fatalf("user not provisioned: %v", err)
	}
	if provisioned.Email == nil || *provisioned.Email != "martyn@example.test" {
		t.Errorf("email should be stored lowercased, got %v", provisioned.Email)
	}
	if provisioned.Role != user.RoleAdmin {
		t.Errorf("`admins` group should grant admin, got %q", provisioned.Role)
	}
	if provisioned.AuthMethod != user.AuthMethodOidc {
		t.Errorf("auth_method: got %q", provisioned.AuthMethod)
	}

	// The issued cookie must work for authenticated API calls immediately.
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie issued by the callback")
	}
	apiReq := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	apiReq.AddCookie(session)
	apiRec := httptest.NewRecorder()
	srv.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusOK {
		t.Fatalf("OIDC session should authenticate API reads, got %d", apiRec.Code)
	}
}

func TestOIDCCallbackRejectsUnverifiedEmail(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	_, state, nonce, verifier := oidcLoginRedirect(t, srv)
	idp.claims = map[string]any{
		"sub":            "authelia-sub-unverified",
		"email":          "nobody@example.test",
		"email_verified": false,
		"nonce":          nonce,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state="+state+"&code=stub-code", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: nonce})
	req.AddCookie(&http.Cookie{Name: "oidc_verifier", Value: verifier})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Verified email required") {
		t.Fatalf("unverified email should be rejected, got %s", rec.Body.String())
	}
	if n := userCountByEmail(t, srv, "nobody@example.test"); n != 0 {
		t.Fatalf("no user may be provisioned for an unverified email, found %d", n)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.Value != "" {
			t.Fatal("no session may be issued for an unverified email")
		}
	}
}

func TestOIDCCallbackRejectsWrongNonce(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	_, state, nonce, verifier := oidcLoginRedirect(t, srv)
	idp.claims = map[string]any{
		"sub":            "authelia-sub-nonce",
		"email":          "nonce@example.test",
		"email_verified": true,
		"nonce":          "a-different-nonce",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state="+state+"&code=stub-code", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: nonce})
	req.AddCookie(&http.Cookie{Name: "oidc_verifier", Value: verifier})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Invalid OIDC nonce") {
		t.Fatalf("nonce mismatch should be rejected, got %s", rec.Body.String())
	}
	if n := userCountByEmail(t, srv, "nonce@example.test"); n != 0 {
		t.Fatalf("no user may be provisioned on a nonce mismatch, found %d", n)
	}
}

func TestOIDCCallbackRejectsBadTokenExchange(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	_, state, nonce, verifier := oidcLoginRedirect(t, srv)
	idp.rejectToken = true

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state="+state+"&code=stub-code", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: nonce})
	req.AddCookie(&http.Cookie{Name: "oidc_verifier", Value: verifier})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "OIDC code exchange failed") {
		t.Fatalf("failed exchange should be reported, got %s", rec.Body.String())
	}
	if n := userCountByEmail(t, srv, "sync@example.test"); n != 0 {
		t.Fatalf("a failed exchange must not provision users, found %d", n)
	}
}

func TestOIDCCallbackReloginSyncsRoleFromGroups(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	completeLogin := func(groups []string) {
		t.Helper()
		_, state, nonce, verifier := oidcLoginRedirect(t, srv)
		idp.claims = map[string]any{
			"sub":                "authelia-sub-sync",
			"email":              "sync@example.test",
			"email_verified":     true,
			"preferred_username": "sync",
			"groups":             groups,
			"nonce":              nonce,
		}
		req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state="+state+"&code=stub-code", nil)
		req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
		req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: nonce})
		req.AddCookie(&http.Cookie{Name: "oidc_verifier", Value: verifier})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("callback failed: %d %s", rec.Code, rec.Body.String())
		}
	}

	completeLogin([]string{"admins"})
	u := srv.DB.User.Query().Where(user.OidcSubEQ("authelia-sub-sync")).OnlyX(srv.Ctx)
	if u.Role != user.RoleAdmin {
		t.Fatalf("first login should grant admin, got %q", u.Role)
	}

	completeLogin([]string{"users"})
	u = srv.DB.User.Query().Where(user.OidcSubEQ("authelia-sub-sync")).OnlyX(srv.Ctx)
	if u.Role != user.RoleViewer {
		t.Fatalf("role should drop to viewer, got %q", u.Role)
	}
	if n := userCountByEmail(t, srv, "sync@example.test"); n != 1 {
		t.Fatalf("re-login must update, not duplicate, the user (count=%d)", n)
	}
}

func TestOIDCLogoutClearsSessionAndRedirects(t *testing.T) {
	idp := newStubIdP(t)
	srv := oidcSDKServer(t, idp)

	u, err := srv.linkOIDCUser("sub-logout", "logout@example.test", "admin", "logoutuser", "Logout")
	if err != nil {
		t.Fatalf("linkOIDCUser: %v", err)
	}
	cookie := sessionCookieFrom(t, srv, u.ID, u.Username, string(u.Role))

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("logout should redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, idp.base()+"/logout") {
		t.Errorf("logout should redirect to the IdP, got %q", loc)
	}
	if !strings.Contains(loc, "post_logout_redirect_uri=") {
		t.Errorf("logout should pass a post-logout redirect, got %q", loc)
	}

	// Session gone.
	req = httptest.NewRequest(http.MethodGet, "/api/feed/current", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session should be cleared after logout, got %d", rec.Code)
	}
}

// userCountByEmail counts users with the given email. The test server seeds a
// legacy admin user, so absolute table counts are not meaningful.
func userCountByEmail(t *testing.T, srv *Server, email string) int {
	t.Helper()
	n, err := srv.DB.User.Query().Where(user.EmailEQ(email)).Count(srv.Ctx)
	if err != nil {
		t.Fatalf("count users by email: %v", err)
	}
	return n
}
