package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"time"
)

// 3x5 glyph bitmaps for digits 0-9 and ':'.
// Each glyph is 5 rows of 3 chars: '1' = lit, '0' = off.
var clockGlyphs = map[rune][]string{
	'0': {"111", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "010", "010", "010"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
	':': {"000", "010", "000", "010", "000"},
}

// RenderClock renders a big HH:MM digital clock (24h, server local time)
// centered on line 1 using 3x5 bitmap digit glyphs scaled up, plus a
// smaller date+day line beneath (e.g. "SUN AUG 23").
func RenderClock(now time.Time, width, height int, theme Theme, fontPath string) (*RenderedImage, error) {
	if width < 8 {
		width = 8
	}
	if height < 8 {
		height = 8
	}
	_ = fontPath // kept for API symmetry; clock uses bitmap glyphs

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{theme.BackgroundColor[0], theme.BackgroundColor[1], theme.BackgroundColor[2], 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	textCol := color.RGBA{theme.TextColor[0], theme.TextColor[1], theme.TextColor[2], 255}
	accentCol := color.RGBA{theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2], 255}

	// Time string HH:MM 24h
	timeStr := now.Format("15:04")

	// Scale factor: digitH*scale ≈ height/3, clamp 1..8
	const digitH = 5
	scale := (height / 3) / digitH
	if scale < 1 {
		scale = 1
	}
	if scale > 8 {
		scale = 8
	}

	const digitW = 3
	const gap = 1 // 1 cell gap between glyphs (scaled)

	// Total clock width in pixels
	n := len(timeStr)
	totalW := n*digitW*scale + (n-1)*gap*scale

	startX := (width - totalW) / 2
	if startX < 0 {
		startX = 0
		// if doesn't fit, reduce scale to fit width
		for scale > 1 && totalW > width {
			scale--
			totalW = n*digitW*scale + (n-1)*gap*scale
			startX = (width - totalW) / 2
			if startX < 0 {
				startX = 0
			}
		}
	}

	clockH := digitH * scale
	// Center vertically in upper portion: roughly 1/3 from top, but centered
	// Put clock at y = (height-clockH)/2 - small offset to leave room for date
	// Keep date line near bottom.
	startY := (height - clockH) / 3
	if startY < 2 {
		startY = 2
	}

	// Draw each glyph
	x := startX
	for _, r := range timeStr {
		glyph, ok := clockGlyphs[r]
		if !ok {
			x += digitW*scale + gap*scale
			continue
		}
		col := textCol
		if r == ':' {
			col = accentCol
		}
		for row, rowStr := range glyph {
			for colIdx, ch := range rowStr {
				if ch == '1' {
					x1 := x + colIdx*scale
					y1 := startY + row*scale
					fillRect(img, x1, y1, x1+scale-1, y1+scale-1, col)
				}
			}
		}
		x += digitW*scale + gap*scale
	}

	// Date line: e.g. "SUN AUG 23"
	dateStr := strings.ToUpper(now.Format("Mon Jan 2"))
	// drawStringSimple uses charW=6, charH=7
	charW := 6
	charH := 7
	dateW := len(dateStr) * charW
	dateX := (width - dateW) / 2
	if dateX < 0 {
		dateX = 0
	}
	// Place near bottom, with small margin
	dateY := height - charH - 2
	if dateY < startY+clockH+2 {
		dateY = startY + clockH + 2
	}
	if dateY+charH < height {
		drawStringSimple(img, dateStr, dateX, dateY, textCol)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
