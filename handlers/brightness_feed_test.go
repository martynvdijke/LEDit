package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"ledit/render"
)

func avgBrightness(b []byte) float64 {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return -1
	}
	bounds := img.Bounds()
	var sum int64
	var n int64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			sum += int64(r>>8) + int64(g>>8) + int64(b>>8)
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}

func TestDimPNGBytes_ScheduledDim(t *testing.T) {
	// simulate scheduled device at 22:30 30%
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	now := mustTime("2026-08-25 22:30")
	lvl := ResolveBrightness(now, windows, nil, nil)
	if lvl != 30 {
		t.Fatalf("lvl %d", lvl)
	}
	// create white png
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{200, 200, 200, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	origAvg := avgBrightness(buf.Bytes())
	dimmed := dimPNGBytes(buf.Bytes(), lvl)
	dimAvg := avgBrightness(dimmed)
	if dimAvg >= origAvg*0.6 {
		t.Fatalf("dimmed avg %v should be ~30%% of %v", dimAvg, origAvg)
	}
	if dimAvg <= 5 {
		t.Fatalf("dimmed too dark %v", dimAvg)
	}
}

func TestDimPNGBytes_Override(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	override := 80
	lvl := ResolveBrightness(mustTime("2026-08-25 22:30"), windows, nil, &override)
	if lvl != 80 {
		t.Fatalf("got %d", lvl)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{100, 100, 100, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	dimmed := dimPNGBytes(buf.Bytes(), lvl)
	avg := avgBrightness(dimmed)
	// 80% of 100 ~80
	if avg < 75 || avg > 85 {
		t.Fatalf("override 80 avg %v", avg)
	}
}

func TestSensorStaleFallback(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	sensorLevel := 70
	// fresh sensor -> 70
	if got := ResolveBrightness(mustTime("2026-08-25 22:30"), windows, &sensorLevel, nil); got != 70 {
		t.Fatalf("fresh sensor %d", got)
	}
	// stale -> nil -> schedule 30
	if got := ResolveBrightness(mustTime("2026-08-25 22:30"), windows, nil, nil); got != 30 {
		t.Fatalf("stale fallback %d", got)
	}
}

func TestFeedWithBrightnessFn(t *testing.T) {
	// serveFeed with brightness 30 should send dimmed frame
	src := &fakeColorSource{c: color.RGBA{200, 200, 200, 255}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		sources := []sourceWithName{{Name: "White", Source: src, cacheKey: "white:1"}}
		// brightness 30 via bFn
		bFn := func() int { return 30 }
		serveFeed(conn, feedConn{}, sources, false, 80*time.Millisecond, 16, 16, &FeedController{}, "none", 500, bFn)
	}))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:]+"/", nil)
	if err != nil {
		t.Fatalf("dial %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(msg, &m)
	bs, _ := m["image"].(string)
	raw, _ := base64.StdEncoding.DecodeString(bs)
	avg := avgBrightness(raw)
	// original 200 -> dimmed 60
	if avg > 80 || avg < 40 {
		t.Fatalf("dimmed avg %v", avg)
	}
	// also test that render.Decode still works
	if _, err := render.DecodeNRGBA(raw); err != nil {
		t.Fatalf("decode %v", err)
	}
}

func TestValidateBrightnessWindows_Invalid(t *testing.T) {
	if err := ValidateBrightnessWindows([]BrightnessWindow{{Days: []int{1}, Start: "bad", End: "09:00", Level: 50}}); err == nil {
		t.Fatalf("expected invalid HH:MM error")
	}
	if err := ValidateBrightnessWindows([]BrightnessWindow{{Days: []int{1}, Start: "07:00", End: "09:00", Level: 200}}); err == nil {
		t.Fatalf("expected level out of range")
	}
	many := make([]BrightnessWindow, 17)
	for i := range many {
		many[i] = BrightnessWindow{Days: []int{1}, Start: "07:00", End: "08:00", Level: 10}
	}
	if err := ValidateBrightnessWindows(many); err == nil {
		t.Fatalf("expected 17th window error")
	}
	// valid
	if err := ValidateBrightnessWindows([]BrightnessWindow{{Days: []int{1}, Start: "07:00", End: "08:00", Level: 50}}); err != nil {
		t.Fatalf("valid unexpected %v", err)
	}
}
