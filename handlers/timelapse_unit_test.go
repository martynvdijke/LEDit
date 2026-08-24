package handlers

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func tinyPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 100, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestShouldCapture_RateLimit(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	interval := 30 * time.Second

	// first capture always
	if !ShouldCapture(now, time.Time{}, interval, "", "clock:0") {
		t.Fatal("first capture should be allowed")
	}
	// same source within interval -> blocked
	last := now.Add(-10 * time.Second)
	if ShouldCapture(now, last, interval, "clock:0", "clock:0") {
		t.Fatal("same source within interval should be blocked")
	}
	// same source after interval -> allowed
	last = now.Add(-31 * time.Second)
	if !ShouldCapture(now, last, interval, "clock:0", "clock:0") {
		t.Fatal("same source after interval should be allowed")
	}
	// same source exactly at interval -> allowed
	last = now.Add(-30 * time.Second)
	if !ShouldCapture(now, last, interval, "clock:0", "clock:0") {
		t.Fatal("same source at exact interval should be allowed")
	}
}

func TestShouldCapture_SourceChangeOverride(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	interval := 30 * time.Second

	// source change after 2s -> allowed despite interval
	last := now.Add(-3 * time.Second)
	if !ShouldCapture(now, last, interval, "clock:0", "weather:1") {
		t.Fatal("source change after 2s should be allowed")
	}
	// source change before de-dupe floor -> blocked
	last = now.Add(-1 * time.Second)
	if ShouldCapture(now, last, interval, "clock:0", "weather:1") {
		t.Fatal("source change within 2s should be blocked")
	}
	// source change exactly 2s -> allowed
	last = now.Add(-2 * time.Second)
	if !ShouldCapture(now, last, interval, "clock:0", "weather:1") {
		t.Fatal("source change at exactly 2s should be allowed")
	}
}

func TestTimelapseFilePath(t *testing.T) {
	ts := time.Date(2026, 8, 24, 14, 5, 9, 123000000, time.UTC)
	got := TimelapseFilePath(42, ts)
	want := filepath.Join("web/media/timelapse", "42", "2026-08-24", "140509_123.jpg")
	if got != want {
		t.Fatalf("TimelapseFilePath got %q want %q", got, want)
	}
	// ensure custom media root affects path
	orig := timelapseMediaRoot
	SetTimelapseMediaRoot("/tmp/custom")
	got2 := TimelapseFilePath(1, ts)
	if got2 != filepath.Join("/tmp/custom", "1", "2026-08-24", "140509_123.jpg") {
		t.Fatalf("custom root path mismatch: %q", got2)
	}
	SetTimelapseMediaRoot(orig)
}

func TestThumbnailJPEG(t *testing.T) {
	pngBytes := tinyPNG(64, 32)
	thumb, w, h, err := thumbnailJPEG(pngBytes, 160)
	if err != nil {
		t.Fatalf("thumbnailJPEG: %v", err)
	}
	if w != 160 {
		t.Fatalf("thumb width = %d want 160", w)
	}
	if h == 0 {
		t.Fatalf("thumb height zero")
	}
	if len(thumb) == 0 || thumb[0] != 0xFF || thumb[1] != 0xD8 {
		t.Fatalf("thumb not JPEG")
	}
	// invalid PNG
	if _, _, _, err := thumbnailJPEG([]byte("not png"), 160); err == nil {
		t.Fatal("expected error for invalid png")
	}
}

func TestProcessTimelapseJob_FileAndRowCreation(t *testing.T) {
	drv, err := sql.Open(dialect.SQLite, "file:timelapse_unit_test.db?cache=shared&_fk=1&mode=memory")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	defer client.Close()
	t.Cleanup(func() { drv.Close() })

	dir := t.TempDir()
	orig := timelapseMediaRoot
	SetTimelapseMediaRoot(dir)
	defer SetTimelapseMediaRoot(orig)

	// set global client for processTimelapseJob
	prevClient := timelapseClient
	timelapseClient = client
	defer func() { timelapseClient = prevClient }()

	pngBytes := tinyPNG(64, 64)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	job := captureJob{
		DeviceID: 99, CapturedAt: now, SourceType: "clock", SourceID: 0, SourceLabel: "Clock",
		PNGBytes: pngBytes, Width: 64, Height: 64,
	}
	if err := processTimelapseJob(job); err != nil {
		t.Fatalf("processTimelapseJob: %v", err)
	}
	rel := TimelapseFilePath(99, now)
	abs := filepath.Join(dir, "99", "2026-08-24", filepath.Base(rel))
	// TimelapseFilePath returns under dir, but dir is absolute so rel is abs
	if _, err := os.Stat(rel); err != nil {
		// try abs path variant
		if _, err2 := os.Stat(abs); err2 != nil {
			t.Fatalf("file not created at %q or %q: %v %v", rel, abs, err, err2)
		}
	}
	// DB row
	frames, err := client.TimelapseFrame.Query().All(context.Background())
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 row, got %d", len(frames))
	}
	if frames[0].DeviceID != 99 || frames[0].SourceType != "clock" {
		t.Fatalf("row mismatch: %+v", frames[0])
	}
	if frames[0].Width != 160 {
		t.Fatalf("width = %d want 160", frames[0].Width)
	}
	// invalid PNG should error and not create row
	job2 := captureJob{DeviceID: 99, CapturedAt: now.Add(time.Second), PNGBytes: []byte("bad"), Width: 64, Height: 64}
	if err := processTimelapseJob(job2); err == nil {
		t.Fatal("expected error for bad png")
	}
}
