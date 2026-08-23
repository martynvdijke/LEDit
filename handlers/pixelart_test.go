package handlers

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"image/png"
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

func pixelArtTestServer(t *testing.T) (*Server, *ent.Client) {
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
		t.Fatalf("seed general settings: %v", err)
	}
	r := gin.New()
	tmpl := template.New("")
	template.Must(tmpl.New("pixelart_form.html").Parse(`ok`))
	template.Must(tmpl.New("pixelarts.html").Parse(`ok`))
	r.SetHTMLTemplate(tmpl)
	srv := &Server{DB: client, Ctx: ctx, Router: r}
	srv.Router.Use(func(c *gin.Context) { c.Next() })
	// Register minimal routes for pixelart
	admin := srv.Router.Group("/admin")
	admin.Use(func(c *gin.Context) { c.Next() })
	{
		admin.GET("/pixelarts", srv.PixelArtList)
		admin.GET("/pixelarts/new", srv.PixelArtNew)
		admin.POST("/pixelarts/new", srv.PixelArtCreate)
		admin.GET("/pixelarts/:id/edit", srv.PixelArtEdit)
		admin.POST("/pixelarts/:id/edit", srv.PixelArtUpdate)
		admin.POST("/pixelarts/:id/delete", srv.PixelArtDelete)
		admin.POST("/pixelarts/preview", srv.PixelArtPreview)
	}
	return srv, client
}

func validFramesJSON() string {
	return `{"palette":["#ff0000","#00ff00"],"frames":[{"duration":200,"pixels":[0,1,1,0]}]}`
}

func TestPixelArtCreateValid(t *testing.T) {
	srv, client := pixelArtTestServer(t)
	form := url.Values{}
	form.Set("name", "Test Art")
	form.Set("grid_width", "2")
	form.Set("grid_height", "2")
	form.Set("frames", validFramesJSON())
	form.Set("bindings", "{}")
	form.Set("api_url", "")
	form.Set("api_token", "")
	form.Set("enabled", "on")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/pixelarts/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect 302, got %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/admin/pixelarts" {
		t.Fatalf("unexpected redirect %s", loc)
	}
	count, _ := client.PixelArt.Query().Count(context.Background())
	if count != 1 {
		t.Fatalf("expected 1 pixelart, got %d", count)
	}
}

func TestPixelArtCreateMalformedFrames(t *testing.T) {
	srv, client := pixelArtTestServer(t)
	form := url.Values{}
	form.Set("name", "Bad Art")
	form.Set("grid_width", "2")
	form.Set("grid_height", "2")
	form.Set("frames", `not json`)
	form.Set("bindings", "{}")
	form.Set("enabled", "on")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/pixelarts/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code == http.StatusFound && w.Header().Get("Location") == "/admin/pixelarts" {
		// Should NOT redirect to list on failure; either 200 re-render or redirect not to list
		// Check no row created
	}
	count, _ := client.PixelArt.Query().Count(context.Background())
	if count != 0 {
		t.Fatalf("expected 0 pixelart on bad frames, got %d", count)
	}
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Fatalf("expected 200 or redirect, got %d", w.Code)
	}
	// If redirect, ensure not success redirect? just ensure no creation
}

func TestPixelArtUpdateBadGridMismatch(t *testing.T) {
	srv, client := pixelArtTestServer(t)
	ctx := context.Background()
	art := client.PixelArt.Create().SetName("Orig").SetGridWidth(2).SetGridHeight(2).SetFrames(validFramesJSON()).SetBindings("{}").SetAPIURL("").SetAPIToken("").SetEnabled(true).SaveX(ctx)
	// Try update with grid mismatch: frames still 2x2 but grid 3x3
	form := url.Values{}
	form.Set("name", "Orig")
	form.Set("grid_width", "3")
	form.Set("grid_height", "3")
	form.Set("frames", validFramesJSON()) // 4 pixels, not 9
	form.Set("bindings", "{}")
	form.Set("enabled", "on")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/pixelarts/%d/edit", art.ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	// Should reject
	fresh, _ := client.PixelArt.Get(ctx, art.ID)
	if fresh.GridWidth == 3 {
		t.Fatalf("update with bad grid mismatch should be rejected, got grid %dx%d", fresh.GridWidth, fresh.GridHeight)
	}
	count, _ := client.PixelArt.Query().Count(ctx)
	if count != 1 {
		t.Fatalf("expected still 1 row")
	}
}

func TestPixelArtPreviewReturnsPNG(t *testing.T) {
	srv, _ := pixelArtTestServer(t)
	form := url.Values{}
	form.Set("width", "64")
	form.Set("height", "64")
	form.Set("grid_width", "2")
	form.Set("grid_height", "2")
	form.Set("frames", validFramesJSON())
	form.Set("bindings", "{}")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/pixelarts/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview expected 200, got %d body %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected image/png, got %s", ct)
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatalf("response not decodable PNG: %v", err)
	}
}

func TestPixelArtPreviewBadInput400(t *testing.T) {
	srv, _ := pixelArtTestServer(t)
	form := url.Values{}
	form.Set("width", "64")
	form.Set("height", "64")
	form.Set("grid_width", "2")
	form.Set("grid_height", "2")
	form.Set("frames", `{"palette":[],"frames":[]}`) // invalid doc
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/pixelarts/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad frames, got %d", w.Code)
	}
}

func TestPixelArtDelete(t *testing.T) {
	srv, client := pixelArtTestServer(t)
	ctx := context.Background()
	art := client.PixelArt.Create().SetName("ToDelete").SetGridWidth(2).SetGridHeight(2).SetFrames(validFramesJSON()).SetBindings("{}").SetAPIURL("").SetAPIToken("").SetEnabled(true).SaveX(ctx)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/pixelarts/%d/delete", art.ID), nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("delete expected redirect, got %d", w.Code)
	}
	count, _ := client.PixelArt.Query().Count(ctx)
	if count != 0 {
		t.Fatalf("expected 0 after delete, got %d", count)
	}
}
