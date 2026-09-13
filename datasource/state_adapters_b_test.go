package datasource

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPiHoleCurrentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"enabled","queries_today":123,"ads_blocked_today":42,"ads_percentage_today":12.34}`))
	}))
	defer srv.Close()
	ds := &PiHoleDS{Token: "tok", URL: srv.URL + "?summary"}
	m, err := ds.CurrentState(nil)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if v, ok := m["blockedQueries"]; !ok || v.(int) != 42 {
		t.Fatalf("blockedQueries=%v", m["blockedQueries"])
	}
	if v, ok := m["percentage"]; !ok || v.(float64) != 12.34 {
		t.Fatalf("percentage=%v want 12.34", m["percentage"])
	}
	// error case
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	}))
	defer bad.Close()
	ds2 := &PiHoleDS{Token: "tok", URL: bad.URL}
	if _, err := ds2.CurrentState(nil); err == nil {
		t.Fatal("expected error on fetch fail")
	}
}

func TestUptimeCurrentState_CacheReuse(t *testing.T) {
	clearUptimeCache()
	orig := probeUptimeTarget
	defer func() { probeUptimeTarget = orig }()
	count := 0
	probeUptimeTarget = func(UptimeTarget) (bool, int) {
		count++
		// alternate: first target up, second down
		if count%2 == 1 {
			return true, 10
		}
		return false, 0
	}
	// Use two targets via Config; probe called per target
	cfg := `[{"name":"A","url":"http://a"},{"name":"B","url":"http://b"}]`
	ds := &UptimeDS{Config: cfg}
	// First call probes 2 targets
	m1, err := ds.CurrentState(nil)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m1["status"] != "DEGRADED" || m1["up"].(int) != 1 || m1["total"].(int) != 2 {
		t.Fatalf("m1=%v", m1)
	}
	c1 := count
	// Second call should be cache hit, no extra probes
	m2, _ := ds.CurrentState(nil)
	if count != c1 {
		t.Fatalf("cache not reused: count %d want %d", count, c1)
	}
	if m2["status"] != "DEGRADED" {
		t.Fatalf("m2 status %v", m2["status"])
	}
	// Empty targets
	dsEmpty := &UptimeDS{Config: `[]`}
	m3, _ := dsEmpty.CurrentState(nil)
	if m3["status"] != "UNKNOWN" || m3["up"].(int) != 0 || m3["total"].(int) != 0 {
		t.Fatalf("empty %v", m3)
	}
	// All up / all down
	clearUptimeCache()
	probeUptimeTarget = func(UptimeTarget) (bool, int) { return true, 5 }
	dsUp := &UptimeDS{Config: `[{"name":"A","url":"http://a"}]`}
	mUp, _ := dsUp.CurrentState(nil)
	if mUp["status"] != "UP" {
		t.Fatalf("want UP got %v", mUp)
	}
	clearUptimeCache()
	probeUptimeTarget = func(UptimeTarget) (bool, int) { return false, 0 }
	dsDown := &UptimeDS{Config: `[{"name":"A","url":"http://a"}]`}
	mDown, _ := dsDown.CurrentState(nil)
	if mDown["status"] != "DOWN" {
		t.Fatalf("want DOWN got %v", mDown)
	}
	clearUptimeCache()
}

func TestGitHubCurrentState(t *testing.T) {
	// success with pulls
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"open_issues_count":10,"stargazers_count":1,"forks_count":1,"pushed_at":"2026-01-01T00:00:00Z"}`))
	})
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("state query %q", r.URL.Query().Get("state"))
		}
		w.Write([]byte(`[{},{},{}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ds := &GitHubDS{Token: "owner/repo", URL: srv.URL + "/repos/%s"}
	m, err := ds.CurrentState(nil)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["openIssues"].(int) != 10 || m["openPRs"].(int) != 3 {
		t.Fatalf("got %v", m)
	}
	// pulls failure -> openPRs 0, no error
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/repos/a/b", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"open_issues_count":5}`)) })
	mux2.HandleFunc("/repos/a/b/pulls", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fail", 500) })
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()
	ds2 := &GitHubDS{Token: "a/b", URL: srv2.URL + "/repos/%s"}
	m2, err := ds2.CurrentState(nil)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if m2["openPRs"].(int) != 0 {
		t.Fatalf("want 0 PRs got %v", m2)
	}
	// fetch error
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) }))
	defer bad.Close()
	ds3 := &GitHubDS{Token: "a/b", URL: bad.URL + "/%s"}
	if _, err := ds3.CurrentState(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestJellyfinCurrentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "tok" {
			t.Errorf("header %q", r.Header.Get("X-Emby-Token"))
		}
		w.Write([]byte(`[{"UserName":"a","NowPlayingItem":{"Name":"x"}},{"UserName":"b"},{"UserName":"c","NowPlayingItem":{"Name":"y"}}]`))
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "tok", URL: srv.URL}
	m, err := ds.CurrentState(nil)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["activeStreams"].(int) != 2 {
		t.Fatalf("want 2 got %v", m)
	}
	// empty
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) }))
	defer srv2.Close()
	ds2 := &JellyfinDS{Token: "tok", URL: srv2.URL}
	m2, _ := ds2.CurrentState(nil)
	if m2["activeStreams"].(int) != 0 {
		t.Fatalf("want 0 got %v", m2)
	}
	// error
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) }))
	defer bad.Close()
	ds3 := &JellyfinDS{Token: "tok", URL: bad.URL}
	if _, err := ds3.CurrentState(nil); err == nil {
		t.Fatal("expected error")
	}
}
