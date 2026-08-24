package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/render"
)

type fakeColorSource struct {
	c color.RGBA
}

func (f *fakeColorSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	if width <= 0 {
		width = 32
	}
	if height <= 0 {
		height = 32
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, f.c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

func transitionTestServer(t *testing.T, sources []sourceWithName, timeout time.Duration, fc *FeedController, style string, ms int) (*httptest.Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serveFeed(conn, feedConn{}, sources, false, timeout, 32, 32, fc, style, ms, nil)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func TestTransitionFadeRamp(t *testing.T) {
	sources := []sourceWithName{
		{Name: "Red", Source: &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}, cacheKey: "fade_red:0"},
		{Name: "Blue", Source: &fakeColorSource{c: color.RGBA{0, 0, 255, 255}}, cacheKey: "fade_blue:0"},
	}
	fc := &FeedController{}
	timeout := 300 * time.Millisecond
	// 400ms => steps 10 => 9 ramp frames per boundary
	_, url := transitionTestServer(t, sources, timeout, fc, "fade", 400)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Collect messages across >=2 boundaries (need ~2*timeout + ramp)
	deadline := time.Now().Add(2000 * time.Millisecond)
	conn.SetReadDeadline(deadline.Add(2 * time.Second))
	var msgs []map[string]any
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// Only consider image messages (have "image")
		if _, ok := m["image"]; !ok {
			continue
		}
		msgs = append(msgs, m)
		if len(msgs) >= 25 {
			break
		}
	}
	if len(msgs) < 4 {
		t.Fatalf("expected many msgs with ramp, got %d", len(msgs))
	}
	// First message is Red without ramp (no prev), second boundary should have ramp Blue etc.
	// Count ramp frames: between canonical frames, source == incoming name
	// We expect at least steps-1 extra per boundary after first.
	// For timeout 600ms we give enough time; check ramp messages are decodable and have incoming source.
	for i, m := range msgs {
		bs, _ := m["image"].(string)
		if _, err := base64.StdEncoding.DecodeString(bs); err != nil {
			t.Fatalf("msg %d image not base64: %v", i, err)
		}
		// also decode PNG
		raw, _ := base64.StdEncoding.DecodeString(bs)
		if _, err := render.DecodeNRGBA(raw); err != nil {
			t.Fatalf("msg %d not decodable PNG: %v", i, err)
		}
		if _, ok := m["source"]; !ok {
			t.Fatalf("msg %d missing source", i)
		}
	}
	// Verify ordering: first msg Red, then ramp msgs should be Blue until next boundary
	// So check that between first Red and next Red there exists Blue messages (ramp+canonical)
	// Simple: second distinct source should appear multiple times consecutively due to ramp.
	// Count consecutive Blue after first Red.
	foundRamp := false
	for i := 1; i < len(msgs); i++ {
		if msgs[i]["source"] == "Blue" && msgs[i-1]["source"] == "Red" {
			// boundary Red->Blue should have ramp: expect next few msgs Blue
			// Actually after Red, the ramp frames are Blue-labeled, then canonical Blue
			// So if we see Red->Blue transition, ramp is at work
			foundRamp = true
			break
		}
	}
	if !foundRamp {
		t.Fatalf("no Red->Blue boundary found in msgs: %v", msgs)
	}
	// Ensure ramp source equals incoming: all ramp messages source == Blue at first boundary
	// Find first Red index then count Blues until next Red
	firstRed := -1
	for i, m := range msgs {
		if m["source"] == "Red" {
			firstRed = i
			break
		}
	}
	if firstRed >= 0 {
		// count Blues after firstRed until next Red
		blueRun := 0
		for i := firstRed + 1; i < len(msgs); i++ {
			if msgs[i]["source"] == "Blue" {
				blueRun++
			} else if msgs[i]["source"] == "Red" {
				break
			}
		}
		// steps 10 => 9 ramp + 1 canonical = 10 Blues per Blue slot
		// Allow tolerance
		if blueRun < 6 {
			t.Fatalf("expected ramp Blues run >=6, got %d msgs=%v", blueRun, msgs)
		}
	}
}

func TestTransitionNoneNoExtras(t *testing.T) {
	sources := []sourceWithName{
		{Name: "Red", Source: &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}, cacheKey: "red:0"},
		{Name: "Blue", Source: &fakeColorSource{c: color.RGBA{0, 0, 255, 255}}, cacheKey: "blue:0"},
	}
	fc := &FeedController{}
	timeout := 200 * time.Millisecond
	_, url := transitionTestServer(t, sources, timeout, fc, "none", 500)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Collect for ~800ms: expect 4 slots (alternating) with exactly 1 per slot
	deadline := time.Now().Add(800 * time.Millisecond)
	conn.SetReadDeadline(deadline.Add(2 * time.Second))
	var msgs []map[string]any
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if _, ok := m["image"]; !ok {
			continue
		}
		msgs = append(msgs, m)
	}
	// With 200ms timeout over 800ms we expect ~4 messages (one per slot)
	if len(msgs) < 3 || len(msgs) > 5 {
		t.Fatalf("none style: expected ~4 msgs, got %d %v", len(msgs), msgs)
	}
	// Alternating sources
	for i := 1; i < len(msgs); i++ {
		if msgs[i]["source"] == msgs[i-1]["source"] {
			t.Fatalf("none style: consecutive same source at %d: %v", i, msgs)
		}
	}
}
