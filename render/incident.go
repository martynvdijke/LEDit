package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"time"
)

// IncidentScene is the display payload for an active monitoring incident.
// More is the number of additional active incidents not shown.
type IncidentScene struct {
	Title    string
	Message  string
	Severity string
	Since    time.Time
	More     int
}

// incidentPalette maps a severity to the scene background, body text, and
// accent (title/border) colours.
func incidentPalette(severity string) (bg, fg, accent color.RGBA) {
	switch severity {
	case "critical":
		return color.RGBA{0x50, 0x00, 0x00, 0xFF}, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, color.RGBA{0xFF, 0x3B, 0x30, 0xFF}
	case "info":
		return color.RGBA{0x00, 0x24, 0x3A, 0xFF}, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, color.RGBA{0x40, 0xB0, 0xFF, 0xFF}
	default: // warning
		return color.RGBA{0x3A, 0x24, 0x00, 0xFF}, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, color.RGBA{0xFF, 0xB0, 0x20, 0xFF}
	}
}

// IncidentPNG renders an incident takeover frame for the given canvas size.
// Deterministic for a given scene and timestamp.
func IncidentPNG(width, height int, sc IncidentScene, now time.Time) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("incident render: invalid canvas %dx%d", width, height)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg, fg, accent := incidentPalette(sc.Severity)
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	bw := 2
	if width < 24 || height < 24 {
		bw = 1
	}
	fillRect(img, 0, 0, width-1, bw-1, accent)
	fillRect(img, 0, height-bw, width-1, height-1, accent)
	fillRect(img, 0, 0, bw-1, height-1, accent)
	fillRect(img, width-bw, 0, width-1, height-1, accent)

	pad := bw + 3
	inner := width - 2*pad
	if inner < simpleCharW*4 {
		pad = 0
		inner = width
	}

	footer := incidentFooter(sc, now)

	titleSize := float64(height) / 4
	if titleSize < 8 {
		titleSize = 8
	}
	bodySize := float64(height) / 6
	if bodySize < 7 {
		bodySize = 7
	}

	titleFace, titleErr := loadFont("fonts/PixelifySans.ttf", titleSize)
	bodyFace, bodyErr := loadFont("fonts/PixelifySans.ttf", bodySize)
	if titleErr != nil || bodyErr != nil {
		// No usable font: single block of 5x7 text, wrapped by character count.
		text := sc.Title + "\n" + sc.Message
		if footer != "" {
			text += "\n" + footer
		}
		drawStringSimple(img, wrapSimple(text, inner/simpleCharW), pad, pad+simpleCharH, fg)
		return encodePNG(img)
	}
	defer titleFace.Close()
	defer bodyFace.Close()

	footerH := 0
	if footer != "" {
		footerH = int(bodySize) + 4
	}

	y := pad + int(titleSize)
	for _, line := range wordWrap(sc.Title, titleFace, inner) {
		drawString(img, line, pad, y, titleFace, accent)
		y += int(titleSize) + 2
	}
	y += 3

	lineH := int(bodySize) + 2
	limit := height - pad - footerH
	for _, line := range wordWrap(sc.Message, bodyFace, inner) {
		if y > limit {
			break
		}
		drawString(img, line, pad, y, bodyFace, fg)
		y += lineH
	}

	if footer != "" {
		drawString(img, footer, pad, height-pad, bodyFace, fg)
	}
	return encodePNG(img)
}

// incidentFooter builds the "since HH:MM  +N more" line, or "" when empty.
func incidentFooter(sc IncidentScene, now time.Time) string {
	footer := ""
	if !sc.Since.IsZero() {
		footer = "since " + sc.Since.Local().Format("15:04")
	}
	if sc.More > 0 {
		if footer != "" {
			footer += "  "
		}
		footer += fmt.Sprintf("+%d more", sc.More)
	}
	return footer
}

// wrapSimple inserts newlines so no line exceeds maxChars (used with the 5x7
// fallback font, which cannot measure text). Existing newlines are preserved.
func wrapSimple(text string, maxChars int) string {
	if maxChars < 1 {
		maxChars = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			switch {
			case line == "":
				line = w
			case len(line)+1+len(w) <= maxChars:
				line += " " + w
			default:
				out = append(out, line)
				line = w
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func encodePNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
