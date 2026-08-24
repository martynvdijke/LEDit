package visualizer

import (
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"time"

	"ledit/datasource/nowplaying"
)

func hashSeed(np nowplaying.NowPlaying) uint64 {
	h := fnv.New64a()
	h.Write([]byte(np.Artist + "|" + np.Track))
	return h.Sum64()
}

func tempoHz(np nowplaying.NowPlaying) float64 {
	if np.TempoBPM != nil && *np.TempoBPM > 0 {
		return float64(*np.TempoBPM) / 60.0
	}
	return 2.0 // 120 BPM
}

func baseColor() color.RGBA { return color.RGBA{80, 250, 123, 255} }
func bgColor() color.RGBA   { return color.RGBA{20, 20, 30, 255} }

// draw helpers shared

func barHeight(seed uint64, idx int, elapsed time.Duration, hz, energy float64, isIdle bool) float64 {
	if isIdle {
		// slow 0.5Hz sine
		t := elapsed.Seconds() * 0.5 * 2 * math.Pi
		return 0.15 + 0.1*math.Sin(t+float64(idx)*0.7)
	}
	phase := float64((seed>>uint(idx%8))&0xFF) / 255.0 * math.Pi * 2
	t := elapsed.Seconds() * hz * 2 * math.Pi
	// bars[i] = 0.5 + 0.5*sin(i*phase + elapsed*tempo*scale)
	val := 0.5 + 0.5*math.Sin(float64(idx)*0.6+phase+t*0.9)
	// energy scales amplitude 0-1
	val = 0.2 + val*0.8*math.Max(0.3, energy)
	if val < 0.05 {
		val = 0.05
	}
	if val > 1 {
		val = 1
	}
	return val
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	for y := y0; y < y1; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := x0; x < x1; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			img.Set(x, y, c)
		}
	}
}
