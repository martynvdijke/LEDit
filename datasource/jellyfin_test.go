package datasource

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJellyfinHeaderPlacement(t *testing.T) {
	var gotEmby, gotAPIKey string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmby = r.Header.Get("X-Emby-Token")
		gotAPIKey = r.Header.Get("X-API-Key")
		gotPath = r.URL.Path
		w.Write([]byte(`[{"UserName":"alice","NowPlayingItem":{"Name":"Movie"},"PlayState":{"PositionTicks":0,"IsPaused":false}}]`))
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "secret123", URL: srv.URL}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if gotEmby != "secret123" {
		t.Fatalf("X-Emby-Token = %q want secret123", gotEmby)
	}
	if gotAPIKey != "" {
		t.Fatalf("X-API-Key should be empty, got %q", gotAPIKey)
	}
	if gotPath != "/Sessions" {
		t.Fatalf("path = %q want /Sessions", gotPath)
	}
}

func TestJellyfinTrailingSlashNormalized(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"UserName":"bob","NowPlayingItem":{"Name":"Film"},"PlayState":{"PositionTicks":0,"IsPaused":false}}]`))
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "tok", URL: srv.URL + "/"}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if gotPath != "/Sessions" {
		t.Fatalf("path = %q want /Sessions (trailing slash not normalized)", gotPath)
	}
	if strings.Contains(gotPath, "//Sessions") {
		t.Fatalf("path contains double slash: %q", gotPath)
	}
}

func TestJellyfinActiveSessionRender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"UserName":"alice","NowPlayingItem":{"Name":"Interstellar","RunTimeTicks":10000000},"PlayState":{"PositionTicks":5000000,"IsPaused":false}}]`))
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestJellyfinPaused(t *testing.T) {
	body := []byte(`[{"UserName":"alice","NowPlayingItem":{"Name":"Movie","RunTimeTicks":10000000},"PlayState":{"PositionTicks":5000000,"IsPaused":true}}]`)
	rows, err := BuildJellyfinRows(body)
	if err != nil {
		t.Fatalf("BuildJellyfinRows: %v", err)
	}
	found := false
	for _, r := range rows {
		if r[0] == "STATUS" && r[1] == "PAUSED" {
			found = true
		}
		if r[0] == "PROGRESS" {
			t.Fatalf("should not have PROGRESS when paused, got %v", rows)
		}
	}
	if !found {
		t.Fatalf("expected PAUSED row, got %v", rows)
	}
}

func TestJellyfinSkipNonPlaying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"UserName":"idle","PlayState":{"PositionTicks":0,"IsPaused":false}},{"UserName":"bob","NowPlayingItem":{"Name":"ActiveMovie"},"PlayState":{"PositionTicks":0,"IsPaused":false}}]`))
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Verify via BuildJellyfinRows that first playing is bob's
	body := []byte(`[{"UserName":"idle","PlayState":{"PositionTicks":0}},{"UserName":"bob","NowPlayingItem":{"Name":"ActiveMovie"},"PlayState":{"PositionTicks":0}}]`)
	rows, err := BuildJellyfinRows(body)
	if err != nil {
		t.Fatalf("BuildJellyfinRows: %v", err)
	}
	if rows[1][1] != "bob" {
		t.Fatalf("USER row = %q want bob, rows=%v", rows[1][1], rows)
	}
}

func TestJellyfinEmptySessionsFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG empty should fallback not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestJellyfin401Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	ds := &JellyfinDS{Token: "bad", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG 401 should fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
