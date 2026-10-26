package effects

import (
	"image"
	"math"
	"time"
)

// gradient draws a drifting palette gradient: a sinusoid that travels across the
// field over time.
type gradient struct{}

func (gradient) Name() string { return "gradient" }

func (gradient) Draw(dst *image.RGBA, p Params, elapsed time.Duration) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	freq := 1.0 + p.Density/100.0*2.0
	invW := freq / float64(w)
	invH := freq * 0.5 / float64(h)
	t := elapsed.Seconds()*speedScale(p.Speed)*0.15 + seedPhase("gradient", p)
	for y := 0; y < h; y++ {
		base := float64(y)*invH + t
		for x := 0; x < w; x++ {
			u := (float64(x)*invW + base) * 2 * math.Pi
			n := 0.5 + 0.5*math.Sin(u)
			setPx(dst, x, y, mixPalette(p.Palette, applyContrast(n, p.Intensity)))
		}
	}
}
