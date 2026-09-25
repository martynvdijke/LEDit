package render

import (
	"bytes"
	"image"
	"testing"
)

func TestBilinearUpscaleDiffers(t *testing.T) {
	src := mkNRGBA(2, 2, func(x, y int) [3]byte {
		if x == 0 && y == 0 {
			return [3]byte{0, 0, 0}
		}
		if x == 1 && y == 0 {
			return [3]byte{255, 0, 0}
		}
		if x == 0 && y == 1 {
			return [3]byte{0, 255, 0}
		}
		return [3]byte{0, 0, 255}
	})
	cfgNearest := PixelMapConfig{Width: 4, Height: 4, Gamma: 1, OriginTop: true, Bilinear: false}
	cfgBilinear := PixelMapConfig{Width: 4, Height: 4, Gamma: 1, OriginTop: true, Bilinear: true}
	a := FrameToPixels(src, cfgNearest)
	b := FrameToPixels(src, cfgBilinear)
	if bytes.Equal(a, b) {
		t.Fatalf("bilinear should differ from nearest")
	}
	for _, v := range b {
		if v > 255 {
			t.Fatalf("out of bounds")
		}
	}
}

func TestBilinearEqualDimsIdentical(t *testing.T) {
	src := mkNRGBA(4, 4, func(x, y int) [3]byte { return [3]byte{byte(x * 10), byte(y * 10), 100} })
	a := FrameToPixels(src, PixelMapConfig{Width: 4, Height: 4, Gamma: 1, OriginTop: true, Bilinear: false})
	b := FrameToPixels(src, PixelMapConfig{Width: 4, Height: 4, Gamma: 1, OriginTop: true, Bilinear: true})
	if !bytes.Equal(a, b) {
		t.Fatalf("equal dims should be identical regardless of bilinear")
	}
}

func TestPerPanelGamma(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		i := src.PixOffset(x, 0)
		src.Pix[i+0] = 128
		src.Pix[i+1] = 128
		src.Pix[i+2] = 128
		src.Pix[i+3] = 255
	}
	cfg := PixelMapConfig{Width: 4, Height: 1, Gamma: 1, OriginTop: true, PanelCols: 2, PanelGammas: []float64{1, 2.2}}
	out := FrameToPixels(src, cfg)
	// col 0,1 should be 128 (gamma 1), col 2,3 should be gamma corrected !=128
	if out[0] != 128 || out[3] != 128 {
		t.Fatalf("panel 0 gamma 1 expected 128 got %d %d", out[0], out[3])
	}
	if out[6] == 128 || out[9] == 128 {
		t.Fatalf("panel 1 gamma 2.2 should differ from 128 got %d %d", out[6], out[9])
	}
}

func TestPerPanelColorOrder(t *testing.T) {
	src := mkNRGBA(4, 1, func(x, y int) [3]byte { return [3]byte{10, 20, 30} })
	cfg := PixelMapConfig{Width: 4, Height: 1, Gamma: 1, OriginTop: true, PanelCols: 2, PanelColorOrders: []string{"RGB", "GRB"}}
	out := FrameToPixels(src, cfg)
	// panel0 RGB, panel1 GRB
	if out[0] != 10 || out[1] != 20 || out[2] != 30 {
		t.Fatalf("panel0 RGB got %v", out[0:3])
	}
	if out[6] != 20 || out[7] != 10 || out[8] != 30 {
		t.Fatalf("panel1 GRB got %v", out[6:9])
	}
}

func TestPerPanelEmptyIdentical(t *testing.T) {
	src := mkNRGBA(4, 1, func(x, y int) [3]byte { return [3]byte{10, 20, 30} })
	base := FrameToPixels(src, PixelMapConfig{Width: 4, Height: 1, Gamma: 1, OriginTop: true})
	withEmpty := FrameToPixels(src, PixelMapConfig{Width: 4, Height: 1, Gamma: 1, OriginTop: true, PanelCols: 2, PanelGammas: []float64{}, PanelColorOrders: []string{}})
	if !bytes.Equal(base, withEmpty) {
		t.Fatalf("empty arrays should be identical")
	}
}

// TestPerPanelIndexDistribution checks panel index math when width is not a
// multiple of the panel count: (x*cols)/width, e.g. 3px over 2 panels -> 0,0,1.
func TestPerPanelIndexDistribution(t *testing.T) {
	src := mkNRGBA(3, 1, func(x, y int) [3]byte { return [3]byte{10, 20, 30} })
	cfg := PixelMapConfig{Width: 3, Height: 1, Gamma: 1, OriginTop: true, PanelCols: 2, PanelColorOrders: []string{"GRB", "BGR"}}
	out := FrameToPixels(src, cfg)
	want := [][3]byte{{20, 10, 30}, {20, 10, 30}, {30, 20, 10}}
	for col := 0; col < 3; col++ {
		got := out[col*3 : col*3+3]
		for c := 0; c < 3; c++ {
			if got[c] != want[col][c] {
				t.Fatalf("col %d = %v want %v", col, got, want[col])
			}
		}
	}
}
