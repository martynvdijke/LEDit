package visualizer

import "image"

// DrawBarsFromBins renders vertical bars directly from client-supplied
// spectrum bins (each 0-255). Unlike the synthetic tempo model this is a pure
// function of the last received bins, so the frame does not advance with time.
func DrawBarsFromBins(width, height int, bins []int) *image.RGBA {
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
	if len(bins) == 0 {
		return img
	}
	ac := baseColor()
	n := len(bins)
	for i := 0; i < n; i++ {
		x0 := i * width / n
		x1 := (i + 1) * width / n
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if x1 > width {
			x1 = width
		}
		bh := binToHeight(bins[i], height-4)
		if bh < 2 {
			bh = 2
		}
		if bh > height-2 {
			bh = height - 2
		}
		fillRect(img, x0, height-bh, x1, height, ac)
	}
	return img
}

// DrawSpectrumFromBins renders a mirrored spectrum centred vertically, sampling
// the supplied bins across the full width.
func DrawSpectrumFromBins(width, height int, bins []int) *image.RGBA {
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
	if len(bins) == 0 {
		return img
	}
	mid := height / 2
	ac := baseColor()
	for x := 0; x < width; x++ {
		idx := x * len(bins) / width
		if idx >= len(bins) {
			idx = len(bins) - 1
		}
		bh := binToHeight(bins[idx], mid-1)
		if bh < 1 {
			bh = 1
		}
		fillRect(img, x, mid-bh, x+1, mid+bh, ac)
	}
	return img
}

// binToHeight maps a 0-255 bin to a 0-max pixel height, clamping out-of-range
// input rather than trusting the sender.
func binToHeight(bin, max int) int {
	if bin < 0 {
		bin = 0
	}
	if bin > 255 {
		bin = 255
	}
	if max < 0 {
		max = 0
	}
	return bin * max / 255
}
