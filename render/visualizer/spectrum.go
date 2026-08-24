package visualizer

import (
	"image"
	"time"

	"ledit/datasource/nowplaying"
)

func DrawSpectrum(width, height int, np nowplaying.NowPlaying, elapsed time.Duration) *image.RGBA {
	if width <= 0 {
		width = 64
	}
	if height <= 0 {
		height = 64
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := bgColor()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bg)
		}
	}
	isIdle := np.State != "play"
	seed := hashSeed(np)
	hz := tempoHz(np)
	energy := np.Energy
	if energy == 0 {
		energy = 0.5
	}
	mid := height / 2
	nBars := width
	ac := baseColor()
	for x := 0; x < nBars; x++ {
		h := barHeight(seed, x, elapsed, hz, energy, isIdle)
		bh := int(h * float64(mid-1))
		if bh < 1 {
			bh = 1
		}
		fillRect(img, x, mid-bh, x+1, mid+bh, ac)
	}
	return img
}
