package render

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"time"
)

// OverlaySpec describes a persistent text strip composited over content frames
// at send time (never baked into the last-known-good cache).
type OverlaySpec struct {
	Enabled    bool
	Position   string // "top" | "bottom"
	Height     int    // strip height in pixels
	Text       string
	SpeedPx    int    // 0 = static
	Background string // "#rrggbb"
	Foreground string // "#rrggbb"
}

// DefaultOverlaySpec returns the v1 defaults matching the device_settings schema.
func DefaultOverlaySpec() OverlaySpec {
	return OverlaySpec{Position: "bottom", Height: 8, SpeedPx: 0, Background: "#000000", Foreground: "#ffffff"}
}

func isValidHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ValidateOverlaySpec rejects invalid overlay config. Disabled specs are always
// valid. maxH mirrors the documented bound min(32, deviceHeight/2) with a floor
// of 4 so tiny devices still accept the minimum strip.
func ValidateOverlaySpec(spec OverlaySpec, deviceHeight int) error {
	if !spec.Enabled {
		return nil
	}
	if spec.Position != "top" && spec.Position != "bottom" {
		return errors.New("overlay_position must be one of top, bottom")
	}
	if spec.Text == "" {
		return errors.New("overlay_text is required when overlay is enabled")
	}
	if len(spec.Text) > 200 {
		return errors.New("overlay_text must be 200 characters or fewer")
	}
	maxH := 32
	if deviceHeight/2 < maxH {
		maxH = deviceHeight / 2
	}
	if maxH < 4 {
		maxH = 4
	}
	if spec.Height < 4 || spec.Height > maxH {
		return fmt.Errorf("overlay_height must be between 4 and %d", maxH)
	}
	if spec.SpeedPx < 0 || spec.SpeedPx > 200 {
		return errors.New("overlay_speed_px must be between 0 and 200")
	}
	if !isValidHexColor(spec.Background) {
		return errors.New("overlay_bg must be a #rrggbb hex color")
	}
	if !isValidHexColor(spec.Foreground) {
		return errors.New("overlay_fg must be a #rrggbb hex color")
	}
	return nil
}

// CompositeOverlayPNG decodes a PNG content frame, draws the overlay strip, and
// re-encodes. It returns the input slice unchanged when the overlay is disabled
// or empty so disabled devices stay byte-identical. now drives the deterministic
// marquee offset (see ScrollOffset).
func CompositeOverlayPNG(data []byte, spec OverlaySpec, now time.Time) ([]byte, error) {
	if !spec.Enabled || spec.Text == "" {
		return data, nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)

	h := spec.Height
	if h <= 0 {
		h = DefaultOverlaySpec().Height
	}
	if h > b.Dy() {
		h = b.Dy()
	}
	y := b.Dy() - h
	if spec.Position == "top" {
		y = 0
	}

	bg := parseHexColor(spec.Background, color.RGBA{0, 0, 0, 255})
	fg := parseHexColor(spec.Foreground, color.RGBA{255, 255, 255, 255})

	fillRect(rgba, b.Min.X, b.Min.Y+y, b.Max.X-1, b.Min.Y+y+h-1, bg)

	textY := b.Min.Y + y + (h-simpleCharH)/2
	if textY < b.Min.Y+y {
		textY = b.Min.Y + y
	}

	textW := len([]rune(spec.Text)) * simpleCharW
	if spec.SpeedPx <= 0 {
		// Static, left-aligned, clipped at the strip's right edge.
		drawStringSimple(rgba, spec.Text, b.Min.X, textY, fg)
	} else {
		gap := simpleCharW * 3
		total := textW + gap
		if total > 0 {
			offset := ScrollOffset(now, 100, spec.SpeedPx, textW, gap)
			strip := image.NewRGBA(image.Rect(0, 0, total, simpleCharH))
			drawStringSimple(strip, spec.Text, 0, 0, fg)
			for dx := 0; dx < b.Dx(); dx++ {
				srcX := ((offset+dx)%total + total) % total
				for row := 0; row < simpleCharH; row++ {
					c := strip.RGBAAt(srcX, row)
					if c.A == 0 {
						continue
					}
					tx := b.Min.X + dx
					ty := textY + row
					if tx >= b.Min.X && tx < b.Max.X && ty >= b.Min.Y && ty < b.Max.Y {
						rgba.Set(tx, ty, c)
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
