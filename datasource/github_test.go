package datasource

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildGitHubRows_RelativeDate(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		offset time.Duration
		want   string
	}{
		{"45m", 45 * time.Minute, "45m"},
		{"3h", 3 * time.Hour, "3h"},
		{"2d", 48 * time.Hour, "2d"},
		{"30m", 30 * time.Minute, "30m"},
		{"24h", 24 * time.Hour, "1d"},
		{"59m", 59 * time.Minute, "59m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pushed := now.Add(-tt.offset).Format(time.RFC3339)
			body := fmt.Sprintf(`{"stargazers_count":10,"open_issues_count":2,"forks_count":5,"pushed_at":"%s"}`, pushed)
			rows, err := BuildGitHubRows([]byte(body), now)
			if err != nil {
				t.Fatalf("BuildGitHubRows: %v", err)
			}
			var push string
			for _, r := range rows {
				if r[0] == "PUSH" {
					push = r[1]
				}
			}
			if push != tt.want {
				t.Fatalf("PUSH = %q want %q", push, tt.want)
			}
		})
	}
}

func TestBuildGitHubRows_Values(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	pushed := now.Add(-2 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{"stargazers_count":123,"open_issues_count":7,"forks_count":42,"pushed_at":"%s"}`, pushed)
	rows, err := BuildGitHubRows([]byte(body), now)
	if err != nil {
		t.Fatalf("BuildGitHubRows: %v", err)
	}
	m := map[string]string{}
	for _, r := range rows {
		m[r[0]] = r[1]
	}
	if m["STARS"] != "123" {
		t.Fatalf("STARS %q", m["STARS"])
	}
	if m["ISSUES"] != "7" {
		t.Fatalf("ISSUES %q", m["ISSUES"])
	}
	if m["FORKS"] != "42" {
		t.Fatalf("FORKS %q", m["FORKS"])
	}
	if m["PUSH"] != "2h" {
		t.Fatalf("PUSH %q want 2h", m["PUSH"])
	}
}

func TestGitHubDS_GetPNG_URLSubstitution(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(fmt.Sprintf(`{"stargazers_count":1,"open_issues_count":1,"forks_count":1,"pushed_at":"%s"}`, time.Now().Format(time.RFC3339))))
	}))
	defer srv.Close()

	ds := &GitHubDS{Token: "owner/repo", URL: srv.URL + "/repos/%s"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(gotPath, "owner/repo") {
		t.Fatalf("path %q should contain owner/repo", gotPath)
	}
}

func TestGitHubDS_GetPNG_MalformedTokenNoHTTP(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ds := &GitHubDS{Token: "malformed-no-slash", URL: srv.URL + "/repos/%s"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if called {
		t.Fatal("should not have made HTTP call for malformed token")
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img == nil || len(img.Data) == 0 {
		t.Fatal("fallback empty")
	}
}

func TestGitHubDS_GetPNG_SuccessRender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(`{"stargazers_count":99,"open_issues_count":3,"forks_count":11,"pushed_at":"%s"}`, time.Now().Add(-30*time.Minute).Format(time.RFC3339))))
	}))
	defer srv.Close()

	ds := &GitHubDS{Token: "a/b", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestGitHubDS_GetPNG_404Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	ds := &GitHubDS{Token: "owner/repo", URL: srv.URL + "/%s"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG should fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestGitHubDS_GetPNG_NoAuthHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.Write([]byte(fmt.Sprintf(`{"stargazers_count":1,"open_issues_count":1,"forks_count":1,"pushed_at":"%s"}`, time.Now().Format(time.RFC3339))))
	}))
	defer srv.Close()

	ds := &GitHubDS{Token: "o/r", URL: srv.URL + "/%s"}
	_, _ = ds.GetPNG(64, 64)
	if gotKey != "" {
		t.Fatalf("X-API-Key should be empty, got %q", gotKey)
	}
}

func TestGitHubFallbackPNG(t *testing.T) {
	img := fallbackGitHub(64, 64)
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
