package render

import (
	"image"
	"math"
)

// PixelMapConfig controls FrameToPixels mapping.
type PixelMapConfig struct {
	Width, Height int
	ColorOrder    string
	Gamma         float64
	Serpentine    bool
	OriginTop     bool
}

// GammaLUT builds a gamma correction lookup table.
// gamma==1 or non-positive yields identity.
func GammaLUT(gamma float64) *[256]byte {
	var lut [256]byte
	if gamma == 1 || gamma <= 0 {
		for i := range 256 {
			lut[i] = byte(i)
		}
		return &lut
	}
	inv := 1.0 / gamma
	for i := range 256 {
		v := math.Pow(float64(i)/255.0, inv) * 255.0
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		lut[i] = byte(math.Round(v))
	}
	return &lut
}

// FrameToPixels converts img to exactly Width*Height*3 RGB bytes.
// Nearest-neighbour scaling is used when source size mismatches config size;
// quality is irrelevant at LED scale.
// ponytail: nearest-neighbour, O(w*h); bilinear/CatmullRom if visual quality matters.
func FrameToPixels(img *image.NRGBA, cfg PixelMapConfig) []byte {
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return nil
	}
	out := make([]byte, w*h*3)

	if img == nil {
		return out
	}

	// Scale/crop to matrix size via nearest-neighbour if needed.
	scaled := img
	sb := img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw != w || sh != h {
		scaled = scaleNearestNeighbor(img, w, h)
	}

	// Build LUT if needed.
	var lut *[256]byte
	useLUT := cfg.Gamma != 1 && cfg.Gamma > 0 && cfg.Gamma != 0
	if useLUT {
		lut = GammaLUT(cfg.Gamma)
	}

	for row := 0; row < h; row++ {
		var y int
		if cfg.OriginTop {
			y = row
		} else {
			y = h - 1 - row
		}
		reverse := cfg.Serpentine && row%2 == 1
		start := row * w * 3
		for col := 0; col < w; col++ {
			var x int
			if reverse {
				x = w - 1 - col
			} else {
				x = col
			}
			si := scaled.PixOffset(x, y)
			r := scaled.Pix[si+0]
			g := scaled.Pix[si+1]
			b := scaled.Pix[si+2]
			if useLUT {
				r = lut[r]
				g = lut[g]
				b = lut[b]
			}
			off := start + col*3
			switch cfg.ColorOrder {
			case "GRB":
				out[off+0] = g
				out[off+1] = r
				out[off+2] = b
			case "BGR":
				out[off+0] = b
				out[off+1] = g
				out[off+2] = r
			default:
				out[off+0] = r
				out[off+1] = g
				out[off+2] = b
			}
		}
	}
	return out
}
