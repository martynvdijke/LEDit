package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"strings"
)

// PixelFrameDoc is the JSON document stored on a PixelArt entity: a shared
// palette plus one or more frames of row-major palette indices. An index of
// -1 (or the configured Transparent index) means transparent, which renders
// as the background color.
type PixelFrameDoc struct {
	Palette     []string     `json:"palette"`
	Transparent int          `json:"transparent,omitempty"` // index treated as transparent; -1 default
	Background  string       `json:"background,omitempty"`  // hex letterbox/background color
	Frames      []PixelFrame `json:"frames"`
}

// PixelFrame is a single animation frame.
type PixelFrame struct {
	Duration int   `json:"duration"` // milliseconds
	Pixels   []int `json:"pixels"`   // row-major palette indices
}

// PixelOverlay is a text label drawn over the composed artwork after scaling,
// so text stays crisp at device resolution.
type PixelOverlay struct {
	Text     string  `json:"text"`
	X        int     `json:"x"`
	Y        int     `json:"y"`
	Color    string  `json:"color"`
	FontSize float64 `json:"font_size"`
}

// MaxPixelGrid bounds artwork dimensions to keep documents and renders sane.
const MaxPixelGrid = 128

// MaxPixelFrames bounds the number of frames per artwork.
const MaxPixelFrames = 64

// ParsePixelFrames parses a frames JSON document and validates it against the
// given grid dimensions (pass 0 for both to skip the pixel-count check).
func ParsePixelFrames(raw string, gridW, gridH int) (PixelFrameDoc, error) {
	var doc PixelFrameDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return doc, fmt.Errorf("frames is not valid JSON: %w", err)
	}
	if err := ValidatePixelDoc(doc, gridW, gridH); err != nil {
		return doc, err
	}
	return doc, nil
}

// ValidatePixelDoc checks palette/pixel invariants: every frame must cover
// exactly gridW*gridH cells with indices in [-1, len(palette)-1] (or the
// transparent index), and durations must be positive.
func ValidatePixelDoc(doc PixelFrameDoc, gridW, gridH int) error {
	if len(doc.Frames) == 0 {
		return fmt.Errorf("at least one frame is required")
	}
	if len(doc.Frames) > MaxPixelFrames {
		return fmt.Errorf("too many frames (max %d)", MaxPixelFrames)
	}
	for i, c := range doc.Palette {
		if strings.HasPrefix(c, "@") {
			// Slot marker (e.g. "@gauge"): color resolved at render time from
			// API bindings; only the name must be non-empty.
			if len(c) < 2 {
				return fmt.Errorf("palette[%d] %q has an empty slot name", i, c)
			}
			continue
		}
		if len(c) < 6 {
			return fmt.Errorf("palette[%d] %q is not a hex color", i, c)
		}
	}
	area := gridW * gridH
	for i, f := range doc.Frames {
		if f.Duration <= 0 {
			return fmt.Errorf("frame %d duration must be > 0ms", i)
		}
		if area > 0 && len(f.Pixels) != area {
			return fmt.Errorf("frame %d has %d pixels, want %d (%dx%d)", i, len(f.Pixels), area, gridW, gridH)
		}
		for _, p := range f.Pixels {
			if p == doc.Transparent || p == -1 {
				continue
			}
			if p < 0 || p >= len(doc.Palette) {
				return fmt.Errorf("frame %d has out-of-range pixel index %d", i, p)
			}
		}
	}
	return nil
}

// RenderPixelFramesGrid renders frame frameIdx of doc onto a width×height
// canvas: integer nearest-neighbor scaling with centering, letterboxing
// transparent cells with the background color, then drawing overlays. Grids
// larger than the canvas are center-cropped with a warning.
func RenderPixelFramesGrid(doc PixelFrameDoc, frameIdx, gridW, gridH, width, height int, overlays []PixelOverlay) (*RenderedImage, error) {
	if len(doc.Frames) == 0 {
		return nil, fmt.Errorf("no frames to render")
	}
	if gridW <= 0 || gridH <= 0 || gridW > MaxPixelGrid || gridH > MaxPixelGrid {
		return nil, fmt.Errorf("invalid grid dimensions %dx%d", gridW, gridH)
	}
	if frameIdx < 0 || frameIdx >= len(doc.Frames) {
		frameIdx = 0
	}
	frame := doc.Frames[frameIdx]
	if len(frame.Pixels) != gridW*gridH {
		return nil, fmt.Errorf("frame has %d pixels, want %d", len(frame.Pixels), gridW*gridH)
	}

	bg := parseHexColor(doc.Background, color.RGBA{0, 0, 0, 255})
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	palette := make([]color.RGBA, len(doc.Palette))
	for i, hex := range doc.Palette {
		palette[i] = parseHexColor(hex, bg)
	}

	scale := min(width/gridW, height/gridH)
	if scale < 1 {
		scale = 1
	}
	drawW, drawH := gridW*scale, gridH*scale
	offX, offY := (width-drawW)/2, (height-drawH)/2
	if drawW > width || drawH > height {
		slog.Warn("pixel art larger than canvas, center-cropping",
			"source", "pixelart", "grid", fmt.Sprintf("%dx%d", gridW, gridH),
			"canvas", fmt.Sprintf("%dx%d", width, height))
	}

	for y := 0; y < gridH; y++ {
		for x := 0; x < gridW; x++ {
			idx := frame.Pixels[y*gridW+x]
			if idx == doc.Transparent || idx < 0 || idx >= len(palette) {
				continue
			}
			x1, y1 := max(offX+x*scale, 0), max(offY+y*scale, 0)
			x2, y2 := min(offX+(x+1)*scale-1, width-1), min(offY+(y+1)*scale-1, height-1)
			if x1 > x2 || y1 > y2 {
				continue // fully cropped
			}
			fillRect(img, x1, y1, x2, y2, palette[idx])
		}
	}

	for _, ov := range overlays {
		if ov.Text == "" {
			continue
		}
		col := parseHexColor(ov.Color, color.RGBA{255, 255, 255, 255})
		size := ov.FontSize
		if size <= 0 {
			size = 12
		}
		face, err := loadFont("fonts/PixelifySans.ttf", size)
		if err != nil {
			drawStringSimple(img, ov.Text, ov.X, ov.Y, col)
			continue
		}
		drawString(img, ov.Text, ov.X, ov.Y, face, col)
		face.Close()
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
