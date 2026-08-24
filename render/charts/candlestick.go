package charts

import (
	"image"
	"image/color"
)

type OHLC struct {
	Open, High, Low, Close float64
}

func DrawCandlestick(dst *image.RGBA, bounds image.Rectangle, values []float64, ohlc []*OHLC, col color.RGBA) {
	useLine := false
	if len(ohlc) != len(values) {
		useLine = true
	} else {
		for _, o := range ohlc {
			if o == nil {
				useLine = true
				break
			}
		}
	}
	if useLine {
		DrawLine(dst, bounds, values, col)
		return
	}
	if len(values) == 0 {
		return
	}
	// compute bounds from high/low
	var mins, maxs []float64
	for _, o := range ohlc {
		mins = append(mins, o.Low)
		maxs = append(maxs, o.High)
	}
	minVal := mins[0]
	maxVal := maxs[0]
	for _, v := range mins {
		if v < minVal {
			minVal = v
		}
	}
	for _, v := range maxs {
		if v > maxVal {
			maxVal = v
		}
	}
	minVal, maxVal = ComputeBounds([]float64{minVal, maxVal})
	n := len(values)
	candleW := bounds.Dx() / n
	if candleW < 2 {
		candleW = 2
	}
	bodyW := candleW * 2 / 3
	if bodyW < 1 {
		bodyW = 1
	}
	for i, o := range ohlc {
		cx := bounds.Min.X + i*candleW + candleW/2
		highY := bounds.Min.Y + MapY(o.High, minVal, maxVal, bounds.Dy())
		lowY := bounds.Min.Y + MapY(o.Low, minVal, maxVal, bounds.Dy())
		openY := bounds.Min.Y + MapY(o.Open, minVal, maxVal, bounds.Dy())
		closeY := bounds.Min.Y + MapY(o.Close, minVal, maxVal, bounds.Dy())
		// wick
		for y := highY; y <= lowY; y++ {
			dst.Set(cx, bounds.Min.Y+y, col)
		}
		// body
		top := openY
		bot := closeY
		if top > bot {
			top, bot = bot, top
		}
		if top == bot {
			bot++
		}
		x0 := cx - bodyW/2
		x1 := x0 + bodyW
		for y := top; y <= bot; y++ {
			for x := x0; x < x1; x++ {
				dst.Set(x, bounds.Min.Y+y, col)
			}
		}
	}
}
