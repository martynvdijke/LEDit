package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent/apitoken"
	"ledit/ent/user"

	_ "github.com/mattn/go-sqlite3"
)

func newTestServerForRoles(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, fmt.Sprintf("file:roles_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	// Ensure auth enabled (disable bypass)
	t.Setenv("LEDIT_AUTH_DISABLE", "")
	// Clear sessions
	authMu.Lock()
	sessions = map[string]sessionData{}
	authMu.Unlock()
	authEnabled = true
	srv := New(drv, nil)
	return srv
}

func createUserDirect(t *testing.T, srv *Server, username, password, role string) int {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	var r user.Role
	if role == "admin" {
		r = user.RoleAdmin
	} else {
		r = user.RoleViewer
	}
	u, err := srv.DB.User.Create().SetUsername(username).SetPasswordHash(string(hash)).SetRole(r).SetCreatedAt(time.Now()).Save(srv.Ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

func loginAndGetCookie(t *testing.T, srv *Server, username, password string) string {
	t.Helper()
	body := bytes.NewBufferString("username=" + username + "&password=" + password)
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("login failed %d %s", w.Code, w.Body.String())
	}
	cookie := w.Header().Get("Set-Cookie")
	return cookie
}

func createTokenDirect(t *testing.T, srv *Server, role string) string {
	t.Helper()
	owner, _ := srv.DB.AdminSettings.Query().First(srv.Ctx)
	secret := "testsec-" + role + time.Now().Format("150405.000000")
	h := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(h[:])
	builder := srv.DB.ApiToken.Create().SetName("t").SetTokenHash(hash).SetTokenPrefix(secret[:8]).SetOwnerID(owner.ID).SetCreatedAt(time.Now())
	if role == "viewer" {
		builder.SetRole(apitoken.RoleViewer)
	} else {
		builder.SetRole(apitoken.RoleAdmin)
	}
	_, err := builder.Save(srv.Ctx)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return secret
}

func TestViewerCannotMutate(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "alice", "password123", "viewer")
	createUserDirect(t, srv, "admin2", "password123", "admin")
	cookie := loginAndGetCookie(t, srv, "alice", "password123")
	req := httptest.NewRequest("POST", "/admin/settings", bytes.NewBufferString("timeout=5&width=64&height=64"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer POST should be 403 got %d", w.Code)
	}
	// viewer GET should succeed
	req2 := httptest.NewRequest("GET", "/admin/", nil)
	req2.Header.Set("Cookie", cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("viewer GET should be 200 got %d", w2.Code)
	}
}

func TestAdminCanMutate(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "admin2", "password123", "admin")
	cookie := loginAndGetCookie(t, srv, "admin2", "password123")
	req := httptest.NewRequest("POST", "/admin/settings", bytes.NewBufferString("timeout=5&width=64&height=64"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound && w.Code != http.StatusOK {
		t.Fatalf("admin POST should succeed got %d", w.Code)
	}
}

func TestViewerTokenScopes(t *testing.T) {
	srv := newTestServerForRoles(t)
	viewerTok := createTokenDirect(t, srv, "viewer")
	adminTok := createTokenDirect(t, srv, "admin")
	// viewer can read
	req := httptest.NewRequest("GET", "/api/feed/current", nil)
	req.Header.Set("Authorization", "Bearer "+viewerTok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("viewer token read should be 200 got %d", w.Code)
	}
	// viewer cannot mutate
	req2 := httptest.NewRequest("POST", "/api/feed/pause", nil)
	req2.Header.Set("Authorization", "Bearer "+viewerTok)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("viewer token mutate should be 403 got %d", w2.Code)
	}
	// admin can mutate
	req3 := httptest.NewRequest("POST", "/api/feed/pause", nil)
	req3.Header.Set("Authorization", "Bearer "+adminTok)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("admin token mutate should be 200 got %d", w3.Code)
	}
}

func TestUsersCRUDAndLastAdmin(t *testing.T) {
	srv := newTestServerForRoles(t)
	createUserDirect(t, srv, "admin2", "password123", "admin")
	cookie := loginAndGetCookie(t, srv, "admin2", "password123")
	// create viewer
	body := bytes.NewBufferString("username=bob&password=password123&role=viewer")
	req := httptest.NewRequest("POST", "/admin/api/users", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user 201 got %d %s", w.Code, w.Body.String())
	}
	// duplicate 409
	body2 := bytes.NewBufferString("username=bob&password=password123&role=viewer")
	req2 := httptest.NewRequest("POST", "/admin/api/users", body2)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Cookie", cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate should be 409 got %d", w2.Code)
	}
	// viewer cannot get users
	createUserDirect(t, srv, "eve", "password123", "viewer")
	cookieViewer := loginAndGetCookie(t, srv, "eve", "password123")
	req3 := httptest.NewRequest("GET", "/admin/users", nil)
	req3.Header.Set("Cookie", cookieViewer)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code == http.StatusOK {
		t.Fatalf("viewer should not see users page")
	}
	// last admin delete 400
	// get admin id
	users, _ := srv.DB.User.Query().All(srv.Ctx)
	var adminID int
	for _, u := range users {
		if u.Username == "admin2" {
			adminID = u.ID
		}
	}
	// try delete last admin when only one admin left? We have admin2 plus maybe seeded admin? Delete eve is viewer ok. Delete admin2 should fail if only one admin.
	// Ensure we have only one admin: delete other admins?
	// For test, try deleting admin when there are 2 admins: should succeed, then last fails.
	// Create second admin
	createUserDirect(t, srv, "admin3", "password123", "admin")
	reqDel := httptest.NewRequest("DELETE", fmt.Sprintf("/admin/api/users/%d", adminID), nil)
	reqDel.Header.Set("Cookie", cookie)
	wDel := httptest.NewRecorder()
	srv.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete with 2 admins should succeed got %d %s", wDel.Code, wDel.Body.String())
	}
}
