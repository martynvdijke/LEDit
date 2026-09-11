package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	stddraw "image/draw"

	xdraw "golang.org/x/image/draw"
)

// NowPlayingParams holds the inputs for a now-playing render.
type NowPlayingParams struct {
	Track        string
	Artist       string
	Album        string
	Position     int // seconds elapsed
	Duration     int // seconds total
	State        string
	ShowAlbumArt bool
	ArtURL       string
	ArtToken     string // optional token for provider-authenticated art fetches
	Width        int
	Height       int
	Theme        Theme
}

const (
	nowPlayingArtMaxBytes = 8 << 20 // 8 MiB guard for album art
	nowPlayingLineH       = simpleCharH + 2
)

var nowPlayingHTTPClient = &http.Client{Timeout: 10 * time.Second}

var nowPlayingDefaultTheme = Theme{
	Name:            "cyber",
	BackgroundColor: [3]uint8{40, 42, 54},
	AccentColor:     [3]uint8{80, 250, 123},
	TextColor:       [3]uint8{139, 233, 253},
	Title:           "NOW PLAYING",
	FontSize:        24,
}

// RenderNowPlaying renders normalized now-playing metadata to a width x height
// PNG: track/artist/album, elapsed/total, a proportional progress bar, and
// optional downscaled album art. It never fails because of album art; missing
// or unreadable art simply falls back to a full-width text layout.
func RenderNowPlaying(p NowPlayingParams) (*RenderedImage, error) {
	if p.Width <= 0 || p.Height <= 0 {
		p.Width, p.Height = 64, 64
	}
	if p.Theme.Name == "" {
		p.Theme = nowPlayingDefaultTheme
	}
	bg := color.RGBA{p.Theme.BackgroundColor[0], p.Theme.BackgroundColor[1], p.Theme.BackgroundColor[2], 255}
	textCol := color.RGBA{p.Theme.TextColor[0], p.Theme.TextColor[1], p.Theme.TextColor[2], 255}
	accent := color.RGBA{p.Theme.AccentColor[0], p.Theme.AccentColor[1], p.Theme.AccentColor[2], 255}

	img := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	stddraw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, stddraw.Src)

	// Progress bar: drawn when there is a duration and vertical room.
	barH := 0
	if p.Duration > 0 && p.Height >= 16 {
		barH = p.Height / 8
		if barH < 3 {
			barH = 3
		}
		if barH > 8 {
			barH = 8
		}
	}
	contentH := p.Height - barH

	// Album art earns space only when enabled, fetchable, and the canvas can
	// still fit a text column (text > bar > art priority).
	artSize, art := 0, image.Image(nil)
	if p.ShowAlbumArt && p.ArtURL != "" {
		if s := artRegionSize(p, contentH); s > 0 {
			if got := fetchArt(p.ArtURL, p.ArtToken); got != nil {
				artSize, art = s, got
			}
		}
	}

	textX := 2
	if artSize > 0 {
		drawArt(img, art, 2, 2, artSize)
		textX = 2 + artSize + 2
	}
	textW := p.Width - textX - 2
	if textW < 1 {
		textW = p.Width - 2
		textX = 2
	}

	y := 2
	for _, line := range nowPlayingLines(p) {
		if y+simpleCharH > contentH {
			break
		}
		drawStringSimple(img, truncateToWidth(line, textW), textX, y, textCol)
		y += nowPlayingLineH
	}

	if barH > 0 {
		barY := p.Height - barH
		dim := color.RGBA{textCol.R / 3, textCol.G / 3, textCol.B / 3, 255}
		fillRect(img, 0, barY, p.Width-1, p.Height-1, dim)
		fw := int(float64(p.Width) * progressFraction(p.Position, p.Duration))
		if fw > 0 {
			fillRect(img, 0, barY, fw-1, p.Height-1, accent)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

// artRegionSize returns the square art edge length for the canvas, or 0 when
// there is not enough room for art plus a minimal text column.
func artRegionSize(p NowPlayingParams, contentH int) int {
	size := contentH - 2
	if half := p.Width / 2; size > half {
		size = half
	}
	if size < 16 || p.Width < 40 || contentH < 20 {
		return 0
	}
	return size
}

// nowPlayingLines builds the text lines in display order, skipping empties.
func nowPlayingLines(p NowPlayingParams) []string {
	var lines []string
	if p.Track != "" {
		lines = append(lines, p.Track)
	}
	if p.Artist != "" {
		lines = append(lines, p.Artist)
	}
	if p.Album != "" {
		lines = append(lines, p.Album)
	}
	if p.Duration > 0 || p.Position > 0 {
		lines = append(lines, formatMMSS(p.Position)+" / "+formatMMSS(p.Duration))
	}
	if len(lines) == 0 {
		lines = append(lines, "Nothing playing")
	}
	return lines
}

// fetchArt downloads and decodes album art, returning nil on any failure.
func fetchArt(url, token string) image.Image {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	resp, err := nowPlayingHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	decoded, _, err := image.Decode(io.LimitReader(resp.Body, nowPlayingArtMaxBytes))
	if err != nil {
		return nil
	}
	return decoded
}

// drawArt scales src to fit inside the square region at (x,y,size) preserving
// aspect ratio, and centers it.
func drawArt(dst *image.RGBA, src image.Image, x, y, size int) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	scale := float64(size) / float64(sw)
	if s := float64(size) / float64(sh); s < scale {
		scale = s
	}
	dw, dh := int(float64(sw)*scale), int(float64(sh)*scale)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	ox := x + (size-dw)/2
	oy := y + (size-dh)/2
	xdraw.CatmullRom.Scale(dst, image.Rect(ox, oy, ox+dw, oy+dh), src, b, stddraw.Over, nil)
}

// progressFraction returns clamp(position/duration, 0, 1); 0 when duration<=0.
func progressFraction(position, duration int) float64 {
	if duration <= 0 {
		return 0
	}
	f := float64(position) / float64(duration)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// formatMMSS formats seconds as mm:ss, clamping negatives to zero.
func formatMMSS(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

// truncateToWidth cuts s to fit width pixels of the simple bitmap font,
// appending "~" when characters were dropped.
func truncateToWidth(s string, width int) string {
	max := width / simpleCharW
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return string(runes[:1])
	}
	return strings.TrimRight(string(runes[:max-1]), " ") + "~"
}
