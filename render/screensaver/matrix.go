package screensaver

import (
	"image"
	"image/color"
	"time"
)

var matrixBG = color.RGBA{5, 10, 5, 255}

// DrawMatrix renders deterministic matrix rain from elapsed.
func DrawMatrix(width, height int, elapsed time.Duration) *image.RGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, matrixBG)
		}
	}
	// 10 fps quantization like render/matrixrain
	const fps = 10
	step := elapsed.Milliseconds() / (1000 / fps)
	trailLen := 6
	if height < trailLen {
		trailLen = height
	}
	for x := 0; x < width; x++ {
		seed := uint32(x*2654435761) + uint32(step)
		rng := lcg(seed)
		speed := 1 + int(rng()%3)
		phase := int(rng() % uint32(height+trailLen))
		head := (int(step)*speed + phase) % (height + trailLen)
		// color trail
		for y := 0; y < height; y++ {
			dist := head - y
			if dist < 0 || dist > trailLen {
				continue
			}
			var c color.RGBA
			if dist == 0 {
				c = color.RGBA{180, 255, 180, 255}
			} else {
				t := float64(dist) / float64(trailLen)
				c = color.RGBA{
					R: uint8(70 * (1 - t)),
					G: uint8(220 - 100*t),
					B: uint8(70 * (1 - t)),
					A: 255,
				}
			}
			// draw 1px per column; use small vertical bar
			if uint32(rng()%5) != 0 { // sparse
				img.Set(x, y, c)
			}
		}
	}
	return img
}

func lcg(seed uint32) func() uint32 {
	if seed == 0 {
		seed = 0x9e3779b9
	}
	s := seed
	return func() uint32 {
		s = s*1664525 + 1013904223
		return s
	}
}
