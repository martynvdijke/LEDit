package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHealthRegistryRecordSuccess(t *testing.T) {
	r := NewHealthRegistry()
	before := time.Now()
	r.RecordSuccess("weather:1", 42*time.Millisecond)
	r.RecordSuccess("weather:1", 58*time.Millisecond)

	sh := r.Snapshot()["weather:1"]
	if sh.ConsecutiveFails != 0 {
		t.Fatalf("ConsecutiveFails = %d, want 0", sh.ConsecutiveFails)
	}
	if sh.LastError != "" {
		t.Fatalf("LastError = %q, want empty", sh.LastError)
	}
	if sh.Renders != 2 {
		t.Fatalf("Renders = %d, want 2", sh.Renders)
	}
	if sh.Failures != 0 {
		t.Fatalf("Failures = %d, want 0", sh.Failures)
	}
	if sh.LastSuccessAt.Before(before) {
		t.Fatalf("LastSuccessAt not updated")
	}
	if sh.LastDuration != 58*time.Millisecond {
		t.Fatalf("LastDuration = %v, want 58ms (last write wins)", sh.LastDuration)
	}
}

func TestHealthRegistryRecordFailure(t *testing.T) {
	r := NewHealthRegistry()
	err := errors.New("boom")
	r.RecordSuccess("f1:2", 10*time.Millisecond)
	r.RecordFailure("f1:2", err, 5*time.Millisecond)

	sh := r.Snapshot()["f1:2"]
	if sh.ConsecutiveFails != 1 {
		t.Fatalf("ConsecutiveFails = %d, want 1", sh.ConsecutiveFails)
	}
	if sh.LastError != "boom" {
		t.Fatalf("LastError = %q, want boom", sh.LastError)
	}
	if sh.Failures != 1 {
		t.Fatalf("Failures = %d, want 1", sh.Failures)
	}
	if sh.Renders != 1 {
		t.Fatalf("Renders = %d, want 1", sh.Renders)
	}
}

