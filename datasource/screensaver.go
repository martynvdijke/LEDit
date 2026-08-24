package datasource

import (
	"bytes"
	"image"
	"image/png"
	"time"

	"ledit/render"
	"ledit/render/screensaver"
)

// ValidScreensaverVariants is the set of allowed ids.
var ValidScreensaverVariants = map[string]bool{
	"starfield": true,
	"dvd":       true,
	"matrix":    true,
	"plasma":    true,
}

// ScreensaverDS is a built-in animated datasource for ambient screensavers.
type ScreensaverDS struct {
	Variant   string
	startTime time.Time
}

func (s *ScreensaverDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if width <= 0 {
		width = 64
	}
	if height <= 0 {
		height = 64
	}
	elapsed := time.Since(s.startTime)
	if s.startTime.IsZero() {
		elapsed = 0
	}
	var img image.Image
	switch s.Variant {
	case "starfield":
		img = screensaver.DrawStarfield(width, height, elapsed)
	case "dvd":
		img = screensaver.DrawDVD(width, height, elapsed)
	case "matrix":
		img = screensaver.DrawMatrix(width, height, elapsed)
	case "plasma":
		img = screensaver.DrawPlasma(width, height, elapsed)
	default:
		img = screensaver.DrawStarfield(width, height, elapsed)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

// Animator: always animates at ~15fps.
func (s *ScreensaverDS) FrameCount() int { return 1000000 }

func (s *ScreensaverDS) NextFrame(now time.Time) int {
	if s.startTime.IsZero() {
		s.startTime = now
		return 0
	}
	elapsed := now.Sub(s.startTime)
	return int(elapsed.Milliseconds() / 66)
}
