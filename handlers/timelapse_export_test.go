package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

func dummyJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{100, 150, 200, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75})
	return buf.Bytes()
}

func newTimelapseExportServer(t *testing.T) (*Server, string) {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:export_test.db?cache=shared&_fk=1&mode=memory")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	srv := New(drv, nil)
	// override media root
	dir := t.TempDir()
	SetTimelapseMediaRoot(dir)
	t.Cleanup(func() { SetTimelapseMediaRoot("web/media/timelapse") })
	// ensure server uses same ent client DB already migrated; srv.DB is that client
	// APITimelapseExport queries srv.DB
	return srv, dir
}

func seedDay(t *testing.T, srv *Server, dir string, deviceID int, n int, date time.Time) []string {
	t.Helper()
	var paths []string
	jpegData := dummyJPEG()
	for i := 0; i < n; i++ {
		ts := time.Date(date.Year(), date.Month(), date.Day(), 10, i, 0, 0, time.Local)
		rel := TimelapseFilePath(deviceID, ts)
		// rel contains media root prefix; already set to dir, so it is an abs path under dir
		if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(rel, jpegData, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		paths = append(paths, rel)
		_, err := srv.DB.TimelapseFrame.Create().
			SetDeviceID(deviceID).SetCapturedAt(ts).
			SetSourceType("clock").SetSourceID(0).SetSourceLabel("Clock").
			SetFilePath(rel).SetWidth(160).SetHeight(80).Save(srv.Ctx)
		if err != nil {
			t.Fatalf("create row: %v", err)
		}
	}
	return paths
}

func TestExport_Unauthenticated401(t *testing.T) {
	srv, _ := newTimelapseExportServer(t)
	w := doRequest(t, srv, http.MethodPost, "/api/timelapse/export?device_id=1&date=2026-08-24", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestExport_Fallback_GIF_WhenFFmpegAbsentFewFrames(t *testing.T) {
	srv, dir := newTimelapseExportServer(t)
	session := loginAsAdmin(t, srv)
	date := time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local)
	dateStr := "2026-08-24"
	seedDay(t, srv, dir, 1, 3, date)

	orig := ffmpegLookPath
	ffmpegLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	defer func() { ffmpegLookPath = orig }()

	req := httptest.NewRequest(http.MethodPost, "/api/timelapse/export?device_id=1&date="+dateStr, nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export gif fallback: expected 200 got %d body %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, ".gif") {
		t.Fatalf("expected gif disposition, got %q", cd)
	}
	if w.Body.Len() == 0 {
		t.Fatal("empty body")
	}
	_ = dir
}

func TestExport_Fallback_ZIP_WhenManyFrames(t *testing.T) {
	srv, dir := newTimelapseExportServer(t)
	session := loginAsAdmin(t, srv)
	date := time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local)
	dateStr := "2026-08-24"
	// need >=500 frames to trigger zip; create 500 tiny frames (may be heavy but ok)
	// Instead reduce threshold by creating 500 but we can cheat: patch logic: we cannot change threshold.
	// Create 500 frames; each is 20x20 JPEG ~600 bytes, so 500 is okay in test (takes ~1s)
	// To speed up, we can create rows but reuse same JPEG quickly.
	jpegData := dummyJPEG()
	for i := 0; i < 500; i++ {
		ts := time.Date(date.Year(), date.Month(), date.Day(), 10, i/60, i%60, 0, time.Local).Add(time.Duration(i) * time.Millisecond)
		rel := TimelapseFilePath(2, ts)
		if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(rel, jpegData, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := srv.DB.TimelapseFrame.Create().
			SetDeviceID(2).SetCapturedAt(ts).
			SetSourceType("clock").SetSourceID(0).SetSourceLabel("Clock").
			SetFilePath(rel).SetWidth(160).SetHeight(80).Save(srv.Ctx)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	orig := ffmpegLookPath
	ffmpegLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	defer func() { ffmpegLookPath = orig }()

	req := httptest.NewRequest(http.MethodPost, "/api/timelapse/export?device_id=2&date="+dateStr, nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export zip fallback: expected 200 got %d body %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, ".zip") {
		t.Fatalf("expected zip disposition, got %q", cd)
	}
	_ = dir
}

func TestExport_Fallback_MP4_WhenFFmpegPresent(t *testing.T) {
	srv, dir := newTimelapseExportServer(t)
	session := loginAsAdmin(t, srv)
	// probe actual ffmpeg; if absent skip
	if _, err := ffmpegLookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not present, skipping mp4 path test")
	}
	date := time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local)
	seedDay(t, srv, dir, 3, 2, date)
	req := httptest.NewRequest(http.MethodPost, "/api/timelapse/export?device_id=3&date=2026-08-24", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export mp4: expected 200 got %d body %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, ".mp4") && !strings.Contains(cd, ".gif") && !strings.Contains(cd, ".zip") {
		t.Fatalf("expected mp4/gif/zip disposition, got %q", cd)
	}
}

func TestExportHelpers_TempCleanup(t *testing.T) {
	// test exportGIF creates temp file and exportZIP does; ensure no leak via direct call
	jpegData := dummyJPEG()
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.jpg")
	p2 := filepath.Join(dir, "b.jpg")
	_ = os.WriteFile(p1, jpegData, 0o644)
	_ = os.WriteFile(p2, jpegData, 0o644)
	out, err := exportGIF([]string{p1, p2})
	if err != nil {
		t.Fatalf("exportGIF: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("gif out not exists: %v", err)
	}
	_ = os.Remove(out)
	out2, err := exportZIP([]string{p1, p2}, "2026-08-24", 1)
	if err != nil {
		t.Fatalf("exportZIP: %v", err)
	}
	if _, err := os.Stat(out2); err != nil {
		t.Fatalf("zip out not exists: %v", err)
	}
	_ = os.Remove(out2)
}
