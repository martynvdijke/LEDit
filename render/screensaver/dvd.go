package screensaver

import (
	"image"
	"image/color"
	"math"
	"time"
)

var dvdBG = color.RGBA{15, 15, 25, 255}

// DVD logo dimensions
const dvdW = 12
const dvdH = 8

// dvdPalette cycles hue on bounce.
var dvdColors = []color.RGBA{
	{255, 80, 80, 255},
	{80, 255, 80, 255},
	{80, 80, 255, 255},
	{255, 255, 80, 255},
	{255, 80, 255, 255},
	{80, 255, 255, 255},
}

// DrawDVD renders a bouncing DVD logo deterministically from elapsed.
func DrawDVD(width, height int, elapsed time.Duration) *image.RGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, dvdBG)
		}
	}
	// Effective bounds for logo
	maxX := width - dvdW
	maxY := height - dvdH
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	// speed in pixels per second
	vx := 18.0
	vy := 12.0
	sec := elapsed.Seconds()
	// Bounce using mirroring formula
	px, bouncesX := bouncePos(sec*vx, float64(maxX))
	py, bouncesY := bouncePos(sec*vy, float64(maxY))
	totalBounces := bouncesX + bouncesY
	// slight phase offset per variant
	px = math.Mod(px+3, float64(maxX+1))
	py = math.Mod(py+2, float64(maxY+1))
	ix, iy := int(math.Round(px)), int(math.Round(py))
	if ix < 0 {
		ix = 0
	}
	if iy < 0 {
		iy = 0
	}
	if ix > maxX {
		ix = maxX
	}
	if iy > maxY {
		iy = maxY
	}
	c := dvdColors[totalBounces%len(dvdColors)]
	// draw rect with border
	for y := 0; y < dvdH; y++ {
		for x := 0; x < dvdW; x++ {
			// border
			if x == 0 || x == dvdW-1 || y == 0 || y == dvdH-1 {
				img.Set(ix+x, iy+y, c)
			} else {
				// fill slightly darker
				fc := color.RGBA{uint8(int(c.R) * 3 / 5), uint8(int(c.G) * 3 / 5), uint8(int(c.B) * 3 / 5), 255}
				img.Set(ix+x, iy+y, fc)
			}
		}
	}
	// inner "DVD" simplified: two pixels gap
	return img
}

func bouncePos(pos float64, limit float64) (float64, int) {
	if limit <= 0 {
		return 0, 0
	}
	period := limit * 2
	mod := math.Mod(pos, period)
	bounces := int(pos / limit)
	if mod < 0 {
		mod += period
	}
	if mod > limit {
		return period - mod, bounces
	}
	return mod, bounces
}
