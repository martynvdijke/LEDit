package charts

import (
	"image"
	"image/color"
)

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		if x0 >= 0 && y0 >= 0 && x0 < img.Bounds().Dx() && y0 < img.Bounds().Dy() {
			img.Set(x0, y0, col)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func DrawLine(dst *image.RGBA, bounds image.Rectangle, values []float64, col color.RGBA) {
	if len(values) == 0 || bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return
	}
	min, max := ComputeBounds(values)
	n := len(values)
	for i := 0; i < n-1; i++ {
		var x0, x1 int
		if n == 1 {
			x0 = bounds.Min.X
			x1 = bounds.Max.X - 1
		} else {
			x0 = bounds.Min.X + int(float64(i)*float64(bounds.Dx()-1)/float64(n-1))
			x1 = bounds.Min.X + int(float64(i+1)*float64(bounds.Dx()-1)/float64(n-1))
		}
		y0 := bounds.Min.Y + MapY(values[i], min, max, bounds.Dy())
		y1 := bounds.Min.Y + MapY(values[i+1], min, max, bounds.Dy())
		drawLine(dst, x0, y0, x1, y1, col)
	}
	if n == 1 {
		y := bounds.Min.Y + MapY(values[0], min, max, bounds.Dy())
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.Set(x, y, col)
		}
	}
}
