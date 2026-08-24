package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

// newResetTestServer spins up a full Server backed by an in-memory SQLite DB.
// Unlike package main tests, LEDIT_AUTH_DISABLE is not set here, so
// initAdminSettings creates the admin account (admin/ledit) and enables auth.
func newResetTestServer(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:test.db?cache=shared&_fk=1&_busy_timeout=5000&mode=memory")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	return New(drv, nil)
}

func doRequest(t *testing.T, srv *Server, method, path, form string) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if form != "" {
		body = bytes.NewReader([]byte(form))
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestForgotPasswordPage(t *testing.T) {
	srv := newResetTestServer(t)
	w := doRequest(t, srv, "GET", "/forgot-password", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Forgot Password") {
		t.Error("expected forgot-password page content")
	}
}

func TestForgotPasswordUnknownUserShowsGenericMessage(t *testing.T) {
	srv := newResetTestServer(t)
	w := doRequest(t, srv, "POST", "/forgot-password", "username=nobody")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "If an account exists for that username") {
		t.Errorf("expected generic success message to avoid user enumeration, got: %s", w.Body.String())
	}
}

func TestForgotPasswordNoEmailSettingsShowsError(t *testing.T) {
	srv := newResetTestServer(t)
	// Admin exists (admin/ledit from initAdminSettings) but no EmailSettings
	// row and no recipient → the action must surface a configuration error
	// rather than pretend to send.
	w := doRequest(t, srv, "POST", "/forgot-password", "username=admin")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Could not send a reset link") {
		t.Errorf("expected email config error, got: %s", w.Body.String())
	}
}

func TestResetPasswordInvalidToken(t *testing.T) {
	srv := newResetTestServer(t)
	w := doRequest(t, srv, "GET", "/reset-password?token=bogus", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid token, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid or has expired") {
		t.Errorf("expected invalid-token message, got: %s", w.Body.String())
	}
}

func TestResetPasswordFullFlow(t *testing.T) {
	srv := newResetTestServer(t)

	// Seed a session so we can verify it gets invalidated on reset.
	authMu.Lock()
	sessions["fakesession"] = sessionData{UserID: 0, Username: "admin", Role: "admin", Expiry: time.Now().Add(time.Hour)}
	authMu.Unlock()

	storeResetToken("testtoken")

	// GET with a valid token renders the form.
	w := doRequest(t, srv, "GET", "/reset-password?token=testtoken", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid token, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Choose a New Password") {
		t.Error("expected reset form content")
	}

	// POST applies the new password and redirects to login.
	w = doRequest(t, srv, "POST", "/reset-password", "token=testtoken&new_password=newpass123&confirm_password=newpass123")
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 after reset, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login?reset=1" {
		t.Errorf("expected redirect to /login?reset=1, got %q", loc)
	}

	// Password hash was updated.
	admin, err := srv.DB.AdminSettings.Query().First(srv.Ctx)
	if err != nil {
		t.Fatalf("failed to load admin: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("newpass123")) != nil {
		t.Error("new password should verify")
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("ledit")) == nil {
		t.Error("old password should no longer verify")
	}

	// Sessions were cleared.
	authMu.Lock()
	n := len(sessions)
	authMu.Unlock()
	if n != 0 {
		t.Errorf("expected sessions to be cleared, got %d", n)
	}

	// Token is single-use: a second POST with the same token is rejected.
	w = doRequest(t, srv, "POST", "/reset-password", "token=testtoken&new_password=otherpass&confirm_password=otherpass")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for reused token, got %d", w.Code)
	}

	// The new password actually works at the login form.
	loginBody := bytes.NewReader([]byte("username=admin&password=newpass123"))
	req := httptest.NewRequest("POST", "/login", loginBody)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lw := httptest.NewRecorder()
	srv.ServeHTTP(lw, req)
	if lw.Code != http.StatusFound {
		t.Fatalf("expected 302 login with new password, got %d", lw.Code)
	}
}

func TestResetPasswordMismatch(t *testing.T) {
	srv := newResetTestServer(t)
	storeResetToken("tok2")

	w := doRequest(t, srv, "POST", "/reset-password", "token=tok2&new_password=aaa&confirm_password=bbb")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with error, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "do not match") {
		t.Errorf("expected mismatch error, got: %s", w.Body.String())
	}

	// Token must survive a failed attempt.
	w = doRequest(t, srv, "GET", "/reset-password?token=tok2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("token should still be valid after mismatch, got %d", w.Code)
	}
}

func TestResetPasswordEmptyNewPassword(t *testing.T) {
	srv := newResetTestServer(t)
	storeResetToken("tok3")

	w := doRequest(t, srv, "POST", "/reset-password", "token=tok3&new_password=&confirm_password=")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with error, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "cannot be empty") {
		t.Errorf("expected empty-password error, got: %s", w.Body.String())
	}
}

func TestLoginPageShowsResetSuccess(t *testing.T) {
	srv := newResetTestServer(t)
	w := doRequest(t, srv, "GET", "/login?reset=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Password reset successfully") {
		t.Errorf("expected reset success banner, got: %s", w.Body.String())
	}
}

func TestLoginPageHasForgotPasswordLink(t *testing.T) {
	srv := newResetTestServer(t)
	w := doRequest(t, srv, "GET", "/login", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "/forgot-password") {
		t.Error("expected forgot-password link on login page")
	}
}
