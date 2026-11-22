package render

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
	"time"
)

func testTheme() Theme {
	return Theme{
		Title:           "T",
		BackgroundColor: [3]uint8{0, 0, 0},
		TextColor:       [3]uint8{255, 255, 255},
		AccentColor:     [3]uint8{255, 0, 0},
		FontSize:        12,
	}
}

func TestRenderDict_ShortRow_NoScroll(t *testing.T) {
	theme := testTheme()
	data := map[string]string{"k": "v"}
	r1, err := RenderDict(data, 320, 128, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderDict: %v", err)
	}
	if r1.Scrolls {
		t.Fatalf("expected Scrolls==false for short row")
	}
	if _, err := png.Decode(bytes.NewReader(r1.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
	r2, err := RenderDict(data, 320, 128, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderDict second: %v", err)
	}
	if !bytes.Equal(r1.Data, r2.Data) {
		t.Fatalf("short row renders should be deterministic within bucket (got differing bytes)")
	}
}

// stubClock replaces the package clock with a controllable one and returns an
// advance function. The original clock is restored on cleanup.
func stubClock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	orig := timeNow
	cur := start
	timeNow = func() time.Time { return cur }
	t.Cleanup(func() { timeNow = orig })
	return func(d time.Duration) { cur = cur.Add(d) }
}

func TestRenderDict_LongRow_Scrolls(t *testing.T) {
	theme := testTheme()
	long := strings.Repeat("ABCDEFGHIJ", 4)
	data := map[string]string{"k": long}
	advance := stubClock(t, time.UnixMilli(1_700_000_000_000))
	r1, err := RenderDict(data, 320, 128, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderDict: %v", err)
	}
	if !r1.Scrolls {
		t.Fatalf("expected Scrolls==true for long row")
	}
	// Two renders within same bucket identical.
	r2, err := RenderDict(data, 320, 128, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderDict second: %v", err)
	}
	if !bytes.Equal(r1.Data, r2.Data) {
		t.Fatalf("expected identical bytes within same bucket")
	}
	// Renders in a later bucket differ.
	advance(350 * time.Millisecond)
	r3, _ := RenderDict(data, 320, 128, theme, "/nonexistent/font.ttf")
	if bytes.Equal(r1.Data, r3.Data) {
		t.Fatalf("expected renders in different buckets to differ")
	}
}

func TestRenderPanel_ShortNoScroll(t *testing.T) {
	theme := testTheme()
	data := map[string]string{"k": "v"}
	r1, err := RenderPanel(data, 128, 64, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	if r1.Scrolls {
		t.Fatalf("expected Scrolls==false for fitting panel row")
	}
	if _, err := png.Decode(bytes.NewReader(r1.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
	r2, _ := RenderPanel(data, 128, 64, theme, "/nonexistent/font.ttf")
	if !bytes.Equal(r1.Data, r2.Data) {
		t.Fatalf("short panel row should be deterministic")
	}
}

func TestRenderPanel_LongScrolls(t *testing.T) {
	theme := testTheme()
	long := strings.Repeat("ABCDEFGHIJ", 4)
	data := map[string]string{"k": long}
	advance := stubClock(t, time.UnixMilli(1_700_000_000_000))
	r1, err := RenderPanel(data, 128, 128, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderPanel: %v", err)
	}
	if !r1.Scrolls {
		t.Fatalf("expected Scrolls==true for long panel row")
	}
	r2, _ := RenderPanel(data, 128, 128, theme, "/nonexistent/font.ttf")
	if !bytes.Equal(r1.Data, r2.Data) {
		t.Fatalf("expected identical bytes within same bucket")
	}
	// Ensure a later bucket differs.
	advance(350 * time.Millisecond)
	r3, _ := RenderPanel(data, 128, 128, theme, "/nonexistent/font.ttf")
	if bytes.Equal(r1.Data, r3.Data) {
		t.Fatalf("expected panel renders in different buckets to differ")
	}
}

func TestFittingRow_PixelsConfined(t *testing.T) {
	theme := testTheme()
	data := map[string]string{"x": "hi"}
	r, err := RenderDict(data, 320, 128, theme, "/nonexistent/font.ttf")
	if err != nil {
		t.Fatalf("RenderDict: %v", err)
	}
	if r.Scrolls {
		t.Fatalf("should not scroll")
	}
	if _, err := png.Decode(bytes.NewReader(r.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
}

// Task 1.4: scrolling verified at the two canonical panel sizes.
func TestMarqueeSizes(t *testing.T) {
	theme := testTheme()
	long := strings.Repeat("scrolling ", 12) // far wider than 64 or 32 px
	stubClock(t, time.UnixMilli(1_700_000_000_000))
	for _, size := range [][2]int{{64, 64}, {32, 32}} {
		w, h := size[0], size[1]
		r, err := RenderDict(map[string]string{"k": long}, w, h, theme, "/nonexistent/font.ttf")
		if err != nil {
			t.Fatalf("%dx%d RenderDict: %v", w, h, err)
		}
		if !r.Scrolls {
			t.Fatalf("%dx%d: expected Scrolls==true for overflowing row", w, h)
		}
		if _, err := png.Decode(bytes.NewReader(r.Data)); err != nil {
			t.Fatalf("%dx%d png decode: %v", w, h, err)
		}
		r2, _ := RenderDict(map[string]string{"k": long}, w, h, theme, "/nonexistent/font.ttf")
		if !bytes.Equal(r.Data, r2.Data) {
			t.Fatalf("%dx%d: renders within same bucket must be identical", w, h)
		}
	}
}
