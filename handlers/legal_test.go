package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ledit/ent/apitoken"
	"ledit/ent/user"
	"ledit/ent/websession"
)

func TestLegalPagesArePublic(t *testing.T) {
	srv := newTestServerForRoles(t)
	cases := []struct {
		path string
		want string
	}{
		{"/privacy", "Privacy"},
		{"/terms", "Terms of use"},
		{"/account/delete", "Delete your account"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tc.path, w.Code)
		}
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: body does not contain %q", tc.path, tc.want)
		}
	}
}

func TestLegalPagesUseConfiguredIdentity(t *testing.T) {
	srv := newTestServerForRoles(t)
	t.Setenv("LEGAL_CONTROLLER_NAME", "Example Labs BV")
	t.Setenv("LEGAL_CONTACT_EMAIL", "privacy@example.com")
	t.Setenv("LEGAL_HOSTING_REGION", "the Netherlands")
	req := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{"Example Labs BV", "privacy@example.com", "the Netherlands"} {
		if !strings.Contains(body, want) {
			t.Fatalf("privacy page does not contain %q", want)
		}
	}
}

func createAccountToken(t *testing.T, srv *Server, ownerID int, role string) string {
	t.Helper()
	secret := "acct-" + role + "-" + time.Now().Format("150405.000000000")
	h := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(h[:])
	b := srv.DB.ApiToken.Create().
		SetName("account test").
		SetTokenHash(hash).
		SetTokenPrefix(secret[:8]).
		SetOwnerID(ownerID).
		SetCreatedAt(time.Now())
	if role == "viewer" {
		b.SetRole(apitoken.RoleViewer)
	} else {
		b.SetRole(apitoken.RoleAdmin)
	}
	if _, err := b.Save(srv.Ctx); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return secret
}

func TestAccountExportReturnsProfileAndTokenMetadataOnly(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "exporter", "password123", "viewer")
	u, err := srv.DB.User.Query().Where(user.UsernameEQ("exporter")).Only(srv.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	secret := createAccountToken(t, srv, u.ID, "viewer")
	cookie := loginAndGetCookie(t, srv, "exporter", "password123")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/export", nil)
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	profile, _ := body["profile"].(map[string]any)
	if profile["username"] != "exporter" || profile["role"] != "viewer" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	tokens, _ := body["api_tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("expected one token, got %d", len(tokens))
	}
	tok, _ := tokens[0].(map[string]any)
	if tok["prefix"] != secret[:8] {
		t.Fatalf("unexpected token prefix: %#v", tok)
	}
	raw := w.Body.String()
	if strings.Contains(raw, secret) || strings.Contains(raw, "token_hash") {
		t.Fatal("export leaked a secret or hash")
	}
}

func TestAccountDeleteRequiresTypedConfirmation(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "alice", "password123", "viewer")
	createUserDirect(t, srv, "root", "password123", "admin")
	cookie := loginAndGetCookie(t, srv, "alice", "password123")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me", strings.NewReader(`{"confirm":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong confirmation, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := srv.DB.User.Query().Where(user.UsernameEQ("alice")).Only(srv.Ctx); err != nil {
		t.Fatal("account should still exist after failed confirmation")
	}
}

func TestAccountDeleteRevokesSessionsAndTokens(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "alice", "password123", "viewer")
	createUserDirect(t, srv, "root", "password123", "admin")
	u, err := srv.DB.User.Query().Where(user.UsernameEQ("alice")).Only(srv.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	secret := createAccountToken(t, srv, u.ID, "viewer")

	// A second browser session proves the deletion clears every session row,
	// not just the one used to call the endpoint.
	other, err := putSession(srv.DB, sessionData{
		UserID: u.ID, Username: u.Username, Role: "viewer",
		Expiry: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie := loginAndGetCookie(t, srv, "alice", "password123")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me", strings.NewReader(`{"confirm":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := srv.DB.User.Get(srv.Ctx, u.ID); err == nil {
		t.Fatal("user row should be deleted")
	}
	if n, _ := srv.DB.WebSession.Query().Where(websession.UserID(u.ID)).Count(srv.Ctx); n != 0 {
		t.Fatalf("expected no sessions, got %d", n)
	}
	if _, ok := lookupSession(srv.DB, other); ok {
		t.Fatal("second session must no longer authenticate")
	}
	tok, err := srv.DB.ApiToken.Query().Where(apitoken.TokenHashEQ(hashAPIToken(secret))).Only(srv.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevokedAt == nil {
		t.Fatal("owned API token should be revoked")
	}

	// The deleted account's session cannot export anything.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/me/export", nil)
	req2.Header.Set("Cookie", cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("export after deletion: expected 401, got %d", w2.Code)
	}
}

func TestLastAdminCannotDeleteAccount(t *testing.T) {
	srv := newTestServerForRoles(t)
	// New() may seed a legacy admin from AdminSettings; this test needs exactly
	// one account so the last-admin guard is the only possible refusal.
	if _, err := srv.DB.User.Delete().Exec(srv.Ctx); err != nil {
		t.Fatal(err)
	}
	createUserDirect(t, srv, "onlyadmin", "password123", "admin")
	cookie := loginAndGetCookie(t, srv, "onlyadmin", "password123")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me", strings.NewReader(`{"confirm":"onlyadmin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for last admin, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "last_admin") {
		t.Fatalf("expected last_admin code, got %s", w.Body.String())
	}
	if _, err := srv.DB.User.Query().Where(user.UsernameEQ("onlyadmin")).Only(srv.Ctx); err != nil {
		t.Fatal("last admin must remain")
	}
}

func TestSettingsAccountDeleteUsesSession(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "alice", "password123", "viewer")
	createUserDirect(t, srv, "root", "password123", "admin")
	cookie := loginAndGetCookie(t, srv, "alice", "password123")

	body := bytes.NewBufferString("confirm=alice")
	req := httptest.NewRequest(http.MethodPost, "/settings/account/delete", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after deletion, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := srv.DB.User.Query().Where(user.UsernameEQ("alice")).Only(srv.Ctx); err == nil {
		t.Fatal("account should be deleted")
	}
}
