package effects

import (
	"image"
	"math"
	"time"
)

// plasma draws a smooth, slowly evolving interference field. It generalizes the
// classic plasma look while taking every color from the theme palette.
type plasma struct{}

func (plasma) Name() string { return "plasma" }

func (plasma) Draw(dst *image.RGBA, p Params, elapsed time.Duration) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	freq := 6.0 + p.Density/100.0*6.0
	invW := freq / float64(w)
	invH := freq / float64(h)
	t := elapsed.Seconds()*speedScale(p.Speed) + seedPhase("plasma", p)
	for y := 0; y < h; y++ {
		fy := float64(y)*invH + t*0.7
		for x := 0; x < w; x++ {
			fx := float64(x) * invW
			v := math.Sin(fx+t) +
				math.Sin(fy) +
				math.Sin((fx+fy+t)*0.5+1.7) +
				math.Sin(math.Sqrt(fx*fx+fy*fy)+t*1.2)
			n := (v + 4) / 8
			setPx(dst, x, y, mixPalette(p.Palette, applyContrast(n, p.Intensity)))
		}
	}
}
