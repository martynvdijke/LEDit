package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
	"time"
)

func testFrame(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func decodeRGBA(t *testing.T, data []byte) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, img.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}

func TestValidateOverlaySpec(t *testing.T) {
	valid := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "hi", SpeedPx: 10, Background: "#000000", Foreground: "#ffffff"}
	if err := ValidateOverlaySpec(valid, 64); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	if err := ValidateOverlaySpec(OverlaySpec{}, 64); err != nil {
		t.Fatalf("disabled spec should be valid: %v", err)
	}
	cases := []struct {
		name string
		spec OverlaySpec
		h    int
	}{
		{"empty text", OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Background: "#000000", Foreground: "#ffffff"}, 64},
		{"bad position", OverlaySpec{Enabled: true, Position: "left", Height: 8, Text: "x", Background: "#000000", Foreground: "#ffffff"}, 64},
		{"height too small", OverlaySpec{Enabled: true, Position: "bottom", Height: 3, Text: "x", Background: "#000000", Foreground: "#ffffff"}, 64},
		{"height exceeds half", OverlaySpec{Enabled: true, Position: "bottom", Height: 40, Text: "x", Background: "#000000", Foreground: "#ffffff"}, 64},
		{"speed too high", OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "x", SpeedPx: 201, Background: "#000000", Foreground: "#ffffff"}, 64},
		{"bad bg", OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "x", Background: "red", Foreground: "#ffffff"}, 64},
		{"bad fg", OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "x", Background: "#000000", Foreground: "zzz"}, 64},
	}
	for _, tc := range cases {
		if err := ValidateOverlaySpec(tc.spec, tc.h); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestCompositeOverlayPNGDisabledByteIdentical(t *testing.T) {
	data := testFrame(t, 64, 64)
	out, err := CompositeOverlayPNG(data, DefaultOverlaySpec(), time.Now())
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatal("disabled overlay must return input bytes unchanged")
	}
	spec := OverlaySpec{Enabled: true, Text: "", Position: "bottom", Height: 8, Background: "#000000", Foreground: "#ffffff"}
	out, err = CompositeOverlayPNG(data, spec, time.Now())
	if err != nil {
		t.Fatalf("composite empty text: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatal("empty text must return input bytes unchanged")
	}
}

func TestCompositeOverlayPNGBottomStatic(t *testing.T) {
	data := testFrame(t, 64, 64)
	spec := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "HI", Background: "#000000", Foreground: "#ffffff"}
	out, err := CompositeOverlayPNG(data, spec, time.Now())
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	img := decodeRGBA(t, out)
	b := img.Bounds()
	// Top of frame untouched (white).
	if c := img.RGBAAt(0, 0); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Fatalf("top pixel changed: %+v", c)
	}
	// Strip background is black away from glyphs (last row).
	if c := img.RGBAAt(b.Max.X-1, b.Max.Y-1); c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("strip background not black: %+v", c)
	}
	// At least one foreground pixel in the strip.
	found := false
	for y := b.Max.Y - 8; y < b.Max.Y && !found; y++ {
		for x := 0; x < b.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R == 255 && c.G == 255 && c.B == 255 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no foreground pixels drawn in bottom strip")
	}
}

func TestCompositeOverlayPNGTopPosition(t *testing.T) {
	data := testFrame(t, 64, 64)
	spec := OverlaySpec{Enabled: true, Position: "top", Height: 8, Text: "HI", Background: "#000000", Foreground: "#ffffff"}
	out, err := CompositeOverlayPNG(data, spec, time.Now())
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	img := decodeRGBA(t, out)
	// Bottom untouched, top strip background black.
	if c := img.RGBAAt(0, 63); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Fatalf("bottom pixel changed: %+v", c)
	}
	if c := img.RGBAAt(63, 0); c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("top strip not drawn: %+v", c)
	}
}

func TestCompositeOverlayPNGScrollDeterministic(t *testing.T) {
	data := testFrame(t, 64, 64)
	spec := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "HELLO WORLD", SpeedPx: 20, Background: "#000000", Foreground: "#ffffff"}
	at := time.Unix(1700000000, 0)
	a, err := CompositeOverlayPNG(data, spec, at)
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	b, err := CompositeOverlayPNG(data, spec, at)
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same instant with scroll must be byte-identical (preview parity)")
	}
	c, err := CompositeOverlayPNG(data, spec, at.Add(2*time.Second))
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	if bytes.Equal(a, c) {
		t.Fatal("scroll should advance over time")
	}
}
