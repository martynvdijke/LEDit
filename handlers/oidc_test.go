package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOIDCDisabled(t *testing.T) {
	srv := newTestServerForRoles(t)
	t.Setenv("OIDC_ENABLED", "")

	for _, path := range []string{"/api/auth/oidc/login", "/api/auth/oidc/callback"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d, want 404", path, w.Code)
		}
	}

	// Logout always clears local session, even with OIDC off.
	req := httptest.NewRequest("GET", "/api/auth/oidc/logout", nil)
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("logout: got %d, want 302", w.Code)
	}
}

func TestOIDCUsername(t *testing.T) {
	if got := oidcUsername("jane.doe@example.com", "", ""); got != "jane.doe" {
		t.Fatalf("email local part: got %q", got)
	}
	if got := oidcUsername("a@b.c", "", ""); got == "" || len(got) < 3 {
		t.Fatalf("short email fallback: got %q", got)
	}
	if got := oidcUsername("user@example.com", "Jane Doe!", ""); got != "Jane-Doe" {
		t.Fatalf("preferred sanitize: got %q", got)
	}
}
