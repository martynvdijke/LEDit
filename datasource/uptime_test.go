package datasource

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseUptimeTargets(t *testing.T) {
	tests := []struct {
		name string
		cfg  string
		want int // expected count, -1 means nil
	}{
		{"valid", `[{"name":"Router","url":"http://192.168.1.1","timeout_seconds":2}]`, 1},
		{"malformed", `not json`, -1},
		{"not-array", `{"name":"x"}`, -1},
		{"missing-fields", `[{"name":"","url":"http://a"},{"name":"B","url":""}]`, -1},
		{"clamps", `[{"name":"A","url":"http://a","timeout_seconds":0},{"name":"B","url":"http://b","timeout_seconds":100}]`, 2},
		{"empty", ``, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseUptimeTargets(tt.cfg)
			if tt.want == -1 {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != tt.want {
				t.Fatalf("len=%d want %d", len(got), tt.want)
			}
			if tt.name == "clamps" {
				if got[0].TimeoutSeconds != 1 {
					t.Fatalf("clamp low = %d want 1", got[0].TimeoutSeconds)
				}
				if got[1].TimeoutSeconds != 30 {
					t.Fatalf("clamp high = %d want 30", got[1].TimeoutSeconds)
				}
			}
			if tt.name == "valid" {
				if got[0].Name != "Router" || got[0].TimeoutSeconds != 2 {
					t.Fatalf("got %+v", got[0])
				}
			}
		})
	}
	t.Run("default timeout", func(t *testing.T) {
		got := ParseUptimeTargets(`[{"name":"A","url":"http://a"}]`)
		if len(got) != 1 || got[0].TimeoutSeconds != 2 {
			t.Fatalf("default timeout want 2 got %+v", got)
		}
	})
}

func TestBuildUptimeRows(t *testing.T) {
	targets := []UptimeTarget{
		{Name: "Router", URL: "http://a"},
		{Name: "VeryLongNameThatExceedsTwentyEightCharsLimitXX", URL: "http://b"},
	}
	// fake probe: first UP, second DOWN
	probe := func(t UptimeTarget) (bool, int) {
		if t.Name == "Router" {
			return true, 12
		}
		return false, 0
	}
	rows := BuildUptimeRows(targets, probe)
	if len(rows) != 2 {
		t.Fatalf("len %d", len(rows))
	}
	if rows[0][0] != "Router" || rows[0][1] != "UP 12ms" {
		t.Fatalf("row0 %+v", rows[0])
	}
	if rows[1][0] != "VeryLongNameThatExceedsTwent" { // truncated 28
		t.Fatalf("trunc got %q len %d", rows[1][0], len(rows[1][0]))
	}
	if rows[1][1] != "DOWN" {
		t.Fatalf("row1 %q", rows[1][1])
	}
	// cap 4
	many := make([]UptimeTarget, 6)
	for i := range many {
		many[i] = UptimeTarget{Name: fmt.Sprintf("N%d", i), URL: "http://x"}
	}
	rows = BuildUptimeRows(many, func(UptimeTarget) (bool, int) { return true, 1 })
	if len(rows) != 4 {
		t.Fatalf("cap want 4 got %d", len(rows))
	}
}

func TestUptimeHEADFallback(t *testing.T) {
	// server rejects HEAD with 405, accepts GET
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	target := UptimeTarget{Name: "Test", URL: srv.URL, TimeoutSeconds: 2}
	up, _ := probeUptimeTarget(target)
	if !up {
		t.Fatal("expected UP via HEAD->GET fallback")
	}
	// server that is unreachable should be DOWN (use closed server)
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed.Close()
	target2 := UptimeTarget{Name: "Down", URL: closed.URL, TimeoutSeconds: 1}
	up2, _ := probeUptimeTarget(target2)
	if up2 {
		t.Fatal("expected DOWN for closed server")
	}
}

func TestUptimeCacheHitAndExpiry(t *testing.T) {
	clearUptimeCache()
	origProbe := probeUptimeTarget
	defer func() { probeUptimeTarget = origProbe }()
	count := 0
	probeUptimeTarget = func(UptimeTarget) (bool, int) {
		count++
		return true, 5
	}
	cfg := `[{"name":"A","url":"http://example.com"}]`
	ds := &UptimeDS{Config: cfg}
	// use short TTL for test
	oldTTL := uptimeCacheTTL
	uptimeCacheTTL = 2 * time.Second
	defer func() { uptimeCacheTTL = oldTTL }()

	img1, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img1.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if count != 1 {
		t.Fatalf("count after first = %d want 1", count)
	}
	// second call within TTL should be cache hit, no extra probe
	img2, _ := ds.GetPNG(64, 64)
	if _, err := png.Decode(bytes.NewReader(img2.Data)); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if count != 1 {
		t.Fatalf("cache hit: count=%d want 1", count)
	}
	// wait expiry
	time.Sleep(2100 * time.Millisecond)
	ds.GetPNG(64, 64)
	if count != 2 {
		t.Fatalf("after expiry count=%d want 2", count)
	}
	// config change -> different key -> fresh probe
	countBefore := count
	ds2 := &UptimeDS{Config: `[{"name":"B","url":"http://example2.com"}]`}
	ds2.GetPNG(64, 64)
	if count != countBefore+1 {
		t.Fatalf("config change should probe, count=%d want %d", count, countBefore+1)
	}
	clearUptimeCache()
}

func TestUptimeFallbacks(t *testing.T) {
	clearUptimeCache()
	// zero targets
	ds := &UptimeDS{Config: `[]`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// all down
	origProbe := probeUptimeTarget
	defer func() { probeUptimeTarget = origProbe }()
	probeUptimeTarget = func(UptimeTarget) (bool, int) { return false, 0 }
	clearUptimeCache()
	ds2 := &UptimeDS{Config: `[{"name":"A","url":"http://a"},{"name":"B","url":"http://b"}]`}
	img2, _ := ds2.GetPNG(64, 64)
	if _, err := png.Decode(bytes.NewReader(img2.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// should be fallback (we can't easily assert content, but should not error and be PNG)
	clearUptimeCache()
}
