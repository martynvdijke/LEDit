package datasource

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func immichThumbBytes() []byte {
	// minimal 1x1 png bytes as thumbnail payload (content doesn't need to be jpeg, we just verify echo)
	return []byte{0x01, 0x02, 0x03, 0x04, 0x05}
}

func TestParseImmichConfigDefaults(t *testing.T) {
	c := ParseImmichConfig("")
	if c.Mode != "memories" {
		t.Fatalf("mode %q want memories", c.Mode)
	}
	if c.IntervalSeconds != 300 {
		t.Fatalf("interval %d want 300", c.IntervalSeconds)
	}
	if !c.Slideshow {
		t.Fatalf("slideshow want true")
	}
	c2 := ParseImmichConfig("not json")
	if c2.Mode != "memories" || !c2.Slideshow {
		t.Fatalf("invalid json defaults failed: %+v", c2)
	}
}

func TestParseImmichConfigClampAndMode(t *testing.T) {
	c := ParseImmichConfig(`{"mode":"random","interval_seconds":10}`)
	if c.Mode != "random" {
		t.Fatalf("mode %q", c.Mode)
	}
	if c.IntervalSeconds != 30 {
		t.Fatalf("clamp %d want 30", c.IntervalSeconds)
	}
	c2 := ParseImmichConfig(`{"mode":"album","album":"abc123","interval_seconds":0}`)
	if c2.IntervalSeconds != 0 {
		t.Fatalf("interval 0 want 0 got %d", c2.IntervalSeconds)
	}
	if c2.Slideshow {
		t.Fatalf("slideshow should be false when interval 0")
	}
	// unknown fields ignored
	c3 := ParseImmichConfig(`{"mode":"memories","unknown":"field","interval_seconds":60}`)
	if c3.Mode != "memories" || c3.IntervalSeconds != 60 {
		t.Fatalf("unknown fields: %+v", c3)
	}
}

