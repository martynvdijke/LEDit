package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"time"
)

// Matrix rain palette. The head is bright, the trail fades from green toward
// near-black to mimic phosphor persistence.
var (
	rainHead       = color.RGBA{184, 255, 176, 255} // #b8ffb0
	rainTrailTop   = color.RGBA{74, 240, 90, 255}   // #4af05a
	rainTrailEnd   = color.RGBA{0, 34, 0, 255}      // #002200
	rainBackground = color.RGBA{10, 10, 12, 255}
)

// rainFPS quantizes time so the animation advances at a fixed frame rate
// regardless of how often the renderer is invoked.
const rainFPS = 10

// rainGlyphs is a compact 5x7 bitmap glyph set (katakana-inspired shapes and
// digits) indexed 0..9. simpleFont already covers digits, so we mix our own
// glyphs with digit glyphs from simpleFont.
var rainGlyphs = [][7][5]uint8{
	// 0: ア
	{{1, 1, 1, 1, 1}, {1, 0, 0, 0, 0}, {0, 1, 0, 1, 0}, {1, 0, 0, 0, 1}, {0, 0, 0, 0, 0}, {1, 0, 1, 0, 0}, {0, 1, 0, 1, 0}},
	// 1: イ
	{{1, 0, 0, 0, 1}, {0, 1, 0, 1, 0}, {1, 1, 1, 1, 1}, {0, 0, 1, 0, 0}, {0, 0, 1, 0, 0}, {0, 0, 1, 0, 0}, {0, 0, 1, 0, 0}},
	// 2: ウ
	{{0, 1, 1, 1, 0}, {1, 0, 0, 0, 1}, {0, 0, 1, 0, 0}, {0, 0, 1, 0, 0}, {0, 0, 1, 0, 0}, {0, 1, 1, 1, 1}, {0, 0, 0, 0, 0}},
	// 3: エ
	{{1, 1, 1, 1, 1}, {0, 0, 0, 0, 0}, {0, 1, 1, 1, 0}, {0, 0, 0, 0, 0}, {1, 1, 1, 1, 1}, {0, 0, 0, 0, 0}, {0, 0, 0, 0, 0}},
	// 4: オ
	{{0, 0, 1, 0, 0}, {1, 0, 1, 0, 1}, {1, 0, 1, 0, 1}, {0, 1, 1, 1, 0}, {0, 0, 1, 0, 0}, {0, 0, 1, 0, 0}, {0, 1, 0, 0, 0}},
	// 5: カ
	{{1, 0, 0, 0, 1}, {1, 0, 0, 0, 1}, {1, 1, 1, 1, 1}, {1, 0, 0, 0, 1}, {1, 0, 0, 0, 1}, {0, 0, 0, 0, 0}, {1, 0, 0, 0, 0}},
	// 6: キ
	{{1, 1, 1, 1, 1}, {0, 0, 0, 0, 0}, {1, 1, 1, 1, 1}, {0, 0, 0, 0, 0}, {1, 1, 1, 1, 1}, {0, 1, 0, 1, 0}, {0, 1, 0, 0, 0}},
	// 7: ン
	{{0, 0, 0, 0, 1}, {0, 0, 0, 1, 0}, {0, 0, 1, 0, 0}, {0, 1, 0, 0, 0}, {1, 0, 0, 0, 0}, {0, 0, 0, 0, 0}, {0, 0, 0, 0, 0}},
	// 8: ト
	{{1, 0, 1, 0, 0}, {1, 0, 1, 0, 0}, {1, 0, 1, 0, 0}, {1, 0, 1, 0, 0}, {1, 0, 1, 0, 0}, {0, 1, 0, 1, 0}, {0, 0, 0, 1, 0}},
	// 9: ロ
	{{1, 1, 1, 1, 1}, {1, 0, 0, 0, 1}, {1, 0, 0, 0, 1}, {1, 0, 0, 0, 1}, {1, 0, 0, 0, 1}, {1, 1, 1, 1, 1}, {0, 0, 0, 0, 0}},
}

// rainLCG is a tiny deterministic per-column PRNG. Same seed + same step ->
// same stream, so renders are byte-identical for a given instant.
type rainLCG struct {
	state uint32
}

func newRainLCG(seed uint32) *rainLCG {
	if seed == 0 {
		seed = 0x9e3779b9
	}
	return &rainLCG{state: seed}
}

func (l *rainLCG) next() uint32 {
	// Numerical Recipes LCG.
	l.state = l.state*1664525 + 1013904223
	return l.state
}

// rainColumn describes one vertical stream of glyphs.
type rainColumn struct {
	head      int  // y position of the brightest glyph
	speed     int  // cells per frame
	glyph     int  // current glyph index
	useCustom bool // custom katakana glyph vs digit
}

// RenderMatrixRain renders a deterministic "Matrix digital rain" animation.
// Columns fall at independent speeds derived from a per-column LCG seeded by
// the column index; the frame step is quantized to rainFPS so the animation
// advances smoothly regardless of refresh rate.
func RenderMatrixRain(now time.Time, width, height int) (*RenderedImage, error) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{rainBackground}, image.Point{}, draw.Src)

	step := int64(now.UnixMilli()) / (1000 / rainFPS)

	// Precompute the trail color gradient once per render.
	trailLen := 6
	if height < trailLen {
		trailLen = height
	}
	trail := make([]color.RGBA, trailLen+1)
	for i := range trailLen + 1 {
		t := float64(i) / float64(trailLen)
		trail[i] = color.RGBA{
			R: uint8(float64(rainTrailTop.R) + (float64(rainTrailEnd.R)-float64(rainTrailTop.R))*t),
			G: uint8(float64(rainTrailTop.G) + (float64(rainTrailEnd.G)-float64(rainTrailTop.G))*t),
			B: uint8(float64(rainTrailTop.B) + (float64(rainTrailEnd.B)-float64(rainTrailTop.B))*t),
			A: 255,
		}
	}

	// Each column is independent: derive its speed and glyph from the LCG.
	for x := 0; x < width; x++ {
		rng := newRainLCG(uint32(x*2654435761) + uint32(step))
		speed := 1 + int(rng.next()%3)
		// Head y advances with the quantized step; phase offset makes columns
		// start at different heights.
		phase := int(rng.next() % uint32(height+trailLen))
		head := (int(step)*speed + phase) % (height + trailLen)
		col := rainColumn{head: head, speed: speed, glyph: int(rng.next() % 10), useCustom: rng.next()%10 < 7}

		for y := 0; y < height; y++ {
			dist := head - y // 0 = head, positive = below (trail)
			var c color.Color
			var glyph [7][5]uint8
			if dist < 0 {
				continue // nothing above the head
			}
			if dist == 0 {
				c = rainHead
			} else if dist <= trailLen {
				c = trail[dist]
			} else {
				continue // fully faded
			}

			// Pick the glyph: custom katakana or a digit from simpleFont.
			if col.useCustom {
				glyph = rainGlyphs[(col.glyph+int(rng.next()))%10]
			} else {
				digit := rune('0' + (col.glyph+int(rng.next()))%10)
				idx := int(digit - 32)
				if idx >= 0 && idx < len(simpleFont) {
					glyph = simpleFont[idx]
				} else {
					continue
				}
			}

			// Draw the 5x7 glyph at (x, y).
			for row := 0; row < 7; row++ {
				for colBit := 0; colBit < 5; colBit++ {
					if glyph[row][colBit] == 1 {
						px, py := x+colBit, y+row
						if px < width && py < height {
							img.Set(px, py, c)
						}
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
