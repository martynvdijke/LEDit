package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
)

func colAt(x int) color.RGBA {
	return color.RGBA{R: uint8(x % 256), G: uint8((x * 3) % 256), B: uint8((x * 7) % 256), A: 255}
}

func logicalCanvas(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		c := colAt(x)
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode logical canvas: %v", err)
	}
	return buf.Bytes()
}

func decodeCanvas(t *testing.T, data []byte) *image.RGBA {
	t.Helper()
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rgba := image.NewRGBA(src.Bounds())
	draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Src)
	return rgba
}

func TestPanelLogicalWidth(t *testing.T) {
	cases := []struct {
		width, cols, gap, want int
	}{
		{128, 1, 8, 128},
		{128, 2, 0, 128},
		{128, 2, 8, 136},
		{192, 3, 4, 200},
		{64, 0, 4, 64},
	}
	for _, c := range cases {
		if got := PanelLogicalWidth(c.width, c.cols, c.gap); got != c.want {
			t.Errorf("PanelLogicalWidth(%d,%d,%d)=%d want %d", c.width, c.cols, c.gap, got, c.want)
		}
	}
}

func TestValidatePanelSpec(t *testing.T) {
	valid := []struct{ cols, gap, w, h int }{
		{1, 0, 64, 32},
		{1, 64, 64, 32},
		{2, 8, 128, 32},
		{3, 4, 192, 64},
	}
	for _, c := range valid {
		if err := ValidatePanelSpec(c.cols, c.gap, c.w, c.h); err != nil {
			t.Errorf("ValidatePanelSpec(%d,%d,%d,%d) unexpected: %v", c.cols, c.gap, c.w, c.h, err)
		}
	}
	invalid := []struct{ cols, gap, w, h int }{
		{0, 0, 64, 32},
		{17, 0, 64, 32},
		{2, -1, 64, 32},
		{2, 65, 64, 32},
		{2, 0, 65, 32},
		{64, 0, 32, 32},
		{1, 0, 64, 0},
	}
	for _, c := range invalid {
		if err := ValidatePanelSpec(c.cols, c.gap, c.w, c.h); err == nil {
			t.Errorf("ValidatePanelSpec(%d,%d,%d,%d) should be invalid", c.cols, c.gap, c.w, c.h)
		}
	}
}

func TestSlicePanelGapsPNGNoop(t *testing.T) {
	logical := logicalCanvas(t, 128, 16)
	for _, c := range []struct{ cols, gap int }{{1, 8}, {2, 0}, {0, 8}} {
		out, err := SlicePanelGapsPNG(logical, c.cols, c.gap)
		if err != nil {
			t.Fatalf("noop %+v: %v", c, err)
		}
		if !bytes.Equal(out, logical) {
			t.Errorf("noop %+v changed bytes", c)
		}
	}
}

func TestSlicePanelGapsPNGTwoPanels(t *testing.T) {
	// physical width 128 = 2 panels of 64; gap 8 -> logical 136.
	logical := logicalCanvas(t, 136, 16)
	out, err := SlicePanelGapsPNG(logical, 2, 8)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	img := decodeCanvas(t, out)
	if img.Bounds().Dx() != 128 || img.Bounds().Dy() != 16 {
		t.Fatalf("dims = %dx%d want 128x16", img.Bounds().Dx(), img.Bounds().Dy())
	}
	for x := 0; x < 128; x++ {
		srcX := x
		if x >= 64 {
			srcX = x + 8
		}
		if got, want := img.RGBAAt(x, 0), colAt(srcX); got != want {
			t.Fatalf("out col %d = %v want source col %d = %v", x, got, srcX, want)
		}
	}
	// A gap column must not appear anywhere in the output.
	gapCol := colAt(64)
	for x := 0; x < 128; x++ {
		if img.RGBAAt(x, 0) == gapCol {
			t.Fatalf("gap column leaked at output x=%d", x)
		}
	}
}

func TestSlicePanelGapsPNGFourPanels(t *testing.T) {
	// 4 panels of 10 = 40 physical, gap 2 -> logical 46.
	const panelW, gap, cols = 10, 2, 4
	logical := logicalCanvas(t, cols*panelW+(cols-1)*gap, 8)
	out, err := SlicePanelGapsPNG(logical, cols, gap)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	img := decodeCanvas(t, out)
	if img.Bounds().Dx() != cols*panelW || img.Bounds().Dy() != 8 {
		t.Fatalf("dims = %dx%d want %dx8", img.Bounds().Dx(), img.Bounds().Dy(), cols*panelW)
	}
	for x := 0; x < cols*panelW; x++ {
		c := x / panelW
		inPanel := x % panelW
		want := colAt(c*panelW + c*gap + inPanel)
		if got := img.RGBAAt(x, 0); got != want {
			t.Fatalf("out col %d = %v want %v", x, got, want)
		}
	}
}

func TestSlicePanelGapsPNGDecodeError(t *testing.T) {
	if _, err := SlicePanelGapsPNG([]byte("not a png"), 2, 4); err == nil {
		t.Fatal("expected decode error")
	}
}
