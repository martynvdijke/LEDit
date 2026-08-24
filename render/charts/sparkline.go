package charts

import (
	"image"
	"image/color"
)

func DrawSparkline(dst *image.RGBA, bounds image.Rectangle, values []float64, col color.RGBA) {
	DrawLine(dst, bounds, values, col)
}
