package screensaver

import (
	"image"
	"image/color"
	"math"
	"time"
)

const starCount = 50

var starfieldBG = color.RGBA{5, 5, 15, 255}

// DrawStarfield renders a deterministic starfield at given dimensions and elapsed time.
func DrawStarfield(width, height int, elapsed time.Duration) *image.RGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// background
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, starfieldBG)
		}
	}
	sec := elapsed.Seconds()
	for i := 0; i < starCount; i++ {
		// deterministic seed per star
		seed := uint32(i*2654435761 + 0x9e3779b9)
		// pseudo random offsets in [0,1)
		rx := pseudoFloat(seed, 0)
		ry := pseudoFloat(seed, 1)
		rz := pseudoFloat(seed, 2)
		speed := 0.5 + rz*2.0 // parallax speed
		// drift
		xf := math.Mod(rx*float64(width)+sec*speed*8, float64(width))
		yf := math.Mod(ry*float64(height)+sec*speed*4, float64(height))
		// brightness based on depth
		bright := uint8(120 + rz*135)
		// size 1 or 2
		sz := 1
		if rz > 0.7 {
			sz = 2
		}
		c := color.RGBA{bright, bright, bright, 255}
		x := int(xf)
		y := int(yf)
		for dy := 0; dy < sz; dy++ {
			for dx := 0; dx < sz; dx++ {
				px, py := x+dx, y+dy
				if px >= 0 && px < width && py >= 0 && py < height {
					img.Set(px, py, c)
				}
			}
		}
	}
	return img
}

func pseudoFloat(seed uint32, salt uint32) float64 {
	s := seed + salt*1013904223
	s = s*1664525 + 1013904223
	return float64(s%10000) / 10000.0
}