func TestImmichMemoriesSkipsVideo(t *testing.T) {
	thumb := immichThumbBytes()
	var gotKeys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "tok" {
			t.Errorf("missing x-api-key header on %s", r.URL.Path)
		}
		gotKeys = append(gotKeys, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/memories"):
			json.NewEncoder(w).Encode([]map[string]any{
				{"assets": []map[string]any{
					{"id": "vid1", "type": "VIDEO"},
					{"id": "img1", "type": "IMAGE"},
				}},
			})
		case strings.Contains(r.URL.Path, "/thumbnail"):
			w.Write(thumb)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img.Format != "JPEG" {
		t.Fatalf("format %q want JPEG", img.Format)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(img.Data))
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if !bytes.Equal(decoded, thumb) {
		t.Fatalf("thumbnail bytes mismatch")
	}
	found := false
	for _, k := range gotKeys {
		if strings.Contains(k, "/thumbnail") && strings.Contains(k, "img1") {
			found = true
		}
		if strings.Contains(k, "vid1") {
			t.Fatalf("should not request video asset thumbnail")
		}
	}
	if !found {
		t.Fatalf("thumbnail not requested for img1, keys %v", gotKeys)
	}
}

func TestImmichAlbumMode(t *testing.T) {
	thumb := immichThumbBytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "tok" {
			t.Errorf("missing x-api-key")
		}
		if strings.HasPrefix(r.URL.Path, "/api/albums/") {
			json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]any{{"id": "a1", "type": "IMAGE"}},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/thumbnail") {
			w.Write(thumb)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"album","album":"myalbum"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img.Format != "JPEG" {
		t.Fatalf("format %q", img.Format)
	}
	dec, _ := base64.StdEncoding.DecodeString(string(img.Data))
	if !bytes.Equal(dec, thumb) {
		t.Fatalf("thumb mismatch")
	}
}

func TestImmichRandomMode(t *testing.T) {
	thumb := immichThumbBytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "tok" {
			t.Errorf("missing x-api-key")
		}
		if strings.HasSuffix(r.URL.Path, "/api/assets/random") {
			json.NewEncoder(w).Encode([]map[string]any{{"id": "r1", "type": "IMAGE"}})
			return
		}
		if strings.Contains(r.URL.Path, "/thumbnail") {
			w.Write(thumb)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"random"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img.Format != "JPEG" {
		t.Fatalf("format %q", img.Format)
	}
	dec, _ := base64.StdEncoding.DecodeString(string(img.Data))
	if len(dec) == 0 {
		t.Fatalf("empty decoded thumb")
	}
}

func TestImmichHeaderPresentEveryRequest(t *testing.T) {
	thumb := immichThumbBytes()
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "tok" {
			t.Errorf("x-api-key missing on %s", r.URL.Path)
		}
		count++
		if strings.HasSuffix(r.URL.Path, "/api/memories") {
			json.NewEncoder(w).Encode([]map[string]any{{"assets": []map[string]any{{"id": "i1", "type": "IMAGE"}}}})
			return
		}
		if strings.Contains(r.URL.Path, "/thumbnail") {
			w.Write(thumb)
			return
		}
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if count < 2 {
		t.Fatalf("expected >=2 requests, got %d", count)
	}
}

func TestImmichFallbackEmptyConfig(t *testing.T) {
	ds := &ImmichDS{URL: "", Token: "", Config: ""}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback should not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("fallback decode: %v", err)
	}
	ds2 := &ImmichDS{URL: "http://example.com", Token: "", Config: ""}
	img2, err := ds2.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img2.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestImmichFallbackOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("should fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("fallback decode: %v", err)
	}
}

func TestImmichFallbackOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"random"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("should fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestImmichFallbackMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestImmichFallbackNoMemories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/memories") {
			w.Write([]byte("[]"))
			return
		}
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestImmichFallbackNoImageAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/memories") {
			json.NewEncoder(w).Encode([]map[string]any{{"assets": []map[string]any{{"id": "v1", "type": "VIDEO"}}}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestImmichCurrentStateKeys(t *testing.T) {
	thumb := immichThumbBytes()
	_ = thumb
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/memories") {
			json.NewEncoder(w).Encode([]map[string]any{
				{"assets": []map[string]any{{"id": "m1", "type": "IMAGE"}, {"id": "v1", "type": "VIDEO"}}},
				{"assets": []map[string]any{{"id": "m2", "type": "IMAGE"}}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ds := &ImmichDS{URL: srv.URL, Token: "tok", Config: `{"mode":"memories"}`}
	st, err := ds.CurrentState(nil)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if _, ok := st["photos"]; !ok {
		t.Fatalf("missing photos")
	}
	if _, ok := st["memories"]; !ok {
		t.Fatalf("missing memories")
	}
	if _, ok := st["last_asset_id"]; !ok {
		t.Fatalf("missing last_asset_id")
	}
	if st["photos"] != 2 {
		t.Fatalf("photos %v want 2", st["photos"])
	}
	if st["memories"] != 2 {
		t.Fatalf("memories %v want 2", st["memories"])
	}
	if st["last_asset_id"] != "m1" {
		t.Fatalf("last_asset_id %v want m1", st["last_asset_id"])
	}
	// failure case
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", 500)
	}))
	defer srv2.Close()
	ds2 := &ImmichDS{URL: srv2.URL, Token: "tok", Config: `{"mode":"memories"}`}
	st2, err := ds2.CurrentState(nil)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if st2["photos"] != 0 || st2["memories"] != 0 || st2["last_asset_id"] != "" {
		t.Fatalf("failure state %+v", st2)
	}
}

func TestImmichAmbient(t *testing.T) {
	ds := &ImmichDS{Config: `{"mode":"memories","interval_seconds":60}`}
	if !ds.Ambient() {
		t.Fatalf("ambient want true")
	}
	ds2 := &ImmichDS{Config: `{"mode":"memories","interval_seconds":0}`}
	if ds2.Ambient() {
		t.Fatalf("ambient want false when interval 0")
	}
	ds3 := &ImmichDS{Config: `{"mode":"memories","interval_seconds":60,"slideshow":false}`}
	if ds3.Ambient() {
		t.Fatalf("ambient want false when slideshow false")
	}
}
