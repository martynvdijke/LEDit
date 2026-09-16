package render

import (
	"image"
	"testing"
)

func mkNRGBA(w, h int, fn func(x, y int) [3]byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := fn(x, y)
			si := img.PixOffset(x, y)
			img.Pix[si+0] = c[0]
			img.Pix[si+1] = c[1]
			img.Pix[si+2] = c[2]
			img.Pix[si+3] = 255
		}
	}
	return img
}

func TestFrameToPixels_Length(t *testing.T) {
	img := mkNRGBA(4, 4, func(x, y int) [3]byte { return [3]byte{10, 20, 30} })
	out := FrameToPixels(img, PixelMapConfig{Width: 4, Height: 4, ColorOrder: "RGB", Gamma: 1, OriginTop: true})
	if len(out) != 4*4*3 {
		t.Fatalf("len %d want %d", len(out), 4*4*3)
	}
}

func TestFrameToPixels_GRB(t *testing.T) {
	img := mkNRGBA(1, 1, func(x, y int) [3]byte { return [3]byte{100, 150, 200} })
	cfg := PixelMapConfig{Width: 1, Height: 1, ColorOrder: "GRB", Gamma: 1, OriginTop: true}
	out := FrameToPixels(img, cfg)
	if out[0] != 150 || out[1] != 100 || out[2] != 200 {
		t.Fatalf("GRB got %v want [150 100 200]", out)
	}
}

func TestFrameToPixels_Gamma(t *testing.T) {
	img := mkNRGBA(1, 1, func(x, y int) [3]byte { return [3]byte{128, 128, 128} })
	a := FrameToPixels(img, PixelMapConfig{Width: 1, Height: 1, Gamma: 1, OriginTop: true})
	if a[0] != 128 {
		t.Fatalf("gamma 1 got %d want 128", a[0])
	}
	b := FrameToPixels(img, PixelMapConfig{Width: 1, Height: 1, Gamma: 2.2, OriginTop: true})
	if b[0] == 128 {
		t.Fatalf("gamma 2.2 should change value, got 128")
	}
}

func TestFrameToPixels_Serpentine(t *testing.T) {
	// 3x2 image, x encodes in R, y in G
	img := mkNRGBA(3, 2, func(x, y int) [3]byte { return [3]byte{byte(x), byte(y), 0} })
	cfg := PixelMapConfig{Width: 3, Height: 2, Gamma: 1, OriginTop: true, Serpentine: true}
	out := FrameToPixels(img, cfg)
	// row0 (y=0) left->right: x 0,1,2
	if out[0] != 0 || out[3] != 1 || out[6] != 2 {
		t.Fatalf("row0 %v", out[0:9])
	}
	// row1 (y=1) reversed: x 2,1,0 => R 2,1,0
	// row1 starts at 9
	if out[9] != 2 || out[12] != 1 || out[15] != 0 {
		t.Fatalf("row1 serpentine %v", out[9:18])
	}
}

func TestFrameToPixels_OriginBottom(t *testing.T) {
	img := mkNRGBA(1, 2, func(x, y int) [3]byte { return [3]byte{byte(y * 10), 0, 0} })
	cfg := PixelMapConfig{Width: 1, Height: 2, Gamma: 1, OriginTop: false}
	out := FrameToPixels(img, cfg)
	// origin bottom: row0 should be y=1 (10), row1 y=0 (0)
	if out[0] != 10 || out[3] != 0 {
		t.Fatalf("origin bottom %v", out)
	}
}

func TestFrameToPixels_MismatchedSource(t *testing.T) {
	img := mkNRGBA(2, 2, func(x, y int) [3]byte { return [3]byte{50, 60, 70} })
	out := FrameToPixels(img, PixelMapConfig{Width: 4, Height: 4, Gamma: 1, OriginTop: true})
	if len(out) != 48 {
		t.Fatalf("len %d", len(out))
	}
}

func TestGammaLUT_Identity(t *testing.T) {
	lut := GammaLUT(1)
	for i := 0; i < 256; i++ {
		if lut[i] != byte(i) {
			t.Fatalf("identity fail at %d", i)
		}
	}
}
