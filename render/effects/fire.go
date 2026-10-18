package effects

import (
	"image"
	"time"
)

// fire draws an upward-scrolling heat field: hot at the bottom, cooling toward
// the top, colored from the palette.
type fire struct{}

func (fire) Name() string { return "fire" }

func (fire) Draw(dst *image.RGBA, p Params, elapsed time.Duration) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	seed := noiseSeed("fire", p)
	scale := 3.0 + p.Density/100.0*9.0
	invW := scale / float64(w)
	invH := scale / float64(h)
	t := elapsed.Seconds() * speedScale(p.Speed) * 2.0
	heatScale := 0.35 + 0.65*(p.Intensity/100.0)
	for y := 0; y < h; y++ {
		// Distance above the bottom row, in noise units, scrolling upward.
		ny := float64(h-1-y)*invH - t
		fall := 1.0 - float64(y)/float64(h)
		for x := 0; x < w; x++ {
			nx := float64(x) * invW
			n := valueNoise2D(seed, nx, ny, 0x22)
			heat := clamp01((n*fall*heatScale - 0.12) / 0.88)
			setPx(dst, x, y, mixPalette(p.Palette, heat))
		}
	}
}
