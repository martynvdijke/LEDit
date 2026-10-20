package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ledit/ent/websession"
)

func TestSessionTokensAreRandomAndHashedAtRest(t *testing.T) {
	srv := newResetTestServer(t)
	sd := sessionData{UserID: 0, Username: "admin", Role: "admin", Expiry: time.Now().Add(time.Hour)}
	tok1, err := putSession(srv.DB, sd)
	if err != nil {
		t.Fatalf("putSession: %v", err)
	}
	tok2, err := putSession(srv.DB, sd)
	if err != nil {
		t.Fatalf("putSession: %v", err)
	}
	if tok1 == tok2 {
		t.Fatal("session tokens must be unique")
	}
	if len(tok1) < 40 {
		t.Fatalf("expected a long random token, got %q", tok1)
	}
	rows, err := srv.DB.WebSession.Query().All(srv.Ctx)
	if err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 persisted sessions, got %d", len(rows))
	}
	for _, row := range rows {
		if row.TokenHash == tok1 || row.TokenHash == tok2 {
			t.Fatal("raw session token must not be stored")
		}
	}
}

func TestSessionSurvivesCacheLoss(t *testing.T) {
	srv := newResetTestServer(t)
	sd := sessionData{UserID: 0, Username: "admin", Role: "admin", Expiry: time.Now().Add(time.Hour)}
	token, err := putSession(srv.DB, sd)
	if err != nil {
		t.Fatalf("putSession: %v", err)
	}

	// Simulate a restart: drop the in-memory cache but keep the database.
	authMu.Lock()
	sessions = map[string]sessionData{}
	authMu.Unlock()

	got, ok := lookupSession(srv.DB, token)
	if !ok {
		t.Fatal("session should reload from the database after cache loss")
	}
	if got.Username != "admin" || got.Role != "admin" {
		t.Fatalf("unexpected restored session: %+v", got)
	}

	deleteSession(srv.DB, token)
	if _, ok := lookupSession(srv.DB, token); ok {
		t.Fatal("deleted session must not authenticate")
	}
	if n, _ := srv.DB.WebSession.Query().Where(websession.TokenHashEQ(hashSessionToken(token))).Count(srv.Ctx); n != 0 {
		t.Fatalf("expected session row to be deleted, got %d", n)
	}
}

func TestLoginSetsSecureSessionCookie(t *testing.T) {
	srv := newResetTestServer(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader("username=admin&password=ledit"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected login redirect, got %d: %s", w.Code, w.Body.String())
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "session=") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("expected HttpOnly session cookie, got %q", cookie)
	}
	if !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("expected SameSite=Lax, got %q", cookie)
	}
	if strings.Contains(cookie, "Secure") {
		t.Fatalf("plain HTTP test request should not set Secure, got %q", cookie)
	}
}
