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
	"unicode/utf8"
)

// Countdown palette.
var (
	countdownBG     = color.RGBA{40, 42, 54, 255}    // #282a36
	countdownText   = color.RGBA{139, 233, 253, 255} // #8be9fd
	countdownAccent = color.RGBA{80, 250, 123, 255}  // #50fa7b
	countdownDone   = color.RGBA{255, 121, 198, 255} // #ff79c6
)

// Canonical countdown option values.
const (
	countdownGranularitySeconds = "seconds"
	countdownGranularityMinutes = "minutes"
	countdownGranularityHours   = "hours"
	countdownGranularityDays    = "days"

	countdownDirectionDown = "down"
	countdownDirectionUp   = "up"

	countdownCompletionNow     = "now"
	countdownCompletionMessage = "message"
	countdownCompletionHide    = "hide"
)

// CountdownOptions carries the configurable countdown rendering behavior.
type CountdownOptions struct {
	Granularity string
	Direction   string
	Completion  string
	Message     string
}

// DefaultCountdownOptions returns the backward-compatible defaults.
func DefaultCountdownOptions() CountdownOptions {
	return CountdownOptions{
		Granularity: countdownGranularitySeconds,
		Direction:   countdownDirectionDown,
		Completion:  countdownCompletionNow,
	}
}

// granularityIndex maps a granularity to its component index in
// [days, hours, minutes, seconds].
func granularityIndex(g string) int {
	switch g {
	case countdownGranularityDays:
		return 0
	case countdownGranularityHours:
		return 1
	case countdownGranularityMinutes:
		return 2
	default:
		return 3
	}
}

// coarserGranularity drops the smallest displayed unit.
func coarserGranularity(g string) (string, bool) {
	switch g {
	case countdownGranularitySeconds:
		return countdownGranularityMinutes, true
	case countdownGranularityMinutes:
		return countdownGranularityHours, true
	case countdownGranularityHours:
		return countdownGranularityDays, true
	default:
		return g, false
	}
}

// formatCountdown renders a duration using granularity as the smallest unit
// displayed. Components larger than the largest non-zero unit are omitted and
// finer-than-granularity units are floored. With seconds granularity the
// default "MM:SS" / "HH:MM:SS" / "Xd HH:MM:SS" shapes are preserved.
func formatCountdown(d time.Duration, granularity string) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	comps := [4]int64{
		total / 86400,
		(total % 86400) / 3600,
		(total % 3600) / 60,
		total % 60,
	}
	granIdx := granularityIndex(granularity)

	hi := -1
	for i, c := range comps {
		if c != 0 {
			hi = i
			break
		}
	}
	if hi == -1 || hi > granIdx {
		hi = granIdx
	}
	// Seconds granularity always shows at least MM:SS.
	if granIdx == 3 && hi == 3 {
		hi = 2
	}

	var b strings.Builder
	if hi == 0 {
		fmt.Fprintf(&b, "%dd", comps[0])
		if granIdx == 0 {
			return b.String()
		}
		b.WriteByte(' ')
		hi = 1
	}
	for i := hi; i <= granIdx; i++ {
		if i > hi {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%02d", comps[i])
	}
	return b.String()
}

// countdownLineWidth returns the pixel width of a single scaled line of text
// drawn with the 6x7-cell bitmap font.
func countdownLineWidth(text string, scale int) int {
	return utf8.RuneCountInString(text) * 6 * scale
}

// countdownFittingText returns the time string at the configured granularity,
// dropping the smallest unit(s) until it fits the width at scale 1 (or days is
// reached).
func countdownFittingText(d time.Duration, granularity string, width int) string {
	g := granularity
	for {
		text := formatCountdown(d, g)
		if countdownLineWidth(text, 1) <= width || g == countdownGranularityDays {
			return text
		}
		next, ok := coarserGranularity(g)
		if !ok {
			return text
		}
		g = next
	}
}

