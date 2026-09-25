package render

import (
	"image"
	"math"
)

// PixelMapConfig controls FrameToPixels mapping.
type PixelMapConfig struct {
	Width, Height    int
	ColorOrder       string
	Gamma            float64
	Serpentine       bool
	OriginTop        bool
	Bilinear         bool
	PanelCols        int
	PanelGammas      []float64
	PanelColorOrders []string
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
		if cfg.Bilinear {
			scaled = scaleBilinear(img, w, h)
		} else {
			scaled = scaleNearestNeighbor(img, w, h)
		}
	}

	// Build LUTs: per-panel if applicable, else global.
	var lut *[256]byte
	useLUT := cfg.Gamma != 1 && cfg.Gamma > 0 && cfg.Gamma != 0
	if useLUT {
		lut = GammaLUT(cfg.Gamma)
	}
	var panelLUTs []*[256]byte
	var panelCOs []string
	if cfg.PanelCols > 1 && (len(cfg.PanelGammas) > 0 || len(cfg.PanelColorOrders) > 0) {
		panelLUTs = make([]*[256]byte, cfg.PanelCols)
		panelCOs = make([]string, cfg.PanelCols)
		for i := 0; i < cfg.PanelCols; i++ {
			if i < len(cfg.PanelGammas) && cfg.PanelGammas[i] > 0 {
				// Gamma==1 yields an identity LUT, which explicitly overrides
				// any device-level gamma for this panel.
				panelLUTs[i] = GammaLUT(cfg.PanelGammas[i])
			}
			if i < len(cfg.PanelColorOrders) {
				co := cfg.PanelColorOrders[i]
				if co == "RGB" || co == "GRB" || co == "BGR" {
					panelCOs[i] = co
				}
			}
		}
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
			// Per-panel overrides.
			effLUT := lut
			effCO := cfg.ColorOrder
			if cfg.PanelCols > 1 && len(panelLUTs) > 0 {
				panelIdx := (col * cfg.PanelCols) / w
				if panelIdx < 0 {
					panelIdx = 0
				}
				if panelIdx >= cfg.PanelCols {
					panelIdx = cfg.PanelCols - 1
				}
				// nil entry = no per-panel override; keep device-level value.
				if panelLUTs[panelIdx] != nil {
					effLUT = panelLUTs[panelIdx]
				}
				if panelCOs[panelIdx] != "" {
					effCO = panelCOs[panelIdx]
				}
			}
			if effLUT != nil {
				r = effLUT[r]
				g = effLUT[g]
				b = effLUT[b]
			}
			off := start + col*3
			switch effCO {
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
