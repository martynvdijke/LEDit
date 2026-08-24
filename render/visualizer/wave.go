package visualizer

import (
	"image"
	"math"
	"time"

	"ledit/datasource/nowplaying"
)

func DrawWave(width, height int, np nowplaying.NowPlaying, elapsed time.Duration) *image.RGBA {
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
	ac := baseColor()
	mid := height / 2
	amp := float64(mid-2) * 0.6
	if isIdle {
		amp = float64(mid-2) * 0.2
		hz = 0.5
	}
	for x := 0; x < width; x++ {
		phase := float64((seed>>uint(x%8))&0xFF) / 255.0
		t := elapsed.Seconds()*hz*2*math.Pi + float64(x)*0.2 + phase
		yOff := math.Sin(t) * amp * (0.5 + 0.5*energy)
		if isIdle {
			yOff = math.Sin(elapsed.Seconds()*0.5*2*math.Pi+float64(x)*0.1) * amp
		}
		y := mid + int(yOff)
		if y < 0 {
			y = 0
		}
		if y >= height {
			y = height - 1
		}
		img.Set(x, y, ac)
		if y+1 < height {
			img.Set(x, y+1, ac)
		}
	}
	return img
}
