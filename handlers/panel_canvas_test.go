package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/datasource"
	"ledit/render"
)

// panelPatternSource renders each column as a distinct red value so a sliced
// frame can be checked column-by-column. red == x for every pixel in column x.
type panelPatternSource struct{}

func (p *panelPatternSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		c := color.RGBA{R: uint8(x), G: 0, B: 0, A: 255}
		for y := 0; y < height; y++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

func panelTestServer(t *testing.T, fc feedConn, width, height int, src datasource.Datasource) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		sources := []sourceWithName{{Name: "Pattern", Source: src, cacheKey: "pattern:0"}}
		serveFeed(conn, fc, sources, false, 300*time.Millisecond, width, height, &FeedController{}, "none", 500, nil)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

// A single-panel device must be byte-identical to the source render.
func TestFeedSinglePanelByteIdentical(t *testing.T) {
	src := &panelPatternSource{}
	url := panelTestServer(t, feedConn{}, 32, 8, src)
	got := readOverlayFrame(t, url)
	want, err := src.GetPNG(32, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want.Data) {
		t.Fatalf("single-panel frame changed: got %d bytes, want %d", len(got), len(want.Data))
	}
}

// A two-panel chain with an 8px bezel: the logical canvas is 136 wide, the
// frame sent to the device must be the physical 128, with the gap columns
// removed and the second panel shifted left by the gap.
func TestFeedSlicesBezelGaps(t *testing.T) {
	url := panelTestServer(t, feedConn{panelCols: 2, panelGap: 8}, 128, 8, &panelPatternSource{})
	got := readOverlayFrame(t, url)
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Bounds().Dx() != 128 || img.Bounds().Dy() != 8 {
		t.Fatalf("expected physical 128x8, got %v", img.Bounds())
	}
	red := func(x int) int {
		r, _, _, _ := img.At(x, 0).RGBA()
		return int(r >> 8)
	}
	for x := 0; x < 128; x++ {
		wantX := x
		if x >= 64 {
			wantX = x + 8 // second panel starts at logical column 72
		}
		if v := red(x); v != wantX {
			t.Fatalf("column %d: got %d, want %d", x, v, wantX)
		}
	}
}

func TestAdminDeviceCreatePersistsPanels(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	form := "name=PANELTest&ip=127.0.0.1&port=6270&width=128&height=64&refresh_interval=1&enabled=on" +
		"&panel_cols=2&panel_gap=8"
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
	if d.PanelCols != 2 || d.PanelGap != 8 {
		t.Fatalf("panel spec not persisted: cols=%d gap=%d", d.PanelCols, d.PanelGap)
	}
}

func TestAdminDeviceUpdatePersistsPanels(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	d := srv.DB.DeviceSettings.Create().SetName("UpdPanel").SetWidth(128).SetHeight(32).SaveX(context.Background())
	form := "name=UpdPanel&ip=127.0.0.1&port=6270&width=96&height=32&refresh_interval=1&enabled=on&panel_cols=3&panel_gap=4"
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/devices/%d/edit", d.ID), strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	got := srv.DB.DeviceSettings.GetX(context.Background(), d.ID)
	if got.PanelCols != 3 || got.PanelGap != 4 {
		t.Fatalf("update did not persist panels: cols=%d gap=%d", got.PanelCols, got.PanelGap)
	}
}

func TestAdminDeviceCreateRejectsIndivisiblePanels(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	form := "name=BadPanel&ip=127.0.0.1&port=6270&width=100&height=64&refresh_interval=1&enabled=on" +
		"&panel_cols=3&panel_gap=4"
	req := httptest.NewRequest(http.MethodPost, "/admin/devices/new", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	n, err := srv.DB.DeviceSettings.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("indivisible panel spec created %d devices", n)
	}
}
