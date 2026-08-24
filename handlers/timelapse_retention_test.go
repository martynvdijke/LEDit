package handlers

import (
	"context"
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

func seedFrame(t *testing.T, client *ent.Client, dir string, deviceID int, capturedAt time.Time) string {
	t.Helper()
	path := TimelapseFilePath(deviceID, capturedAt)
	// ensures dir exists; path is under timelapseMediaRoot which is dir (abs)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// create file with known size 10KB
	b := make([]byte, 10*1024)
	for i := range b {
		b[i] = byte(i % 256)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := client.TimelapseFrame.Create().
		SetDeviceID(deviceID).SetCapturedAt(capturedAt).
		SetSourceType("clock").SetSourceID(0).SetSourceLabel("Clock").
		SetFilePath(path).SetWidth(160).SetHeight(80).Save(context.Background())
	if err != nil {
		t.Fatalf("create row: %v", err)
	}
	return path
}

func TestRetention_PruneByAge(t *testing.T) {
	drv, err := sql.Open(dialect.SQLite, "file:retention_age.db?cache=shared&_fk=1&mode=memory")
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
	prevClient := timelapseClient
	timelapseClient = client
	defer func() { timelapseClient = prevClient }()

	t.Setenv("TIMELAPSE_RETENTION_DAYS", "30")
	t.Setenv("TIMELAPSE_MAX_GB", "10")
	t.Setenv("TIMELAPSE_MAX_FRAMES", "10000")

	oldTime := time.Now().AddDate(0, 0, -31)
	recent := time.Now().AddDate(0, 0, -1)
	seedFrame(t, client, dir, 1, oldTime)
	seedFrame(t, client, dir, 1, recent)

	n, err := RunTimelapseCleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d want 1", n)
	}
	remaining, _ := client.TimelapseFrame.Query().All(context.Background())
	if len(remaining) != 1 {
		t.Fatalf("remaining = %d want 1", len(remaining))
	}
	if remaining[0].CapturedAt.Equal(oldTime) {
		t.Fatal("old frame not pruned")
	}
}

func TestRetention_PruneByPerDeviceCountCap(t *testing.T) {
	drv, err := sql.Open(dialect.SQLite, "file:retention_count.db?cache=shared&_fk=1&mode=memory")
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
	prevClient := timelapseClient
	timelapseClient = client
	defer func() { timelapseClient = prevClient }()

	t.Setenv("TIMELAPSE_RETENTION_DAYS", "30")
	t.Setenv("TIMELAPSE_MAX_GB", "10")
	t.Setenv("TIMELAPSE_MAX_FRAMES", "2")

	base := time.Now().Add(-3 * time.Hour)
	for i := 0; i < 4; i++ {
		seedFrame(t, client, dir, 5, base.Add(time.Duration(i)*time.Minute))
	}
	n, err := RunTimelapseCleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d want 2", n)
	}
	frames, _ := client.TimelapseFrame.Query().All(context.Background())
	if len(frames) != 2 {
		t.Fatalf("remaining = %d want 2", len(frames))
	}
	// remaining should be newest 2
	if frames[0].CapturedAt.After(frames[1].CapturedAt) {
		t.Fatal("frames not sorted asc after cleanup")
	}
}

func TestRetention_PruneBySizeCap_OldestFirst(t *testing.T) {
	drv, err := sql.Open(dialect.SQLite, "file:retention_size.db?cache=shared&_fk=1&mode=memory")
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
	prevClient := timelapseClient
	timelapseClient = client
	defer func() { timelapseClient = prevClient }()

	t.Setenv("TIMELAPSE_RETENTION_DAYS", "30")
	t.Setenv("TIMELAPSE_MAX_FRAMES", "10000")
	// ~15KB per file, cap ~0.00002 GB => ~20KB, so 3 files exceed
	t.Setenv("TIMELAPSE_MAX_GB", "0.00002")

	base := time.Now().Add(-3 * time.Hour)
	paths := make([]string, 3)
	for i := 0; i < 3; i++ {
		p := seedFrame(t, client, dir, 7, base.Add(time.Duration(i)*time.Minute))
		paths[i] = p
		// enlarge to 15KB (already 10KB, rewrite larger)
		b := make([]byte, 15*1024)
		_ = os.WriteFile(p, b, 0o644)
	}
	n, err := RunTimelapseCleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 deleted, got %d", n)
	}
	// oldest file should be gone
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("oldest file should be deleted")
	}
}
