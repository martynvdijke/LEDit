package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
)

func newPlaylistAuthTestServer(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	srv := New(drv, nil)
	// Ensure GeneralSettings exists so anonymous GET is not redirected to /setup
	// for reasons unrelated to auth; NeedsSetup still triggers on default password,
	// but SetupMiddleware allows authenticated sessions through and otherwise
	// redirects GET /admin/* to /setup — accept either /setup or /login as valid
	// anonymous redirect target (mirrors existing api_token lifecycle tests which
	// only assert 302).
	if _, err := srv.DB.GeneralSettings.Query().First(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	return srv
}

func TestPlaylistAuth_Anonymous(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   url.Values
	}{
		{"list GET", http.MethodGet, "/admin/playlists", nil},
		{"new GET", http.MethodGet, "/admin/playlists/new", nil},
		{"create POST", http.MethodPost, "/admin/playlists/new", url.Values{"name": {"Test"}, "items": {`[{"source_type":"systemstats","source_id":0}]`}}},
		{"edit GET", http.MethodGet, "/admin/playlists/1/edit", nil},
		{"update POST", http.MethodPost, "/admin/playlists/1/edit", url.Values{"name": {"Test"}, "items": {`[{"source_type":"systemstats","source_id":0}]`}}},
		{"delete POST", http.MethodPost, "/admin/playlists/1/delete", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newPlaylistAuthTestServer(t)
			var body string
			if tc.body != nil {
				body = tc.body.Encode()
			}
			w := doRequest(t, srv, tc.method, tc.path, body)
			if w.Code != http.StatusFound {
				t.Fatalf("%s %s anonymous: expected 302 redirect to login, got %d body %s", tc.method, tc.path, w.Code, w.Body.String())
			}
			loc := w.Header().Get("Location")
			if !strings.Contains(loc, "/login") && !strings.Contains(loc, "/setup") {
				t.Fatalf("expected redirect to /login or /setup, got %q (code %d)", loc, w.Code)
			}
		})
	}
}

func TestPlaylistAuth_AnonymousCreateNotPersisted(t *testing.T) {
	srv := newPlaylistAuthTestServer(t)
	form := url.Values{}
	form.Set("name", "Test")
	form.Set("items", `[{"source_type":"systemstats","source_id":0}]`)
	w := doRequest(t, srv, http.MethodPost, "/admin/playlists/new", form.Encode())
	if w.Code != http.StatusFound {
		// Anonymous should be redirected, not create
	}
	count, _ := srv.DB.Playlist.Query().Count(context.Background())
	if count != 0 {
		t.Fatalf("expected 0 playlists after anonymous POST, got %d", count)
	}
}

func TestPlaylistAuth_AuthenticatedList(t *testing.T) {
	srv := newPlaylistAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/admin/playlists", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated GET /admin/playlists: expected 200, got %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Playlists") {
		t.Fatalf("expected page heading 'Playlists', got body %q", body[:min(500, len(body))])
	}
}

func TestPlaylistAuth_CreateAndDelete(t *testing.T) {
	srv := newPlaylistAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	// Create valid playlist via authenticated POST.
	form := url.Values{}
	form.Set("name", "Test")
	form.Set("items", `[{"source_type":"systemstats","source_id":0}]`)
	req := httptest.NewRequest(http.MethodPost, "/admin/playlists/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("create: expected 302 redirect, got %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/admin/playlists" {
		t.Fatalf("create: expected redirect to /admin/playlists, got %q", loc)
	}

	// Verify row persisted with parsed items.
	count, _ := srv.DB.Playlist.Query().Count(ctx)
	if count != 1 {
		t.Fatalf("expected 1 playlist after create, got %d", count)
	}
	pl := srv.DB.Playlist.Query().FirstX(ctx)
	if pl.Name != "Test" {
		t.Fatalf("expected name Test, got %q", pl.Name)
	}
	items, err := datasource.ParsePlaylistItems(pl.Items)
	if err != nil {
		t.Fatalf("stored items failed to parse: %v (raw %q)", err, pl.Items)
	}
	if len(items) != 1 || items[0].SourceType != "systemstats" || items[0].SourceID != 0 {
		t.Fatalf("unexpected stored items %+v", items)
	}

	// Delete via authenticated POST.
	delReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/playlists/%d/delete", pl.ID), nil)
	delReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	delReq.AddCookie(session)
	dw := httptest.NewRecorder()
	srv.ServeHTTP(dw, delReq)
	if dw.Code != http.StatusFound {
		t.Fatalf("delete: expected 302, got %d body %s", dw.Code, dw.Body.String())
	}
	count, _ = srv.DB.Playlist.Query().Count(ctx)
	if count != 0 {
		t.Fatalf("expected 0 playlists after delete, got %d", count)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
