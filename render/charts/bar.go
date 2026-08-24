package charts

import (
	"image"
	"image/color"
)

func DrawBar(dst *image.RGBA, bounds image.Rectangle, values []float64, col color.RGBA) {
	if len(values) == 0 || bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return
	}
	min, max := ComputeBounds(values)
	n := len(values)
	bw := bounds.Dx() / n
	if bw < 1 {
		bw = 1
	}
	for i, v := range values {
		x0 := bounds.Min.X + i*bw
		x1 := x0 + bw - 1
		if i == n-1 {
			x1 = bounds.Max.X - 1
		}
		yTop := bounds.Min.Y + MapY(v, min, max, bounds.Dy())
		yBot := bounds.Max.Y - 1
		if yTop > yBot {
			yTop, yBot = yBot, yTop
		}
		for y := yTop; y <= yBot; y++ {
			for x := x0; x <= x1 && x < bounds.Max.X; x++ {
				dst.Set(x, y, col)
			}
		}
	}
}
