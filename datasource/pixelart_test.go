package datasource

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func framesJSON(palette []string, frames [][]int, durations []int) string {
	type f struct {
		Duration int   `json:"duration"`
		Pixels   []int `json:"pixels"`
	}
	doc := map[string]any{
		"palette": palette,
		"frames":  []f{},
	}
	var fs []f
	for i, pix := range frames {
		dur := 100
		if i < len(durations) {
			dur = durations[i]
		}
		fs = append(fs, f{Duration: dur, Pixels: pix})
	}
	doc["frames"] = fs
	b, _ := json.Marshal(doc)
	return string(b)
}

func TestPixelArtPlaybackOrder(t *testing.T) {
	// 3 frames with durations 100, 200, 300
	palette := []string{"#000000", "#ff0000", "#00ff00"}
	frames := [][]int{{0, 1}, {0, 2}, {1, 2}}
	durs := []int{100, 200, 300}
	raw := framesJSON(palette, frames, durs)
	ds := &PixelArtDS{GridWidth: 1, GridHeight: 2, FramesJSON: raw}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// At start -> frame 0
	if got := ds.NextFrame(start); got != 0 {
		t.Fatalf("at 0ms want 0 got %d", got)
	}
	// 50ms still 0
	if got := ds.NextFrame(start.Add(50 * time.Millisecond)); got != 0 {
		t.Fatalf("at 50ms want 0 got %d", got)
	}
	// 150ms -> frame 1 (0 duration 100 elapsed)
	if got := ds.NextFrame(start.Add(150 * time.Millisecond)); got != 1 {
		t.Fatalf("at 150ms want 1 got %d", got)
	}
	// 350ms -> frame 2 (100+200=300, so 350 in frame2)
	if got := ds.NextFrame(start.Add(350 * time.Millisecond)); got != 2 {
		t.Fatalf("at 350ms want 2 got %d", got)
	}
	// 650ms -> looped (total 600, 650%600=50 -> frame0)
	if got := ds.NextFrame(start.Add(650 * time.Millisecond)); got != 0 {
		t.Fatalf("at 650ms want 0 got %d", got)
	}
	if ds.FrameCount() != 3 {
		t.Fatalf("FrameCount want 3 got %d", ds.FrameCount())
	}
}

func TestPixelArtRateLimitedFetching(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Write([]byte(`{"main":{"temp":10}}`))
	}))
	defer srv.Close()
	palette := []string{"#000000", "#ffffff"}
	raw := framesJSON(palette, [][]int{{0}}, []int{100})
	ds := &PixelArtDS{
		GridWidth: 1, GridHeight: 1, FramesJSON: raw,
		BindingsJSON: `{}`,
		APIURL:       srv.URL, MinRefresh: 30 * time.Second,
	}
	for i := 0; i < 5; i++ {
		if _, err := ds.GetPNG(64, 64); err != nil {
			t.Fatalf("GetPNG %d: %v", i, err)
		}
	}
	if count.Load() != 1 {
		t.Fatalf("request count = %d, want 1 (rate limited)", count.Load())
	}
	// After forcing expiry, next fetch should happen.
	ds.mu.Lock()
	ds.lastFetch = time.Now().Add(-31 * time.Second)
	ds.mu.Unlock()
	if _, err := ds.GetPNG(64, 64); err != nil {
		t.Fatalf("GetPNG after expiry: %v", err)
	}
	if count.Load() != 2 {
		t.Fatalf("after expiry count = %d, want 2", count.Load())
	}
}

func TestPixelArtSlotBandResolution(t *testing.T) {
	// palette: index0 black, 1 white, 2 slot gauge, 3 blue (cold), 4 green (mid), 5 red (hot)
	palette := []string{"#000000", "#ffffff", "@gauge", "#0000ff", "#00ff00", "#ff0000"}
	// grid 2x1: pixel0 = slot index 2, pixel1 = white 1
	raw := framesJSON(palette, [][]int{{2, 1}}, []int{100})
	bindings := `{"colorSlots":[{"slot":"gauge","path":"main.temp","bands":[{"max":0,"colorIndex":3},{"max":15,"colorIndex":4},{"colorIndex":5}]}]}`
	// temp=10 -> should pick index 4 (green)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"main":{"temp":10}}`))
	}))
	defer srv.Close()
	ds := &PixelArtDS{
		GridWidth: 2, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: srv.URL, MinRefresh: time.Hour,
	}
	img, err := ds.GetPNG(100, 100)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	// Scale = min(100/2,100/1)=50, offX=0 offY=25, pixel0 at 0,25 size 50
	// Sample center of first cell.
	r, g, b, _ := decoded.At(25, 50).RGBA()
	// green 00ff00 -> normalized
	if r>>8 != 0x00 || g>>8 != 0xff || b>>8 != 0x00 {
		t.Fatalf("slot band for temp 10: want green got %02x%02x%02x", r>>8, g>>8, b>>8)
	}
	// temp 20 -> catch-all red
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"main":{"temp":20}}`))
	}))
	defer srv2.Close()
	ds2 := &PixelArtDS{
		GridWidth: 2, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: srv2.URL, MinRefresh: time.Hour,
	}
	img2, _ := ds2.GetPNG(100, 100)
	dec2, _ := png.Decode(bytes.NewReader(img2.Data))
	r2, g2, b2, _ := dec2.At(25, 50).RGBA()
	if r2>>8 != 0xff || g2>>8 != 0x00 || b2>>8 != 0x00 {
		t.Fatalf("slot band for temp 20: want red got %02x%02x%02x", r2>>8, g2>>8, b2>>8)
	}
	// temp -5 -> blue
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"main":{"temp":-5}}`))
	}))
	defer srv3.Close()
	ds3 := &PixelArtDS{
		GridWidth: 2, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: srv3.URL, MinRefresh: time.Hour,
	}
	img3, _ := ds3.GetPNG(100, 100)
	dec3, _ := png.Decode(bytes.NewReader(img3.Data))
	r3, g3, b3, _ := dec3.At(25, 50).RGBA()
	if r3>>8 != 0x00 || g3>>8 != 0x00 || b3>>8 != 0xff {
		t.Fatalf("slot band for temp -5: want blue got %02x%02x%02x", r3>>8, g3>>8, b3>>8)
	}
}

