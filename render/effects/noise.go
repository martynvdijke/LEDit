package effects

import (
	"image"
	"time"
)

// noise draws smooth deterministic value noise drifting upward.
type noise struct{}

func (noise) Name() string { return "noise" }

func (noise) Draw(dst *image.RGBA, p Params, elapsed time.Duration) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	seed := noiseSeed("noise", p)
	scale := 3.0 + p.Density/100.0*11.0
	invW := scale / float64(w)
	invH := scale / float64(h)
	t := elapsed.Seconds() * speedScale(p.Speed) * 0.5
	for y := 0; y < h; y++ {
		ny := float64(y)*invH + t
		for x := 0; x < w; x++ {
			n := valueNoise2D(seed, float64(x)*invW, ny, 0x33)
			setPx(dst, x, y, mixPalette(p.Palette, applyContrast(n, p.Intensity)))
		}
	}
}
