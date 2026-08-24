package screensaver

import (
	"image"
	"image/color"
	"math"
	"time"
)

// DrawPlasma renders sin-based plasma field.
func DrawPlasma(width, height int, elapsed time.Duration) *image.RGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	t := elapsed.Seconds()
	// variant offset
	phase := 1.7
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fx := float64(x) / float64(width) * 6
			fy := float64(y) / float64(height) * 6
			v := math.Sin(fx+t) + math.Sin(fy+t*0.7) + math.Sin((fx+fy+t)*0.5+phase) + math.Sin(math.Sqrt(fx*fx+fy*fy)+t*1.2)
			// v in [-4,4] -> normalize to [0,1]
			n := (v + 4) / 8
			if n < 0 {
				n = 0
			}
			if n > 1 {
				n = 1
			}
			// palette gradient
			r := uint8(80 + 120*math.Sin(n*math.Pi))
			g := uint8(40 + 180*n)
			b := uint8(150 + 100*math.Cos(n*math.Pi*0.5))
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	return img
}
