package effects

import (
	"image"
	"time"
)

// sparkle draws sparse twinkling points over the palette background.
type sparkle struct{}

func (sparkle) Name() string { return "sparkle" }

func (sparkle) Draw(dst *image.RGBA, p Params, elapsed time.Duration) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	seed := noiseSeed("sparkle", p)
	tick := TickIndex(elapsed)
	// speed 50 keeps the built-in two second twinkle; higher speeds shorten it.
	cycle := int64(30)
	if s := speedScale(p.Speed); s > 0 {
		cycle = int64(clampFloat(30.0/s, 2, 600))
	}
	threshold := p.Density / 100.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			spot := randUnit(seed, int64(x), int64(y), 0x51)
			if spot > threshold {
				setPx(dst, x, y, p.Palette.Background)
				continue
			}
			offset := int64(spot * float64(cycle))
			phase := (tick + offset) % cycle
			brightness := 1.0 - float64(phase)/float64(cycle)
			brightness = clamp01(brightness * (0.4 + 1.2*(p.Intensity/100.0)))
			c := lerpRGBA(p.Palette.Background, p.Palette.Accent, brightness)
			if brightness > 0.75 {
				c = lerpRGBA(p.Palette.Accent, p.Palette.Text, (brightness-0.75)/0.25)
			}
			setPx(dst, x, y, c)
		}
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
