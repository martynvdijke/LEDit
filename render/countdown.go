package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"time"
)

// Countdown palette.
var (
	countdownBG     = color.RGBA{40, 42, 54, 255}    // #282a36
	countdownText   = color.RGBA{139, 233, 253, 255} // #8be9fd
	countdownAccent = color.RGBA{80, 250, 123, 255}  // #50fa7b
	countdownDone   = color.RGBA{255, 121, 198, 255} // #ff79c6
)

// formatCountdown renders a remaining duration as a compact countdown string:
//
//	>= 24h   -> "2d 14:33:07"
//	>= 1h    -> "14:33:07"
//	<  1h    -> "33:07"
//	expired  -> "DONE"
func formatCountdown(remaining time.Duration) string {
	if remaining <= 0 {
		return "DONE"
	}
	secs := int(remaining / time.Second)
	if secs >= 24*3600 {
		days := secs / (24 * 3600)
		secs %= 24 * 3600
		return fmt.Sprintf("%dd %02d:%02d:%02d", days, secs/3600, (secs%3600)/60, secs%60)
	}
	if secs >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
	}
	return fmt.Sprintf("%02d:%02d", secs/60, secs%60)
}

// RenderCountdown renders a countdown timer: an optional label on top and the
// remaining time below, using the 5x7 pixel font. When the target has passed
// the display shows a blinking "DONE".
func RenderCountdown(label string, target, now time.Time, width, height int) (*RenderedImage, error) {
	if width < 8 {
		width = 8
	}
	if height < 8 {
		height = 8
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{countdownBG}, image.Point{}, draw.Src)

	remaining := target.Sub(now)
	text := formatCountdown(remaining)
	done := remaining <= 0

	col := countdownText
	if done {
		col = countdownDone
		// Blink DONE on alternating seconds.
		if now.Second()%2 == 1 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				return nil, err
			}
			return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
		}
	}

	// Layout: label (if any) centered near the top, time centered in the
	// remaining space. Small canvases drop the label to keep the time legible.
	centerX := func(s string) int {
		return max(0, (width-len(s)*6)/2)
	}

	y := 2
	if label != "" && height >= 24 {
		drawStringSimple(img, label, centerX(label), y, countdownAccent)
		y += 10
	}

	if done && height < 24 {
		// DONE centered vertically on tiny canvases.
		y = max(0, (height-7)/2)
	}
	drawStringSimple(img, text, centerX(text), y, col)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
