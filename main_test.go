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

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
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
		"beer": "IPA",
		"abv":  "6.5",
		"brew": "Local",
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

func TestServerAdminF1New(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/f1/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminWeatherNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/weather/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminHomeAssistantNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/homeassistant/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminUntappdNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/untappd/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminImageNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/images/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCryptoDatasource(t *testing.T) {
	s := &datasource.CryptoDS{Token: "bitcoin"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Crypto GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestServerAdminCryptoNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/crypto/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminVideoNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/videos/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminF1CreateAndEditAndDelete(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("token=f1token&url=http://f1api")
	req := httptest.NewRequest("POST", "/admin/datasources/f1/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithF1().OnlyX(testCtx)
	f1s, _ := settings.Edges.F1OrErr()
	if len(f1s) != 1 || f1s[0].Token != "f1token" {
		t.Errorf("expected 1 F1 source with token f1token, got %d %v", len(f1s), f1s)
	}

	editReq := httptest.NewRequest("GET", "/admin/datasources/f1/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for edit, got %d", w2.Code)
	}

	delReq := httptest.NewRequest("POST", "/admin/datasources/f1/1/delete", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusFound {
		t.Errorf("expected 302 for delete, got %d", w3.Code)
	}

	exists := srv.DB.F1.Query().ExistX(testCtx)
	if exists {
		t.Error("expected F1 source to be deleted")
	}
}

func TestServerAdminDatasourceCreateAndEditCycle(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	tests := []struct {
		endpoint string
		token    string
		url      string
	}{
		{"sonarr", "stoken", "surl"},
		{"radarr", "rtoken", "rurl"},
		{"f1", "ftoken", "furl"},
		{"weather", "wtoken", "wurl"},
		{"homeassistant", "hatoken", "haurl"},
		{"untappd", "utoken", "uurl"},
		{"crypto", "bitcoin", ""},
		{"stock", "aapl", "https://finance.yahoo.com"},
		{"rssfeed", "", "http://example.com/rss"},
		{"calendar", "", "http://example.com/cal"},
	}

	for _, tt := range tests {
		body := bytes.NewBufferString("token=" + tt.token + "&url=" + tt.url)
		req := httptest.NewRequest("POST", "/admin/datasources/"+tt.endpoint+"/new", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("%s create: expected 302, got %d", tt.endpoint, w.Code)
		}
	}

	// Create a textslide (different fields)
	tsBody := bytes.NewBufferString("content=TestSlide&color=%23FFFFFF&bg_color=%23000000&font_size=32")
	tsReq := httptest.NewRequest("POST", "/admin/textslides/new", tsBody)
	tsReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tsW := httptest.NewRecorder()
	srv.ServeHTTP(tsW, tsReq)
	if tsW.Code != http.StatusFound {
		t.Errorf("textslide create: expected 302, got %d", tsW.Code)
	}

	settings := srv.DB.GeneralSettings.Query().
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().WithCrypto().
		WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		OnlyX(testCtx)

	sonarr, _ := settings.Edges.SonarrOrErr()
	radarr, _ := settings.Edges.RadarrOrErr()
	f1, _ := settings.Edges.F1OrErr()
	weather, _ := settings.Edges.WeatherOrErr()
	ha, _ := settings.Edges.HomeAssistantOrErr()
	untappd, _ := settings.Edges.UntappdOrErr()
	crypto, _ := settings.Edges.CryptoOrErr()
	stocks, _ := settings.Edges.StocksOrErr()
	rssFeeds, _ := settings.Edges.RssFeedsOrErr()
	calendars, _ := settings.Edges.CalendarsOrErr()
	textSlides, _ := settings.Edges.TextSlidesOrErr()

	if len(sonarr) != 1 || sonarr[0].Token != "stoken" {
		t.Error("Sonarr not created correctly")
	}
	if len(f1) != 1 || f1[0].Token != "ftoken" {
		t.Error("F1 not created correctly")
	}
	if len(weather) != 1 || weather[0].Token != "wtoken" {
		t.Error("Weather not created correctly")
	}
	if len(ha) != 1 || ha[0].Token != "hatoken" {
		t.Error("HomeAssistant not created correctly")
	}
	if len(untappd) != 1 || untappd[0].Token != "utoken" {
		t.Error("Untappd not created correctly")
	}
	if len(radarr) != 1 || radarr[0].Token != "rtoken" {
		t.Error("Radarr not created correctly")
	}
	if len(crypto) != 1 || crypto[0].Token != "bitcoin" {
		t.Error("Crypto not created correctly")
	}
	if len(stocks) != 1 || stocks[0].Token != "aapl" {
		t.Error("Stock not created correctly")
	}
	if len(rssFeeds) != 1 || rssFeeds[0].URL != "http://example.com/rss" {
		t.Error("RssFeed not created correctly")
	}
	if len(calendars) != 1 || calendars[0].URL != "http://example.com/cal" {
		t.Error("Calendar not created correctly")
	}
	if len(textSlides) != 1 || textSlides[0].Content != "TestSlide" {
		t.Error("TextSlide not created correctly")
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

func TestAPIFeedStatus(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/api/feed/current", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["paused"] != false {
		t.Error("expected paused to be false")
	}
}

func TestAPIFeedPauseResume(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)

	body := bytes.NewBufferString(`{}`)
	body2 := bytes.NewBufferString(`{}`)

	req := httptest.NewRequest("POST", "/api/feed/pause", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	req2 := httptest.NewRequest("GET", "/api/feed/current", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["paused"] != true {
		t.Error("expected paused to be true after Pause")
	}

	req3 := httptest.NewRequest("POST", "/api/feed/resume", body2)
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w3.Code)
	}
}

func TestAPIWebhookNotify(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	body := bytes.NewBufferString(`{"title":"Test","message":"Hello"}`)
	req := httptest.NewRequest("POST", "/api/webhook/notify", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPINotificationHistory(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/api/notifications", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var notifs []map[string]any
	json.Unmarshal(w.Body.Bytes(), &notifs)
	if notifs == nil {
		t.Error("expected notification list")
	}
}

func TestServerAdminSchedules(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	req := httptest.NewRequest("GET", "/admin/schedules", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := bytes.NewBufferString("name=Morning&cron=08:00-12:00&enabled=on")
	req2 := httptest.NewRequest("POST", "/admin/schedules/new", body)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w2.Code)
	}

	exists := srv.DB.Schedule.Query().ExistX(testCtx)
	if !exists {
		t.Error("expected schedule to exist")
	}
}

func TestServerAdminDevices(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	req := httptest.NewRequest("GET", "/admin/devices", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Errorf("expected 200 or 302, got %d", w.Code)
	}
}

func TestServerAdminTheme(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/theme", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Errorf("expected 200 or 302, got %d", w.Code)
	}
}

func TestServerAdminAnalytics(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/analytics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Errorf("expected 200 or 302, got %d", w.Code)
	}
}

func TestLoginPage(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAnalyticsTracking(t *testing.T) {
	handlers.TrackDisplay("TestSource", 1.5)
	stats := handlers.GetAnalytics()
	if stats.TotalDisplays < 1 {
		t.Error("expected at least 1 display event")
	}
	if count, ok := stats.BySource["TestSource"]; !ok || count < 1 {
		t.Error("expected TestSource in analytics")
	}
}

func TestAdminNotificationsPage(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/notifications", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPIFeedNext(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest("POST", "/api/feed/next", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDSNFunction(t *testing.T) {
	dsn := db.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}
}

func TestDeviceSettingsCreateEditDelete(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("name=TestPi&ip=192.168.1.100&port=6270&enabled=on&width=64&height=32")
	req := httptest.NewRequest("POST", "/admin/devices/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("device create: expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithDeviceSettings().OnlyX(testCtx)
	devices, _ := settings.Edges.DeviceSettingsOrErr()
	if len(devices) != 1 || devices[0].Name != "TestPi" {
		t.Errorf("expected 1 device named TestPi, got %d", len(devices))
	}

	editReq := httptest.NewRequest("GET", "/admin/devices/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("device edit: expected 200, got %d", w2.Code)
	}

	delReq := httptest.NewRequest("POST", "/admin/devices/1/delete", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusFound {
		t.Errorf("device delete: expected 302, got %d", w3.Code)
	}

	exists := srv.DB.DeviceSettings.Query().ExistX(testCtx)
	if exists {
		t.Error("expected device to be deleted")
	}
}

func TestScheduleCreateAndEdit(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("name=Evening&cron=18:00-22:00&enabled=on")
	req := httptest.NewRequest("POST", "/admin/schedules/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("schedule create: expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithSchedules().OnlyX(testCtx)
	schedules, _ := settings.Edges.SchedulesOrErr()
	if len(schedules) != 1 || schedules[0].Name != "Evening" {
		t.Errorf("expected 1 schedule named Evening, got %d", len(schedules))
	}

	editReq := httptest.NewRequest("GET", "/admin/schedules/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("schedule edit: expected 200, got %d", w2.Code)
	}

	updateBody := bytes.NewBufferString("name=Night&cron=22:00-06:00&enabled=on")
	updReq := httptest.NewRequest("POST", "/admin/schedules/1/edit", updateBody)
	updReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, updReq)
	if w3.Code != http.StatusFound {
		t.Errorf("schedule update: expected 302, got %d", w3.Code)
	}

	sched := srv.DB.Schedule.GetX(testCtx, 1)
	if sched.Name != "Night" {
		t.Errorf("expected name to be Night, got %s", sched.Name)
	}
}

func TestLoginAction(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)

	body := bytes.NewBufferString("username=admin&password=ledit")
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("login: expected 302, got %d", w.Code)
	}
	if w.Header().Get("Set-Cookie") == "" {
		t.Error("login should set session cookie")
	}
}

func TestLoginActionInvalid(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)

	body := bytes.NewBufferString("username=admin&password=wrong")
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("failed login: expected 200 (shows form with error), got %d", w.Code)
	}
}

func TestDSUpdateAndNewForm(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	// Test update
	body := bytes.NewBufferString("token=newtoken&url=newurl")
	req := httptest.NewRequest("POST", "/admin/datasources/sonarr/1/edit", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound && w.Code != http.StatusOK {
		t.Errorf("update nonexistent: expected 302 or 200, got %d", w.Code)
	}

	// Test datasource_form for all types renders correctly
	types := []string{"sonarr", "radarr", "f1", "weather", "homeassistant", "untappd", "images", "videos", "crypto", "stock", "rssfeed", "calendar"}
	for _, ds := range types {
		req2 := httptest.NewRequest("GET", "/admin/datasources/"+ds+"/new", nil)
		w2 := httptest.NewRecorder()
		srv.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Errorf("GET /admin/datasources/%s/new: expected 200, got %d", ds, w2.Code)
		}
	}

	// Test textslide form renders correctly
	tsReq := httptest.NewRequest("GET", "/admin/textslides/new", nil)
	tsW := httptest.NewRecorder()
	srv.ServeHTTP(tsW, tsReq)
	if tsW.Code != http.StatusOK {
		t.Errorf("GET /admin/textslides/new: expected 200, got %d", tsW.Code)
	}
}

func TestAnalyticsGetStats(t *testing.T) {
	prevCount := handlers.GetAnalytics().TotalDisplays

	handlers.TrackDisplay("SourceA", 2.0)
	handlers.TrackDisplay("SourceB", 1.0)
	handlers.TrackDisplay("SourceA", 3.0)

	stats := handlers.GetAnalytics()
	if stats.TotalDisplays != prevCount+3 {
		t.Errorf("expected %d total displays (prev %d + 3), got %d", prevCount+3, prevCount, stats.TotalDisplays)
	}
	if stats.BySource["SourceA"] == 0 {
		t.Errorf("expected SourceA to have >0 displays")
	}
	if stats.BySource["SourceB"] == 0 {
		t.Errorf("expected SourceB to have >0 displays")
	}
	if stats.Uptime == "" {
		t.Error("expected non-empty uptime")
	}
	if len(stats.Recent) < 3 {
		t.Errorf("expected at least 3 recent events, got %d", len(stats.Recent))
	}
}

func TestFeedControllerPauseResume(t *testing.T) {
	// Reset state first
	handlers.GlobalFeed.Resume()
	for handlers.GlobalFeed.ShouldSkip() {
		// drain any pending skips
	}

	if handlers.GlobalFeed.IsPaused() {
		t.Error("feed should not be paused initially")
	}

	handlers.GlobalFeed.Pause()
	if !handlers.GlobalFeed.IsPaused() {
		t.Error("feed should be paused after Pause()")
	}

	status := handlers.GlobalFeed.Status()
	if s, ok := status["paused"].(bool); !ok || !s {
		t.Error("Status() should return paused=true")
	}

	handlers.GlobalFeed.Resume()
	if handlers.GlobalFeed.IsPaused() {
		t.Error("feed should not be paused after Resume()")
	}

	handlers.GlobalFeed.Next()
	if !handlers.GlobalFeed.ShouldSkip() {
		t.Error("ShouldSkip should return true after Next()")
	}
	skipAfterDrain := handlers.GlobalFeed.ShouldSkip()
	if skipAfterDrain {
		t.Error("ShouldSkip should return false after first read (one-shot)")
	}
}

func TestCryptoDSMultipleCoins(t *testing.T) {
	s := &datasource.CryptoDS{Token: "bitcoin,ethereum,solana"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("CryptoDS with multiple coins failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
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

func TestSystemStatsDatasource(t *testing.T) {
	s := &datasource.SystemStatsDS{}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("SystemStats GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
	if len(img.Data) == 0 {
		t.Error("expected non-empty image data")
	}
}

func TestRssFeedDatasource(t *testing.T) {
	s := &datasource.RssFeedDS{URL: "", Name: "TestFeed"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("RssFeed GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestCalendarDatasource(t *testing.T) {
	s := &datasource.CalendarDS{URL: "", Name: "TestCal"}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Calendar GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestStockDatasource(t *testing.T) {
	s := &datasource.StockDS{Token: "", URL: ""}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("Stock GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
}

func TestTextSlideDatasource(t *testing.T) {
	s := &datasource.TextSlideDS{Content: "Hello World", Color: "#FFFFFF", BgColor: "#000000", FontSize: 32}
	img, err := s.GetPNG()
	if err != nil {
		t.Fatalf("TextSlide GetPNG failed: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("expected PNG format, got %s", img.Format)
	}
	if len(img.Data) == 0 {
		t.Error("expected non-empty image data")
	}
}

func TestServerAdminStockNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/stock/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminRSSNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/rssfeed/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminCalendarNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/datasources/calendar/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminTextSlideNew(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	req := httptest.NewRequest("GET", "/admin/textslides/new", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServerAdminStockCreateAndEditAndDelete(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("token=aapl&url=https://finance.yahoo.com")
	req := httptest.NewRequest("POST", "/admin/datasources/stock/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithStocks().OnlyX(testCtx)
	stocks, _ := settings.Edges.StocksOrErr()
	if len(stocks) != 1 || stocks[0].Token != "aapl" {
		t.Errorf("expected 1 stock with token aapl, got %d %v", len(stocks), stocks)
	}

	editReq := httptest.NewRequest("GET", "/admin/datasources/stock/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for edit, got %d", w2.Code)
	}

	delReq := httptest.NewRequest("POST", "/admin/datasources/stock/1/delete", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusFound {
		t.Errorf("expected 302 for delete, got %d", w3.Code)
	}

	exists := srv.DB.Stock.Query().ExistX(testCtx)
	if exists {
		t.Error("expected stock to be deleted")
	}
}

func TestServerAdminRSSCreateAndEditAndDelete(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("url=http://example.com/rss&name=NewsFeed")
	req := httptest.NewRequest("POST", "/admin/datasources/rssfeed/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithRssFeeds().OnlyX(testCtx)
	feeds, _ := settings.Edges.RssFeedsOrErr()
	if len(feeds) != 1 || feeds[0].Name != "NewsFeed" || feeds[0].URL != "http://example.com/rss" {
		t.Errorf("expected 1 feed with name NewsFeed, got %d %v", len(feeds), feeds)
	}

	editReq := httptest.NewRequest("GET", "/admin/datasources/rssfeed/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for edit, got %d", w2.Code)
	}

	delReq := httptest.NewRequest("POST", "/admin/datasources/rssfeed/1/delete", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusFound {
		t.Errorf("expected 302 for delete, got %d", w3.Code)
	}

	exists := srv.DB.RssFeed.Query().ExistX(testCtx)
	if exists {
		t.Error("expected RSS feed to be deleted")
	}
}

func TestServerAdminCalendarCreateAndEditAndDelete(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("url=http://example.com/cal&name=MyCalendar")
	req := httptest.NewRequest("POST", "/admin/datasources/calendar/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithCalendars().OnlyX(testCtx)
	calendars, _ := settings.Edges.CalendarsOrErr()
	if len(calendars) != 1 || calendars[0].Name != "MyCalendar" || calendars[0].URL != "http://example.com/cal" {
		t.Errorf("expected 1 calendar with name MyCalendar, got %d %v", len(calendars), calendars)
	}

	editReq := httptest.NewRequest("GET", "/admin/datasources/calendar/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for edit, got %d", w2.Code)
	}

	delReq := httptest.NewRequest("POST", "/admin/datasources/calendar/1/delete", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusFound {
		t.Errorf("expected 302 for delete, got %d", w3.Code)
	}

	exists := srv.DB.Calendar.Query().ExistX(testCtx)
	if exists {
		t.Error("expected calendar to be deleted")
	}
}

func TestServerAdminTextSlideCreateAndEditAndDelete(t *testing.T) {
	drv := openTestDB(t)
	defer drv.Close()

	srv := handlers.New(drv)
	srv.DB.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		SaveX(testCtx)

	body := bytes.NewBufferString("content=Hello&color=%23FFFFFF&bg_color=%23000000&font_size=32")
	req := httptest.NewRequest("POST", "/admin/textslides/new", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	settings := srv.DB.GeneralSettings.Query().WithTextSlides().OnlyX(testCtx)
	slides, _ := settings.Edges.TextSlidesOrErr()
	if len(slides) != 1 || slides[0].Content != "Hello" {
		t.Errorf("expected 1 text slide with content Hello, got %d %v", len(slides), slides)
	}

	editReq := httptest.NewRequest("GET", "/admin/textslides/1/edit", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, editReq)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for edit, got %d", w2.Code)
	}

	delReq := httptest.NewRequest("POST", "/admin/textslides/1/delete", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, delReq)
	if w3.Code != http.StatusFound {
		t.Errorf("expected 302 for delete, got %d", w3.Code)
	}

	exists := srv.DB.TextSlide.Query().ExistX(testCtx)
	if exists {
		t.Error("expected text slide to be deleted")
	}
}
