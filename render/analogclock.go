package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"time"
)

// Analog clock palette (cyber-ish, matches panel theme).
var (
	clockBG     = color.RGBA{40, 42, 54, 255}    // #282a36
	clockFace   = color.RGBA{68, 71, 90, 255}    // #44475a
	clockText   = color.RGBA{139, 233, 253, 255} // #8be9fd
	clockAccent = color.RGBA{80, 250, 123, 255}  // #50fa7b
	clockSecond = color.RGBA{255, 121, 198, 255} // #ff79c6
)

// clockAngles returns the sweep angles (in radians, measured clockwise from
// 12 o'clock) for the hour, minute and second hands at the given instant.
// Fractional seconds keep the second hand moving smoothly.
func clockAngles(now time.Time) (hour, minute, second float64) {
	fsec := float64(now.Second()) + float64(now.Nanosecond())/1e9
	fmin := float64(now.Minute()) + fsec/60
	fhour := float64(now.Hour()%12) + fmin/60
	second = fsec / 60 * 2 * math.Pi
	minute = fmin / 60 * 2 * math.Pi
	hour = fhour / 12 * 2 * math.Pi
	return hour, minute, second
}

// line draws a Bresenham line between two integer points.
func line(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 >= 0 && y0 >= 0 && x0 < img.Bounds().Dx() && y0 < img.Bounds().Dy() {
			img.Set(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// circleOutline draws a 1px circle outline via the midpoint algorithm.
func circleOutline(img *image.RGBA, cx, cy, r int, c color.Color) {
	x, y := r, 0
	err := 1 - r
	for x >= y {
		setPx := func(px, py int) {
			if px >= 0 && py >= 0 && px < img.Bounds().Dx() && py < img.Bounds().Dy() {
				img.Set(px, py, c)
			}
		}
		setPx(cx+x, cy+y)
		setPx(cx+y, cy+x)
		setPx(cx-y, cy+x)
		setPx(cx-x, cy+y)
		setPx(cx-x, cy-y)
		setPx(cx-y, cy-x)
		setPx(cx+y, cy-x)
		setPx(cx+x, cy-y)
		y++
		if err < 0 {
			err += 2*y + 1
		} else {
			x--
			err += 2*(y-x) + 1
		}
	}
}

// RenderAnalogClock draws an analog clock face — circle, 12/3/6/9 markers and
// hour/minute/second hands — plus a small digital HH:MM strip when the canvas
// is tall enough. The output depends only on the injected time, so the same
// instant always produces byte-identical output at a given resolution.
func RenderAnalogClock(now time.Time, width, height int) (*RenderedImage, error) {
	if width < 8 {
		width = 8
	}
	if height < 8 {
		height = 8
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{clockBG}, image.Point{}, draw.Src)

	// Reserve the bottom strip for the digital time when there is room.
	digital := height >= 40 && width >= 40
	faceH := height
	if digital {
		faceH = height - 12
	}

	cx := width / 2
	cy := faceH / 2
	r := min(width, faceH)/2 - 2
	if r < 1 {
		r = 1
	}

	circleOutline(img, cx, cy, r, clockFace)

	// 12/3/6/9 hour markers.
	for _, m := range []struct{ px, py int }{
		{cx, cy - r + 1},
		{cx + r - 1, cy},
		{cx, cy + r - 1},
		{cx - r + 1, cy},
	} {
		img.Set(m.px, m.py, clockAccent)
		if m.px+1 < width {
			img.Set(m.px+1, m.py, clockAccent)
		}
	}

	hour, minute, second := clockAngles(now)

	// Hand lengths proportional to the face radius.
	hand := func(angle, length float64) (int, int) {
		return cx + int(math.Round(length*math.Sin(angle))),
			cy - int(math.Round(length*math.Cos(angle)))
	}
	hx, hy := hand(hour, float64(r)*0.5)
	mx, my := hand(minute, float64(r)*0.75)
	sx, sy := hand(second, float64(r)*0.85)

	line(img, cx, cy, mx, my, clockText)
	line(img, cx, cy, sx, sy, clockSecond)
	// Hour hand on top so it stays visible against the minute hand.
	line(img, cx, cy, hx, hy, clockAccent)
	img.Set(cx, cy, clockAccent)

	if digital {
		ts := now.Format("15:04")
		textX := max(0, (width-len(ts)*6)/2)
		drawStringSimple(img, ts, textX, faceH+2, clockText)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
