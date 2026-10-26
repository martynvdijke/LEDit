package effects

import (
	"image"
	"math"
	"time"
)

// scan draws a bright band sweeping across the field in the configured
// direction over a faint static gradient so the surface is never blank.
type scan struct{}

func (scan) Name() string { return "scan" }

func (scan) Draw(dst *image.RGBA, p Params, elapsed time.Duration) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	dir := p.Direction
	if dir == "" {
		dir = DirectionLeft
	}
	phase := 0.5
	if dir != DirectionNone {
		t := elapsed.Seconds()*speedScale(p.Speed) + seedPhase("scan", p)
		phase = math.Mod(t/2.0, 1.0)
		if phase < 0 {
			phase++
		}
	}
	bandHalf := 0.06 + p.Intensity/100.0*0.22
	bandGain := 0.35 + 0.65*(p.Intensity/100.0)
	vertical := dir == DirectionUp || dir == DirectionDown
	maxAxis := float64(w - 1)
	if vertical {
		maxAxis = float64(h - 1)
	}
	if maxAxis <= 0 {
		maxAxis = 1
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			axis := float64(x) / maxAxis
			if vertical {
				axis = float64(y) / maxAxis
			}
			if dir == DirectionUp || dir == DirectionLeft {
				axis = 1 - axis
			}
			dist := math.Abs(axis - phase)
			if dist > 0.5 {
				dist = 1 - dist
			}
			band := clamp01(1 - dist/bandHalf)
			faint := 0.06 + 0.18*(1-axis)
			setPx(dst, x, y, mixPalette(p.Palette, clamp01(band*bandGain+faint)))
		}
	}
}