func TestHealthStatusClassification(t *testing.T) {
	cases := []struct {
		name string
		sh   SourceHealth
		want string
	}{
		{"fresh", SourceHealth{ConsecutiveFails: 0}, "green"},
		{"one fail after success", SourceHealth{Renders: 5, ConsecutiveFails: 1}, "yellow"},
		{"two fails after success", SourceHealth{Renders: 5, ConsecutiveFails: 2}, "yellow"},
		{"three fails after success", SourceHealth{Renders: 5, ConsecutiveFails: 3}, "red"},
		{"fail with no prior success", SourceHealth{ConsecutiveFails: 1, Renders: 0}, "red"},
		{"many fails", SourceHealth{Renders: 5, ConsecutiveFails: 9}, "red"},
	}
	for _, tc := range cases {
		if got := StatusOf(tc.sh); got != tc.want {
			t.Errorf("%s: StatusOf = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHealthEWMA(t *testing.T) {
	r := NewHealthRegistry()
	r.RecordSuccess("x:1", 100*time.Millisecond)
	// Warm-up: first sample raw.
	if got := r.Snapshot()["x:1"].EWMADurationMs; got != 100.0 {
		t.Fatalf("first EWMA = %v, want 100", got)
	}
	r.RecordSuccess("x:1", 200*time.Millisecond)
	// 0.3*200 + 0.7*100 = 130
	if got := r.Snapshot()["x:1"].EWMADurationMs; got != 130.0 {
		t.Fatalf("second EWMA = %v, want 130", got)
	}
}

func TestHealthCacheCounters(t *testing.T) {
	r := NewHealthRegistry()
	r.RecordMatrixCacheHit()
	r.RecordMatrixCacheHit()
	r.RecordMatrixCacheMiss()
	hits, misses := r.CacheCounters()
	if hits != 2 || misses != 1 {
		t.Fatalf("counters = %d/%d, want 2/1", hits, misses)
	}
}

func TestHealthSnapshotCopy(t *testing.T) {
	r := NewHealthRegistry()
	r.RecordSuccess("a:1", time.Millisecond)
	snap := r.Snapshot()
	entry := snap["a:1"]
	entry.Renders = 999 // mutate the copy
	snap["a:1"] = entry
	if got := r.Snapshot()["a:1"].Renders; got != 1 {
		t.Fatalf("registry mutated via snapshot: Renders = %d, want 1", got)
	}
}

func TestClassifySummary(t *testing.T) {
	r := NewHealthRegistry()
	r.RecordSuccess("a:1", time.Millisecond)   // green
	r.RecordFailure("b:1", errors.New("x"), 0) // red (no prior success)
	r.RecordSuccess("c:1", time.Millisecond)   // green
	r.RecordFailure("d:1", errors.New("y"), 0) // red
	r.RecordSuccess("e:1", time.Millisecond)   // green
	r.RecordFailure("e:1", errors.New("z"), 0) // yellow (1 fail after success)
	g, y, red := classifySummary(r.Snapshot())
	if g != 2 || y != 1 || red != 2 {
		t.Fatalf("summary = %d/%d/%d, want 2/1/2", g, y, red)
	}
}

func TestDeviceLiveness(t *testing.T) {
	now := time.Now()
	within := now.Add(-2 * time.Minute)
	justOver := now.Add(-3*time.Minute - 100*time.Millisecond)
	over := now.Add(-4 * time.Minute)
	cases := []struct {
		name     string
		lastSeen *time.Time
		interval int
		want     string
	}{
		{"never seen", nil, 60, "never"},
		{"just seen", &now, 60, "alive"},
		{"within 3x", &within, 60, "alive"},
		{"just over 3x", &justOver, 60, "stale"},
		{"over 3x", &over, 60, "stale"},
	}
	for _, tc := range cases {
		if got := deviceLiveness(tc.lastSeen, tc.interval); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFeedRecordsSourceHealth drives serveFeed over a real WebSocket with one
// healthy and one failing source and asserts the health registry records
// success for the former and failures for the latter.
func TestFeedRecordsSourceHealth(t *testing.T) {
	Health.Reset()
	defer Health.Reset()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		sources := []sourceWithName{
			{Name: "OK", Source: &okDS{}, cacheKey: "ok:1"},
			{Name: "Bad", Source: &failingDS{}, cacheKey: "bad:1"},
		}
		serveFeed(conn, feedConn{}, sources, false, 30*time.Millisecond, 64, 64, &FeedController{})
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:]+"/feed", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Read frames until both sources have rendered at least once.
	deadline := time.Now().Add(5 * time.Second)
	conn.SetReadDeadline(deadline)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read frame: %v", err)
		}
		snap := Health.Snapshot()
		if sh, ok := snap["ok:1"]; ok && sh.Renders > 0 {
			if bad, ok := snap["bad:1"]; ok && bad.Failures > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			break
		}
	}

	snap := Health.Snapshot()
	ok, okFound := snap["ok:1"]
	if !okFound {
		t.Fatalf("no health entry for ok:1")
	}
	if ok.Renders == 0 {
		t.Fatalf("ok:1 has no successful renders")
	}
	if ok.ConsecutiveFails != 0 {
		t.Fatalf("ok:1 ConsecutiveFails = %d, want 0", ok.ConsecutiveFails)
	}
	bad, badFound := snap["bad:1"]
	if !badFound {
		t.Fatalf("no health entry for bad:1")
	}
	if bad.Failures == 0 {
		t.Fatalf("bad:1 has no recorded failures")
	}
	if bad.ConsecutiveFails == 0 {
		t.Fatalf("bad:1 ConsecutiveFails = 0, want > 0")
	}
}

func TestEndpointHealthKey(t *testing.T) {
	cases := []struct {
		ep   string
		id   int
		want string
	}{
		{"weather", 3, "weather:3"},
		{"rssfeed", 1, "rssfeed:1"},
		{"matrixlayout", 2, "matrix:2"},
		{"countdowns", 4, "countdown:4"},
		{"aidigests", 5, "aidigest:5"},
		{"textslides", 6, "textslides:6"},
	}
	for _, tc := range cases {
		if got := endpointHealthKey(tc.ep, tc.id); got != tc.want {
			t.Errorf("endpointHealthKey(%s,%d) = %q, want %q", tc.ep, tc.id, got, tc.want)
		}
	}
}

func TestSortedHealthRows(t *testing.T) {
	r := NewHealthRegistry()
	r.RecordSuccess("z:1", time.Millisecond)
	r.RecordFailure("a:1", errors.New("err"), time.Millisecond)
	rows := sortedHealthRows(r.Snapshot())
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Key != "a:1" || rows[1].Key != "z:1" {
		t.Fatalf("rows not sorted: %q, %q", rows[0].Key, rows[1].Key)
	}
	if rows[0].Status != "red" {
		t.Fatalf("a:1 status = %q, want red", rows[0].Status)
	}
	if rows[1].Status != "green" {
		t.Fatalf("z:1 status = %q, want green", rows[1].Status)
	}
	if !strings.Contains(rows[0].LastError, "err") {
		t.Fatalf("a:1 LastError = %q, want to contain err", rows[0].LastError)
	}
}
