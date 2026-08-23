package handlers

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func settingsTestServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r := gin.New()
	tmpl := template.New("")
	// need at least dummy templates for redirects; not used for POST
	template.Must(tmpl.New("settings.html").Parse(`ok`))
	r.SetHTMLTemplate(tmpl)
	srv := &Server{DB: client, Ctx: ctx, Router: r}
	// minimal middleware: need flash handling but we just check redirect + DB
	r.POST("/admin/settings", srv.AdminSettingsSave)
	return srv, client
}

func TestSettingsValidationRejectsBadStyle(t *testing.T) {
	srv, client := settingsTestServer(t)
	form := url.Values{}
	form.Set("timeout", "1")
	form.Set("width", "64")
	form.Set("height", "64")
	form.Set("transition_style", "spinny")
	form.Set("transition_ms", "500")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings" {
		t.Fatalf("expected redirect to /admin/settings, got %s", loc)
	}
	gs, _ := client.GeneralSettings.Get(context.Background(), 1)
	if gs.TransitionStyle == "spinny" {
		t.Fatalf("bad style persisted")
	}
}

func TestSettingsValidationRejectsBadMs(t *testing.T) {
	srv, client := settingsTestServer(t)
	form := url.Values{}
	form.Set("timeout", "1")
	form.Set("width", "64")
	form.Set("height", "64")
	form.Set("transition_style", "fade")
	form.Set("transition_ms", "50")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/settings" {
		t.Fatalf("expected redirect to /admin/settings, got %s", loc)
	}
	gs, _ := client.GeneralSettings.Get(context.Background(), 1)
	if gs.TransitionMs == 50 {
		t.Fatalf("bad ms persisted")
	}
}

func TestSettingsValidationPersistsValidPair(t *testing.T) {
	srv, client := settingsTestServer(t)
	form := url.Values{}
	form.Set("timeout", "1")
	form.Set("width", "64")
	form.Set("height", "64")
	form.Set("transition_style", "fade")
	form.Set("transition_ms", "750")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d body %s", w.Code, w.Body.String())
	}
	gs, _ := client.GeneralSettings.Get(context.Background(), 1)
	if gs.TransitionStyle != "fade" {
		t.Fatalf("expected fade, got %s", gs.TransitionStyle)
	}
	if gs.TransitionMs != 750 {
		t.Fatalf("expected 750, got %d", gs.TransitionMs)
	}
}