func TestPixelArtFrameRuleRestricts(t *testing.T) {
	palette := []string{"#000000", "#ffffff"}
	frames := [][]int{{0}, {1}, {0}}
	raw := framesJSON(palette, frames, []int{100, 100, 100})
	// rule: temp >=20 -> only frame 2
	bindings := `{"frameRules":[{"path":"main.temp","min":20,"frameIndices":[2]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"main":{"temp":25}}`))
	}))
	defer srv.Close()
	ds := &PixelArtDS{
		GridWidth: 1, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: srv.URL, MinRefresh: time.Hour,
	}
	// Trigger fetch via GetPNG
	if _, err := ds.GetPNG(64, 64); err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if ds.FrameCount() != 1 {
		t.Fatalf("FrameCount with rule want 1 got %d", ds.FrameCount())
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Reset startTime for deterministic test
	ds.mu.Lock()
	ds.startTime = time.Time{}
	ds.mu.Unlock()
	if got := ds.NextFrame(start); got != 2 {
		t.Fatalf("NextFrame with rule want 2 got %d", got)
	}
	if got := ds.NextFrame(start.Add(500 * time.Millisecond)); got != 2 {
		t.Fatalf("NextFrame loop with single allowed want 2 got %d", got)
	}
	// No matching rule -> all frames
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"main":{"temp":10}}`))
	}))
	defer srv2.Close()
	ds2 := &PixelArtDS{
		GridWidth: 1, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: srv2.URL, MinRefresh: time.Hour,
	}
	if _, err := ds2.GetPNG(64, 64); err != nil {
		t.Fatalf("GetPNG2: %v", err)
	}
	if ds2.FrameCount() != 3 {
		t.Fatalf("FrameCount without match want 3 got %d", ds2.FrameCount())
	}
}

func TestPixelArtOverlayRenders(t *testing.T) {
	palette := []string{"#000000", "#ffffff"}
	raw := framesJSON(palette, [][]int{{0}}, []int{100})
	bindings := `{"overlays":[{"path":"main.temp","x":2,"y":50,"color":"#ffffff","fontSize":12,"format":"%.1f°C"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"main":{"temp":21.5}}`))
	}))
	defer srv.Close()
	ds := &PixelArtDS{
		GridWidth: 1, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: srv.URL, MinRefresh: time.Hour,
	}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("overlay PNG decode failed: %v", err)
	}
}

func TestPixelArtFallbackPaths(t *testing.T) {
	palette := []string{"#000000", "#ffffff", "@gauge"}
	raw := framesJSON(palette, [][]int{{2}}, []int{100})
	bindings := `{"colorSlots":[{"slot":"gauge","path":"main.temp","bands":[{"max":0,"colorIndex":0},{"colorIndex":1}]}]}`
	// Bad URL -> should fallback to authored colors and still produce PNG
	ds := &PixelArtDS{
		GridWidth: 1, GridHeight: 1, FramesJSON: raw, BindingsJSON: bindings,
		APIURL: "http://127.0.0.1:1", MinRefresh: time.Hour,
	}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG bad URL should not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode fallback: %v", err)
	}
	// Empty frames -> placeholder
	ds2 := &PixelArtDS{GridWidth: 2, GridHeight: 2, FramesJSON: ``}
	img2, err := ds2.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("placeholder GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img2.Data)); err != nil {
		t.Fatalf("placeholder decode: %v", err)
	}
	// Also empty frames with invalid JSON
	ds3 := &PixelArtDS{GridWidth: 1, GridHeight: 1, FramesJSON: `{"palette":[],"frames":[]}`}
	img3, err := ds3.GetPNG(32, 32)
	if err != nil {
		t.Fatalf("empty frames placeholder: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img3.Data)); err != nil {
		t.Fatalf("placeholder decode 2: %v", err)
	}
	_ = img2
	_ = img3
}
