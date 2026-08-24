package charts

import "image/color"

func ComputeBounds(values []float64) (min, max float64) {
	if len(values) == 0 {
		return 0, 1
	}
	min, max = values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	if span == 0 {
		pad := 0.0
		if max != 0 {
			pad = max * 0.05
			if pad < 0 {
				pad = -pad
			}
		}
		if pad == 0 {
			pad = 1
		}
		min -= pad
		max += pad
	} else {
		pad := span * 0.05
		min -= pad
		max += pad
	}
	return min, max
}

func MapY(v, min, max float64, h int) int {
	if h <= 1 {
		return 0
	}
	if max == min {
		return h / 2
	}
	ratio := (v - min) / (max - min)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return (h - 1) - int(ratio*float64(h-1))
}

func ChooseColor(accent color.RGBA) color.RGBA { return accent }
