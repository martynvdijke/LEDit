package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

func newHandlerTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:handler_test.db?cache=shared&_fk=1&mode=memory")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	srv := New(drv, nil)
	dir := t.TempDir()
	SetTimelapseMediaRoot(dir)
	t.Cleanup(func() { SetTimelapseMediaRoot("web/media/timelapse") })
	return srv, dir
}

func TestTimelapseFrames_AuthAndDayRange(t *testing.T) {
	srv, dir := newHandlerTestServer(t)
	// unauthenticated -> 401
	w := doRequest(t, srv, http.MethodGet, "/api/timelapse/frames?device_id=1&date=2026-08-24", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth frames: expected 401 got %d", w.Code)
	}
	session := loginAsAdmin(t, srv)
	// missing params -> 400
	req := httptest.NewRequest(http.MethodGet, "/api/timelapse/frames?device_id=1", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing date: expected 400 got %d", rec.Code)
	}
	// seed frame for today
	today := time.Now()
	todayStr := today.Format("2006-01-02")
	seedDay(t, srv, dir, 10, 1, today)
	// query correct day returns 1
	req = httptest.NewRequest(http.MethodGet, "/api/timelapse/frames?device_id=10&date="+todayStr, nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("frames query: expected 200 got %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Frames []struct {
			FilePath string `json:"file_path"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("frames = %d want 1", len(resp.Frames))
	}
	// different day returns 0
	req = httptest.NewRequest(http.MethodGet, "/api/timelapse/frames?device_id=10&date=2020-01-01", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("frames other day: expected 200 got %d", rec.Code)
	}
	var resp2 struct {
		Frames []any `json:"frames"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp2)
	if len(resp2.Frames) != 0 {
		t.Fatalf("frames for empty day = %d want 0", len(resp2.Frames))
	}
}

func TestTimelapseGallery_Auth(t *testing.T) {
	srv, _ := newHandlerTestServer(t)
	w := doRequest(t, srv, http.MethodGet, "/admin/timelapse", "")
	if w.Code != http.StatusFound {
		t.Fatalf("unauth gallery: expected 302 got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login" && loc != "/setup" {
		t.Fatalf("redirect loc = %q want /login or /setup", loc)
	}
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/admin/timelapse", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth gallery: expected 200 got %d", rec.Code)
	}
	body := rec.Body.String()
	if !containsTL(body, "Timelapse Gallery") {
		t.Fatal("gallery body missing title")
	}
}

func TestTimelapseExport_AuthAndEmptyDay(t *testing.T) {
	srv, _ := newHandlerTestServer(t)
	w := doRequest(t, srv, http.MethodPost, "/api/timelapse/export?device_id=1&date=2026-08-24", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth export: expected 401 got %d", w.Code)
	}
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/api/timelapse/export?device_id=1&date=2026-08-24", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty day export: expected 400 got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestCaptureEnqueue_NonBlocking(t *testing.T) {
	// Enqueue should not block even when channel full; test drops with warn once
	// Fill channel
	for i := 0; i < 128; i++ {
		timelapseCh <- captureJob{DeviceID: i}
	}
	// next enqueue should be non-blocking drop
	done := make(chan struct{})
	go func() {
		EnqueueTimelapseCapture(captureJob{DeviceID: 999})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("EnqueueTimelapseCapture blocked")
	}
	// drain
	for len(timelapseCh) > 0 {
		<-timelapseCh
	}
}

func TestMediaHandler_Auth(t *testing.T) {
	srv, dir := newHandlerTestServer(t)
	// create dummy file
	p := filepath.Join(dir, "shot.jpg")
	_ = os.WriteFile(p, []byte("fake"), 0o644)
	// unauth should 401
	w := doRequest(t, srv, http.MethodGet, "/media/timelapse/shot.jpg", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("media unauth: expected 401 got %d", w.Code)
	}
}

func containsTL(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
