package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/render"
)

func overlayTestServer(t *testing.T, fc feedConn) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		sources := []sourceWithName{{Name: "Static", Source: &fakeStaticSource{}, cacheKey: "static:0"}}
		serveFeed(conn, fc, sources, false, 300*time.Millisecond, 32, 32, &FeedController{}, "none", 500, nil)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func readOverlayFrame(t *testing.T, url string) []byte {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b64, _ := msg["image"].(string)
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	return decoded
}

func TestOverlayAppliedToFeedFrame(t *testing.T) {
	spec := render.OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "HI", SpeedPx: 0, Background: "#ff0000", Foreground: "#00ff00"}
	url := overlayTestServer(t, feedConn{overlay: spec})
	raw := readOverlayFrame(t, url)

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	// Bottom-left is strip background (red) below the glyph rows.
	r, g, bl, _ := img.At(b.Min.X, b.Max.Y-1).RGBA()
	if r>>8 != 255 || g>>8 != 0 || bl>>8 != 0 {
		t.Fatalf("bottom strip background = %d,%d,%d, want red", r>>8, g>>8, bl>>8)
	}
	// Top row remains the source's black.
	r2, g2, b2, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	if r2>>8 != 0 || g2>>8 != 0 || b2>>8 != 0 {
		t.Fatalf("top pixel changed to %d,%d,%d", r2>>8, g2>>8, b2>>8)
	}
}

func TestOverlayDisabledFrameUnchanged(t *testing.T) {
	url := overlayTestServer(t, feedConn{})
	raw := readOverlayFrame(t, url)
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// fakeStaticSource renders an all-zero frame; no strip background present.
	r, g, b, _ := img.At(0, 31).RGBA()
	if r != 0 || g != 0 || b != 0 {
		t.Fatalf("disabled overlay should not draw a strip: %d,%d,%d", r>>8, g>>8, b>>8)
	}
	if img.Bounds().Dx() != 32 {
		t.Fatalf("unexpected frame size %v", img.Bounds())
	}
}

func TestOverlayPersistenceRoundTrip(t *testing.T) {
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(openMemDB(t))))
	defer client.Close()
	ctx := context.Background()
	ds := client.DeviceSettings.Create().
		SetName("overlay").
		SetOverlayEnabled(true).
		SetOverlayPosition("top").
		SetOverlayHeight(6).
		SetOverlayText("hello").
		SetOverlaySpeedPx(25).
		SetOverlayBg("#112233").
		SetOverlayFg("#445566").
		SaveX(ctx)
	if !ds.OverlayEnabled || ds.OverlayPosition != "top" || ds.OverlayHeight != 6 || ds.OverlayText != "hello" || ds.OverlaySpeedPx != 25 {
		t.Fatalf("round trip mismatch: %+v", ds)
	}
	if ds.OverlayBg != "#112233" || ds.OverlayFg != "#445566" {
		t.Fatalf("colors mismatch: %s %s", ds.OverlayBg, ds.OverlayFg)
	}
}

// TestAdminDeviceCreatePersistsOverlay drives the real create handler from the
// admin form and asserts the overlay columns were persisted.
func TestAdminDeviceCreatePersistsOverlay(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	form := "name=OVLTest&ip=127.0.0.1&port=6270&width=64&height=64&refresh_interval=1&enabled=on" +
		"&overlay_enabled=on&overlay_text=OVL&overlay_position=top&overlay_height=10" +
		"&overlay_speed_px=25&overlay_bg=%23ff0000&overlay_fg=%23ffffff"
	req := httptest.NewRequest(http.MethodPost, "/admin/devices/new", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	d, err := srv.DB.DeviceSettings.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !d.OverlayEnabled || d.OverlayText != "OVL" || d.OverlayPosition != "top" ||
		d.OverlayHeight != 10 || d.OverlaySpeedPx != 25 {
		t.Fatalf("overlay not persisted: %+v", d)
	}
	if d.OverlayBg != "#ff0000" || d.OverlayFg != "#ffffff" {
		t.Fatalf("overlay colors not persisted: %s %s", d.OverlayBg, d.OverlayFg)
	}
}
