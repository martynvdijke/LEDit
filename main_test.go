package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"ledit/datasource"
	"ledit/db"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/generalsettings"
	"ledit/handlers"
	"ledit/render"
	"ledit/render/themes"

	_ "github.com/mattn/go-sqlite3"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gorilla/websocket"
)

var testCtx = context.Background()

func TestMain(m *testing.M) {
	os.MkdirAll("testdata", 0755)
	code := m.Run()
	os.RemoveAll("testdata")
	os.Exit(code)
}

func openTestDB(t *testing.T) *sql.Driver {
	drv, err := sql.Open(dialect.SQLite, "file:test.db?cache=shared&_fk=1&mode=memory")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	return drv
}

func TestDBSchemaCreation(t *testing.T) {
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(openTestDB(t))))
	defer client.Close()

	settings := client.GeneralSettings.Create().
		SetTimeout(5.0).
		SetRandom(true).
		SetWidth(64).
		SetHeight(64).
		SaveX(testCtx)

	if settings.Timeout != 5.0 {
		t.Errorf("expected timeout 5.0, got %f", settings.Timeout)
	}
	if !settings.Random {
		t.Error("expected random to be true")
	}
	if settings.Width != 64 {
		t.Errorf("expected width 64, got %d", settings.Width)
	}
}

func TestGeneralSettingsCreateAndQuery(t *testing.T) {
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(openTestDB(t))))
	defer client.Close()

	client.GeneralSettings.Create().
		SetTimeout(3.0).
		SetRandom(false).
		SetWidth(128).
		SetHeight(32).
		SaveX(testCtx)

	settings := client.GeneralSettings.Query().Where(generalsettings.ID(1)).OnlyX(testCtx)
	if settings.Timeout != 3.0 {
		t.Errorf("expected 3.0, got %f", settings.Timeout)
	}
}

func TestSonarrDatasource(t *testing.T) {
	s := &datasource.SonarrDS{Token: "test", URL: "http://localhost"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Sonarr GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
	if len(img.Data) == 0 {
		t.Error("expected non-empty image data")
	}
}

func TestRadarrDatasource(t *testing.T) {
	s := &datasource.RadarrDS{Token: "test", URL: "http://localhost"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Radarr GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestF1Datasource(t *testing.T) {
	s := &datasource.F1DS{Token: "test", URL: "http://localhost"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("F1 GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestWeatherDatasource(t *testing.T) {
	s := &datasource.WeatherDS{Token: "test", URL: "http://localhost"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Weather GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestHomeAssistantDatasource(t *testing.T) {
	s := &datasource.HomeAssistantDS{Token: "test", URL: "http://localhost"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("HomeAssistant GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestUntappdDatasource(t *testing.T) {
	s := &datasource.UntappdDS{Token: "test", URL: "http://localhost"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Untappd GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestImageDatasource(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "test-*.png")
	defer os.Remove(tmpFile.Name())

	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	png.Encode(tmpFile, img)
	tmpFile.Close()

	s := &datasource.ImageDS{Path: tmpFile.Name()}
	result, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Image GetPNG failed: %v", err)
	}
	if result.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", result.Format)
	}
}

func TestImageDatasourceNotFound(t *testing.T) {
	s := &datasource.ImageDS{Path: "nonexistent.png"}
	_, err := s.GetPNG()
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestVideoDatasourceNotFound(t *testing.T) {
	s := &datasource.VideoDS{Path: "nonexistent.mp4"}
	_, err := s.GetPNG()
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestRenderDict(t *testing.T) {
	data := map[string]string{
		"name":    "test",
		"version": "1.0",
	}
	result, err := render.RenderDict(data, 400, 400, themes.DefaultTheme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderDict failed: %v", err)
	}
	if result.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", result.Format)
	}
	if len(result.Data) == 0 {
		t.Error("expected non-empty PNG data")
	}
	parsed, err := png.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatalf("Decode png failed: %v", err)
	}
	bounds := parsed.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 400 {
		t.Errorf("expected 400x400, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRenderDictDefaultTheme(t *testing.T) {
	data := map[string]string{
		"status": "ok",
	}
	result, err := render.RenderDict(data, 200, 200, themes.DefaultTheme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderDict with default theme failed: %v", err)
	}
	if result.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", result.Format)
	}
}

func TestRenderDictF1Theme(t *testing.T) {
	data := map[string]string{
		"race":  "Monaco",
		"lap":   "12",
		"speed": "280",
	}
	result, err := render.RenderDict(data, 400, 400, themes.F1Theme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderDict with F1 theme failed: %v", err)
	}
	if result.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", result.Format)
	}
}

func TestRenderDictUntappdTheme(t *testing.T) {
	data := map[string]string{
		"beer":  "IPA",
		"abv":   "6.5",
		"brew":  "Local",
	}
	result, err := render.RenderDict(data, 400, 400, themes.UntappdTheme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderDict with Untappd theme failed: %v", err)
	}
	if result.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", result.Format)
	}
}

func TestRenderFileExists(t *testing.T) {
	if !render.FileExists("main.go") {
		t.Error("main.go should exist")
	}
	if render.FileExists("nonexistent") {
		t.Error("nonexistent file should not exist")
	}
}

func TestRenderGetExtension(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"image.png", "png"},
		{"image.jpg", "jpg"},
		{"file.mp4", "mp4"},
		{"noext", ""},
	}
	for _, tt := range tests {
		got := render.GetExtension(tt.path)
		if got != tt.expected {
			t.Errorf("GetExtension(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

func TestServerIndexPage(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestServerAdminDashboard(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminSettings(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/settings", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminSonarrNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/sonarr/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerPostSettings(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	body := bytes.NewBufferString("timeout=3.5&random=on&width=64&height=64")
	req := httptest.NewRequest("POST", "/admin/settings", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).OnlyX(testCtx)
	if settings.Timeout != 3.5 {
		t.Errorf("expected timeout 3.5, got %f", settings.Timeout)
	}
	if !settings.Random {
		t.Error("expected random to be true")
	}
}

func TestDSNFunction(t *testing.T) {
	dsn := db.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}
}

func TestWebSocketUpgrade(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).
		SetRandom(false).
		SetWidth(64).
		SetHeight(64).
		SaveX(testCtx)

	s := httptest.NewServer(srv.Router)
	defer s.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+s.URL[4:]+"/ws/feed", nil)
	if err != nil {
		t.Skipf("WebSocket dial failed: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WebSocket read failed: %v", err)
	}
	var result map[string]string
	json.Unmarshal(msg, &result)
	if result["format"] != "PNG" && result["format"] != "MP4" && result["error"] != "no datasources configured" {
		t.Errorf("expected PNG/MP4 format or error message, got %v", result)
	}
}