// drawStringScaled draws text with the 5x7 bitmap font scaled by an integer
// factor using fillRect, keeping pixels crisp (no anti-aliasing).
func drawStringScaled(img *image.RGBA, text string, x, y, scale int, col color.Color) {
	cx := x
	for _, r := range text {
		idx := int(r - 32)
		if idx < 0 || idx >= len(simpleFont) {
			cx += 6 * scale
			continue
		}
		glyph := simpleFont[idx]
		for row := range 7 {
			for bit := range 5 {
				if glyph[row][bit] == 1 {
					x0 := cx + bit*scale
					y0 := y + row*scale
					fillRect(img, x0, y0, x0+scale-1, y0+scale-1, col)
				}
			}
		}
		cx += 6 * scale
	}
}

// countdownScale picks the largest integer glyph scale that fits text within
// width and availH pixels; it never returns less than 1.
func countdownScale(text string, width, availH int) int {
	lineW := countdownLineWidth(text, 1)
	scale := width / lineW
	if s := availH / 7; s < scale {
		scale = s
	}
	if scale < 1 {
		scale = 1
	}
	if scale > 16 {
		scale = 16
	}
	return scale
}

// countdownState resolves the string to draw for the current state. ok is false
// when only the background should be drawn (hidden completion or the off phase
// of the Now! blink).
func countdownState(dur time.Duration, now time.Time, width int, opts CountdownOptions) (text string, ok bool) {
	switch opts.Direction {
	case countdownDirectionUp:
		if dur < 0 {
			dur = 0
		}
		return countdownFittingText(dur, opts.Granularity, width), true
	default: // down
		if dur > 0 {
			return countdownFittingText(dur, opts.Granularity, width), true
		}
		switch opts.Completion {
		case countdownCompletionHide:
			return "", false
		case countdownCompletionMessage:
			if opts.Message == "" {
				return "Now!", true
			}
			return opts.Message, true
		default: // now
			if now.Second()%2 == 1 {
				return "", false
			}
			return "Now!", true
		}
	}
}

// RenderCountdown renders a countdown with the default options.
func RenderCountdown(label string, target, now time.Time, width, height int) (*RenderedImage, error) {
	return RenderCountdownWithOptions(label, target, now, width, height, DefaultCountdownOptions())
}

// RenderCountdownWithOptions renders a countdown/elapsed timer: an optional
// label above the time, using configurable granularity, direction, and
// completion behavior. The text is integer-scaled to the device resolution and
// falls back to coarser units when it cannot fit.
func RenderCountdownWithOptions(label string, target, now time.Time, width, height int, opts CountdownOptions) (*RenderedImage, error) {
	if opts.Granularity == "" {
		opts.Granularity = countdownGranularitySeconds
	}
	if opts.Direction == "" {
		opts.Direction = countdownDirectionDown
	}
	if opts.Completion == "" {
		opts.Completion = countdownCompletionNow
	}
	if width < 8 {
		width = 8
	}
	if height < 8 {
		height = 8
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{countdownBG}, image.Point{}, draw.Src)
	encode := func() (*RenderedImage, error) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
	}

	dur := target.Sub(now)
	text, ok := countdownState(dur, now, width, opts)
	if !ok {
		return encode()
	}
	col := countdownText
	if opts.Direction != countdownDirectionUp && dur <= 0 {
		col = countdownDone
	}

	// Reserve room for the label only when both lines fit vertically.
	hasLabel := label != ""
	if hasLabel && height < 7+2+7 {
		hasLabel = false
	}
	availH := height
	if hasLabel {
		availH = height - (7 + 2)
	}

	scale := countdownScale(text, width, availH)
	timeH := 7 * scale
	totalH := timeH
	if hasLabel {
		totalH += 7 + 2
	}
	y := (height - totalH) / 2
	if y < 0 {
		y = 0
	}

	if hasLabel {
		lx := (width - countdownLineWidth(label, 1)) / 2
		if lx < 0 {
			lx = 0
		}
		drawStringSimple(img, label, lx, y, countdownAccent)
		y += 7 + 2
	}

	tx := (width - countdownLineWidth(text, scale)) / 2
	if tx < 0 {
		tx = 0
	}
	drawStringScaled(img, text, tx, y, scale, col)

	return encode()
}
