package datasource

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"ledit/render"
)

func TestClockDS_GetPNG_NonNil(t *testing.T) {
	ds := &ClockDS{}
	start := time.Now()
	img, err := ds.GetPNG(64, 64)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("GetPNG error: %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
	if img.Format != "PNG" {
		t.Fatalf("Format=%q want PNG", img.Format)
	}
	if len(img.Data) == 0 {
		t.Fatal("empty data")
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("GetPNG took %v > 100ms (possible network)", elapsed)
	}
}

func TestClockDS_NoNetwork(t *testing.T) {
	ds := &ClockDS{}
	start := time.Now()
	_, err := ds.GetPNG(32, 32)
	if err != nil {
		t.Fatalf("GetPNG error: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("too slow")
	}
}

func TestRenderClock_OutputChanges(t *testing.T) {
	theme := DefaultTheme()
	t1 := time.Date(2026, 8, 23, 10, 30, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 23, 11, 30, 0, 0, time.UTC)
	a, err := render.RenderClock(t1, 64, 64, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderClock t1: %v", err)
	}
	b, err := render.RenderClock(t2, 64, 64, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderClock t2: %v", err)
	}
	if bytes.Equal(a.Data, b.Data) {
		t.Fatal("expected different bytes for different times")
	}
}

func TestRenderClock_SizeScaling(t *testing.T) {
	theme := DefaultTheme()
	now := time.Date(2026, 8, 23, 12, 34, 0, 0, time.UTC)
	a, err := render.RenderClock(now, 64, 64, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("64x64: %v", err)
	}
	b, err := render.RenderClock(now, 128, 32, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("128x32: %v", err)
	}
	imgA, err := png.Decode(bytes.NewReader(a.Data))
	if err != nil {
		t.Fatalf("decode A: %v", err)
	}
	imgB, err := png.Decode(bytes.NewReader(b.Data))
	if err != nil {
		t.Fatalf("decode B: %v", err)
	}
	if imgA.Bounds().Dx() != 64 || imgA.Bounds().Dy() != 64 {
		t.Fatalf("A bounds %v want 64x64", imgA.Bounds())
	}
	if imgB.Bounds().Dx() != 128 || imgB.Bounds().Dy() != 32 {
		t.Fatalf("B bounds %v want 128x32", imgB.Bounds())
	}
	if bytes.Equal(a.Data, b.Data) {
		t.Fatal("different sizes should differ")
	}
	// Ensure completion <100ms for each
	start := time.Now()
	_, _ = render.RenderClock(now, 64, 64, theme, "fonts/PixelifySans.ttf")
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("too slow")
	}
}
