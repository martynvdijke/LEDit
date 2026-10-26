package effects

import (
	"image"
	"image/color"
	"math"
)

// setPx writes a color straight into the destination pixel buffer. Using the
// raw Pix slice (instead of image.RGBA.Set) keeps steady-state rendering free
// of interface conversions and allocations.
func setPx(dst *image.RGBA, x, y int, c color.RGBA) {
	i := dst.PixOffset(x, y)
	pix := dst.Pix
	pix[i+0] = c.R
	pix[i+1] = c.G
	pix[i+2] = c.B
	pix[i+3] = 255
}

// fillPx fills the whole destination buffer with a single color.
func fillPx(dst *image.RGBA, c color.RGBA) {
	pix := dst.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		pix[i+0] = c.R
		pix[i+1] = c.G
		pix[i+2] = c.B
		pix[i+3] = 255
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	t = clamp01(t)
	return color.RGBA{
		R: uint8(clamp255(lerp(float64(a.R), float64(b.R), t))),
		G: uint8(clamp255(lerp(float64(a.G), float64(b.G), t))),
		B: uint8(clamp255(lerp(float64(a.B), float64(b.B), t))),
		A: 255,
	}
}

func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// mixPalette maps t in 0..1 across the palette roles: background at 0, accent
// at 0.5 and text at 1.
func mixPalette(p Palette, t float64) color.RGBA {
	t = clamp01(t)
	if t < 0.5 {
		return lerpRGBA(p.Background, p.Accent, t*2)
	}
	return lerpRGBA(p.Accent, p.Text, (t-0.5)*2)
}

// speedScale converts a 0..100 speed parameter into a time multiplier where 50
// is real time, 0 is frozen and 100 is double speed.
func speedScale(speed float64) float64 {
	return speed / 50.0
}

// contrastScale converts a 0..100 intensity parameter into a contrast factor
// where 50 leaves the field unchanged.
func contrastScale(intensity float64) float64 {
	return 0.4 + intensity/100.0*1.2
}

func applyContrast(n, intensity float64) float64 {
	return clamp01((n-0.5)*contrastScale(intensity) + 0.5)
}

// hash64 is the splitmix64 finalizer: a fast, well-distributed deterministic
// hash used to derive pseudo-random values.
func hash64(v uint64) uint64 {
	v += 0x9e3779b97f4a7c15
	v = (v ^ (v >> 30)) * 0xbf58476d1ce4e5b9
	v = (v ^ (v >> 27)) * 0x94d049bb133111eb
	return v ^ (v >> 31)
}

// randUnit hashes a coordinate and salt into a deterministic 0..1 value.
func randUnit(seed, x, y int64, salt uint64) float64 {
	h := hash64(uint64(seed) ^ (uint64(x) * 0x9e3779b97f4a7c15) ^ (uint64(y) * 0xc2b2ae3d27d4eb4f) ^ salt)
	return float64(h>>11) / float64(1<<53)
}

// noiseSeed derives a stable per-effect seed from the parameters so Seed 0
// still gives each preset a distinct but reproducible pattern.
func noiseSeed(effect string, p Params) int64 {
	if p.Seed != 0 {
		return p.Seed
	}
	var h uint64 = 0x9e3779b97f4a7c15
	for i := 0; i < len(effect); i++ {
		h = hash64(h ^ uint64(effect[i]))
	}
	return int64(h & 0x7fffffffffffffff)
}

// valueNoise2D samples smooth deterministic value noise on an integer lattice
// with bilinear interpolation.
func valueNoise2D(seed int64, x, y float64, salt uint64) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	fx, fy := x-x0, y-y0
	ix0, iy0 := int64(x0), int64(y0)
	v00 := randUnit(seed, ix0, iy0, salt)
	v10 := randUnit(seed, ix0+1, iy0, salt)
	v01 := randUnit(seed, ix0, iy0+1, salt)
	v11 := randUnit(seed, ix0+1, iy0+1, salt)
	sx := smoothstep(fx)
	sy := smoothstep(fy)
	top := lerp(v00, v10, sx)
	bottom := lerp(v01, v11, sx)
	return lerp(top, bottom, sy)
}

func smoothstep(t float64) float64 { return t * t * (3 - 2*t) }

// seedPhase derives a stable phase offset in 0..2π from the parameters seed so
// that every effect honours the seed, including the analytic ones.
func seedPhase(effect string, p Params) float64 {
	s := uint64(noiseSeed(effect, p))
	u := float64(s%100000) / 100000.0
	return u * 2 * math.Pi
}
