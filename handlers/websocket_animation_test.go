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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/render"
)

// fakeAnimatedSource implements datasource.Datasource + datasource.Animator
type fakeAnimatedSource struct {
	mu       sync.Mutex
	start    time.Time
	frameDur time.Duration
	count    int
	failNext bool
	calls    int
}

func newFakeAnimatedSource(dur time.Duration, count int) *fakeAnimatedSource {
	return &fakeAnimatedSource{start: time.Now(), frameDur: dur, count: count}
}
func (f *fakeAnimatedSource) FrameCount() int { return f.count }
func (f *fakeAnimatedSource) NextFrame(now time.Time) int {
	if f.count <= 1 {
		return 0
	}
	f.mu.Lock()
	s := f.start
	d := f.frameDur
	f.mu.Unlock()
	if s.IsZero() {
		return 0
	}
	elapsed := now.Sub(s)
	if elapsed < 0 {
		return 0
	}
	idx := int(elapsed/d) % f.count
	return idx
}
func (f *fakeAnimatedSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	f.mu.Lock()
	fail := f.failNext
	f.mu.Unlock()
	// no failure by default; test can toggle
	if fail {
		return nil, errFake
	}
	idx := f.NextFrame(time.Now())
	// alternate colors
	c := color.RGBA{255, 0, 0, 255}
	if idx == 1 {
		c = color.RGBA{0, 0, 255, 255}
	}
	if width <= 0 {
		width = 32
	}
	if height <= 0 {
		height = 32
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

var errFake = errFakeSentinel()

func errFakeSentinel() error { return &fakeErr{} }

type fakeErr struct{}

func (e *fakeErr) Error() string { return "fake render error" }

type fakeStaticSource struct{}

func (f *fakeStaticSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

// helper: serveFeed via ws upgrade with injected sources
func animTestServer(t *testing.T, sources []sourceWithName, timeout time.Duration, fc *FeedController) (*httptest.Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serveFeed(conn, feedConn{}, sources, false, timeout, 32, 32, fc, "none", 500, nil)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func TestAnimationMultipleMessagesInSlot(t *testing.T) {
	anim := newFakeAnimatedSource(100*time.Millisecond, 2)
	sources := []sourceWithName{
		{Name: "Anim", Source: anim, cacheKey: "anim:0"},
		{Name: "Static", Source: &fakeStaticSource{}, cacheKey: "static:0"},
	}
	fc := &FeedController{}
	timeout := 800 * time.Millisecond
	_, url := animTestServer(t, sources, timeout, fc)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Collect messages for ~700ms — should see multiple Anim frames before Static.
	var animMsgs []map[string]any
	deadline := time.Now().Add(700 * time.Millisecond)
	conn.SetReadDeadline(deadline.Add(1 * time.Second))
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// Only count Anim source messages
		if msg["source"] == "Anim" {
			// Validate shape
			for _, k := range []string{"format", "image", "source", "next"} {
				if _, ok := msg[k]; !ok {
					t.Fatalf("missing key %s in %v", k, msg)
				}
			}
			if s, _ := msg["format"].(string); s != "PNG" {
				t.Fatalf("format != PNG: %v", msg["format"])
			}
			bs, _ := msg["image"].(string)
			if _, err := base64.StdEncoding.DecodeString(bs); err != nil {
				t.Fatalf("image not base64: %v", err)
			}
			animMsgs = append(animMsgs, msg)
			if len(animMsgs) >= 3 {
				break
			}
		}
	}
	if len(animMsgs) < 2 {
		t.Fatalf("expected >=2 Anim messages within one slot, got %d", len(animMsgs))
	}
	// Images should differ across frames (colors alternate)
	if len(animMsgs) >= 2 {
		a := animMsgs[0]["image"].(string)
		b := animMsgs[1]["image"].(string)
		if a == b {
			t.Fatalf("expected distinct frame images, got identical")
		}
	}
}

func TestAnimationSkipInterruptsPromptly(t *testing.T) {
	anim := newFakeAnimatedSource(100*time.Millisecond, 2)
	sources := []sourceWithName{
		{Name: "Anim", Source: anim, cacheKey: "anim:0"},
		{Name: "Static", Source: &fakeStaticSource{}, cacheKey: "static:0"},
	}
	fc := &FeedController{}
	timeout := 2000 * time.Millisecond
	_, url := animTestServer(t, sources, timeout, fc)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Wait for first Anim frame
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	var first map[string]any
	_ = json.Unmarshal(data, &first)
	if first["source"] != "Anim" {
		t.Fatalf("first source expected Anim got %v", first["source"])
	}
	// Send next to skip
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"next"}`)); err != nil {
		t.Fatalf("write next: %v", err)
	}
	// Next message should be Static and arrive well before full timeout (within 500ms)
	conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	start := time.Now()
	_, data2, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read after next: %v", err)
	}
	elapsed := time.Since(start)
	var second map[string]any
	_ = json.Unmarshal(data2, &second)
	// Due to animation ticks, there may be an extra Anim frame before skip is observed (up to 50ms).
	// Loop until we see Static or timeout.
	if second["source"] != "Static" {
		// try one more read
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, data3, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read static: %v", err)
		}
		_ = json.Unmarshal(data3, &second)
		elapsed = time.Since(start)
	}
	if second["source"] != "Static" {
		t.Fatalf("expected Static after next, got %v", second["source"])
	}
	if elapsed > 700*time.Millisecond {
		t.Fatalf("skip not prompt: took %v", elapsed)
	}
	_ = http.StatusOK // keep import
}
