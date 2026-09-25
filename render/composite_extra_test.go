package render

import (
	"bytes"
	"image"
	"testing"
)

func TestScaleBilinearDims(t *testing.T) {
	src := solidNRGBA(2, 2, struct{ R, G, B, A uint8 }{10, 20, 30, 255})
	// workaround: use helper
	s := mkNRGBA(2, 2, func(x, y int) [3]byte { return [3]byte{10, 20, 30} })
	dst := scaleBilinear(s, 4, 4)
	if dst.Bounds().Dx() != 4 || dst.Bounds().Dy() != 4 {
		t.Fatalf("dims %v", dst.Bounds())
	}
	_ = src
}

func TestScaleBilinearClampAndDeterminism(t *testing.T) {
	src := mkNRGBA(2, 2, func(x, y int) [3]byte {
		if x == 0 && y == 0 {
			return [3]byte{0, 0, 0}
		}
		if x == 1 {
			return [3]byte{255, 255, 255}
		}
		return [3]byte{128, 128, 128}
	})
	a := scaleBilinear(src, 4, 4)
	b := scaleBilinear(src, 4, 4)
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatalf("not deterministic")
	}
	// just check within bounds and determinism already, no strict endpoint check for mixed source
	if a.Bounds().Dx() != 4 {
		t.Fatalf("dims")
	}
	// single pixel source
	single := mkNRGBA(1, 1, func(x, y int) [3]byte { return [3]byte{99, 88, 77} })
	expanded := scaleBilinear(single, 2, 2)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			c := expanded.NRGBAAt(x, y)
			if c.R != 99 || c.G != 88 || c.B != 77 {
				t.Fatalf("single pixel expand got %v", c)
			}
		}
	}
	_ = image.Rect
}

// TestScaleBilinearNoNegativeWeights guards the floor-vs-truncate bug: with
// truncation the first centre-aligned sample for upscale yields a negative
// weight and wrapped pixels.
func TestScaleBilinearNoNegativeWeights(t *testing.T) {
	src := mkNRGBA(2, 2, func(x, y int) [3]byte {
		if (x+y)%2 == 0 {
			return [3]byte{0, 0, 0}
		}
		return [3]byte{255, 255, 255}
	})
	dst := scaleBilinear(src, 4, 4)
	cases := []struct {
		x, y int
		want uint8
	}{
		{0, 0, 0},  // edge clamp, no extrapolation
		{0, 1, 64}, // dy=0.25 across black/white vertical
		{1, 0, 64}, // dx=0.25 across black/white horizontal
		{1, 1, 96}, // bilinear midpoint
		{3, 3, 0},  // clamped bottom-right
	}
	for _, c := range cases {
		got := dst.NRGBAAt(c.x, c.y)
		if got.R != c.want || got.G != c.want || got.B != c.want {
			t.Fatalf("pixel(%d,%d)=%d want %d", c.x, c.y, got.R, c.want)
		}
	}
}
