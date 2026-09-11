package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent"
	"ledit/ent/enttest"
)

func nowPlayingTestServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("ledit"), bcrypt.DefaultCost)
	if _, err := client.AdminSettings.Create().SetUsername("admin").SetPasswordHash(string(h)).Save(ctx); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	EnableAuth()
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New()}
	srv.Router.POST("/login", srv.LoginAction)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte("username=admin&password=ledit")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(w, req)
	var sess *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			sess = c
			break
		}
	}
	if sess == nil {
		t.Fatalf("no session: %d %s", w.Code, w.Body.String())
	}
	admin := srv.Router.Group("/admin")
	admin.Use(AuthMiddleware())
	{
		admin.GET("/api/nowplaying", srv.APINowPlayingList)
		admin.GET("/api/nowplaying/:id", srv.APINowPlayingGet)
		admin.POST("/api/nowplaying", srv.APINowPlayingCreate)
		admin.PUT("/api/nowplaying/:id", srv.APINowPlayingUpdate)
		admin.DELETE("/api/nowplaying/:id", srv.APINowPlayingDelete)
	}
	return srv, sess
}

func nowPlayingJSONRequest(srv *Server, method, path string, sess *http.Cookie, body any) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sess != nil {
		req.AddCookie(sess)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func createNowPlaying(t *testing.T, srv *Server, sess *http.Cookie, body map[string]any) map[string]any {
	t.Helper()
	w := nowPlayingJSONRequest(srv, http.MethodPost, "/admin/api/nowplaying", sess, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return created
}

func jsonID(m map[string]any) string {
	if v, ok := m["id"]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	if v, ok := m["ID"]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func TestAPINowPlayingCreatePlex201(t *testing.T) {
	srv, sess := nowPlayingTestServer(t)
	created := createNowPlaying(t, srv, sess, map[string]any{
		"name": "Living Room", "provider": "plex",
		"url": "http://plex.local:32400", "token": "tok", "show_album_art": true,
	})
	if jsonID(created) == "" || jsonID(created) == "0" {
		t.Fatalf("no id in %s", created)
	}
}

func TestAPINowPlayingCreateJellyfin201(t *testing.T) {
	srv, sess := nowPlayingTestServer(t)
	createNowPlaying(t, srv, sess, map[string]any{
		"name": "Jelly", "provider": "jellyfin", "url": "http://jelly.local:8096",
	})
}

func TestAPINowPlayingMissingURL400(t *testing.T) {
	srv, sess := nowPlayingTestServer(t)
	for _, provider := range []string{"plex", "jellyfin"} {
		w := nowPlayingJSONRequest(srv, http.MethodPost, "/admin/api/nowplaying", sess, map[string]any{
			"name": "No URL", "provider": provider,
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("provider %s: expected 400 got %d %s", provider, w.Code, w.Body.String())
		}
	}
}

func TestAPINowPlayingUnknownProvider400(t *testing.T) {
	srv, sess := nowPlayingTestServer(t)
	w := nowPlayingJSONRequest(srv, http.MethodPost, "/admin/api/nowplaying", sess, map[string]any{
		"name": "Bad", "provider": "soundcloud", "url": "http://x",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPINowPlayingMissingName400(t *testing.T) {
	srv, sess := nowPlayingTestServer(t)
	w := nowPlayingJSONRequest(srv, http.MethodPost, "/admin/api/nowplaying", sess, map[string]any{
		"name": "", "provider": "spotify", "token": "tok",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPINowPlayingUnauthenticated(t *testing.T) {
	srv, _ := nowPlayingTestServer(t)
	w := nowPlayingJSONRequest(srv, http.MethodPost, "/admin/api/nowplaying", nil, map[string]any{
		"name": "X", "provider": "spotify", "token": "tok",
	})
	if w.Code != http.StatusFound && w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 302 or 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPINowPlayingUpdateDeleteRoundTrip(t *testing.T) {
	srv, sess := nowPlayingTestServer(t)
	created := createNowPlaying(t, srv, sess, map[string]any{
		"name": "Before", "provider": "plex", "url": "http://old:32400", "token": "secret",
	})
	id := jsonID(created)
	if id == "" {
		t.Fatalf("no id in %s", created)
	}
	w := nowPlayingJSONRequest(srv, http.MethodPut, "/admin/api/nowplaying/"+id, sess, map[string]any{
		"name": "After", "provider": "plex", "url": "http://new:32400",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	w = nowPlayingJSONRequest(srv, http.MethodGet, "/admin/api/nowplaying/"+id, sess, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "After") {
		t.Fatalf("get after update: %d %s", w.Code, w.Body.String())
	}
	// token must survive an update that omits it (blank = preserve).
	if !strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("token was blanked on update: %s", w.Body.String())
	}
	w = nowPlayingJSONRequest(srv, http.MethodDelete, "/admin/api/nowplaying/"+id, sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = nowPlayingJSONRequest(srv, http.MethodGet, "/admin/api/nowplaying/"+id, sess, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete got %d", w.Code)
	}
}
