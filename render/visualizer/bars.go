package visualizer

import (
	"image"
	"time"

	"ledit/datasource/nowplaying"
)

func DrawBars(width, height int, np nowplaying.NowPlaying, elapsed time.Duration) *image.RGBA {
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
	nBars := 16
	if width < 32 {
		nBars = 8
	}
	barW := width / nBars
	if barW < 1 {
		barW = 1
	}
	ac := baseColor()
	for i := 0; i < nBars; i++ {
		h := barHeight(seed, i, elapsed, hz, energy, isIdle)
		bh := int(h * float64(height-4))
		if bh < 2 {
			bh = 2
		}
		x0 := i * barW
		x1 := x0 + barW - 1
		if x1 > width {
			x1 = width
		}
		y0 := height - bh
		fillRect(img, x0, y0, x1, height, ac)
	}
	return img
}
