// Package effects provides a deterministic, theme-palette driven effect engine
// for rendering animated LED matrix art.
//
// The package is intentionally a leaf: it imports only the standard library so
// that every other rendering package (and its tests) can use it without pulling
// in datasource or handler code. Randomness is derived from a seeded hash of
// (effect, seed, tick, pixel) rather than the global math/rand source, and the
// wall clock is never read inside Draw, so a frame is a pure function of its
// arguments.
package effects

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strconv"
	"time"
)

// TickInterval is the shared animation quantum (about 15 frames per second)
// used to quantize elapsed time before rendering. Keeping every caller on the
// same quantum makes a frame a pure function of (effect, params, seed, tick)
// and gives the datasource Animator contract a single cadence.
const TickInterval = 66 * time.Millisecond

// ParamsSchemaVersion is the version stamped into serialized parameter
// documents. Readers accept older versions and ignore unknown future fields.
const ParamsSchemaVersion = 1

// Direction values accepted by every effect.
const (
	DirectionUp    = "up"
	DirectionDown  = "down"
	DirectionLeft  = "left"
	DirectionRight = "right"
	DirectionNone  = "none"
)

// ValidDirections returns the closed set of direction values. A fresh slice is
// returned on every call so callers cannot mutate the shared definition.
func ValidDirections() []string {
	return []string{DirectionUp, DirectionDown, DirectionLeft, DirectionRight, DirectionNone}
}

// DirectionValid reports whether dir is empty (unset) or a member of the
// closed direction set.
func DirectionValid(dir string) bool {
	if dir == "" {
		return true
	}
	for _, d := range ValidDirections() {
		if d == dir {
			return true
		}
	}
	return false
}

// Palette holds the theme roles consumed by effects. Effects never contain
// literal brand colors: the palette is always supplied by the caller from the
// effective theme.
type Palette struct {
	Background color.RGBA
	Accent     color.RGBA
	Text       color.RGBA
}

// DefaultPalette matches the built-in dark theme.
func DefaultPalette() Palette {
	return Palette{
		Background: color.RGBA{R: 0x28, G: 0x2a, B: 0x36, A: 255},
		Accent:     color.RGBA{R: 0x50, G: 0xfa, B: 0x7b, A: 255},
		Text:       color.RGBA{R: 0x8b, G: 0xe9, B: 0xfd, A: 255},
	}
}

// PaletteFromRGB builds a palette from raw theme color triples.
func PaletteFromRGB(bg, accent, text [3]uint8) Palette {
	return Palette{
		Background: color.RGBA{R: bg[0], G: bg[1], B: bg[2], A: 255},
		Accent:     color.RGBA{R: accent[0], G: accent[1], B: accent[2], A: 255},
		Text:       color.RGBA{R: text[0], G: text[1], B: text[2], A: 255},
	}
}

// IsZero reports whether no palette colors are set (a behaviour-only preset).
func (p Palette) IsZero() bool {
	return p == Palette{}
}

// Params is the versioned, bounded parameter document for a single effect.
// Numeric fields are percentages in the 0..100 range; Seed 0 means "derive a
// stable seed from the effect and its parameters".
type Params struct {
	Palette   Palette
	Speed     float64
	Intensity float64
	Density   float64
	Direction string
	Seed      int64
}

// StripPalette clears the palette so only behaviour is persisted with a preset.
func (p Params) StripPalette() Params {
	p.Palette = Palette{}
	return p
}

// Clone returns an independent copy of the parameter document.
func (p Params) Clone() Params { return p }

type paletteDoc struct {
	Background *string `json:"background,omitempty"`
	Accent     *string `json:"accent,omitempty"`
	Text       *string `json:"text,omitempty"`
}

type paramsDoc struct {
	Version   int         `json:"v"`
	Palette   *paletteDoc `json:"palette,omitempty"`
	Speed     float64     `json:"speed"`
	Intensity float64     `json:"intensity"`
	Density   float64     `json:"density"`
	Direction string      `json:"direction"`
	Seed      int64       `json:"seed"`
}

// MarshalJSON writes the versioned parameter document. A zero palette is
// omitted entirely so behaviour-only presets stay byte stable.
func (p Params) MarshalJSON() ([]byte, error) {
	doc := paramsDoc{
		Version:   ParamsSchemaVersion,
		Speed:     p.Speed,
		Intensity: p.Intensity,
		Density:   p.Density,
		Direction: p.Direction,
		Seed:      p.Seed,
	}
	if !p.Palette.IsZero() {
		bg, accent, text := formatHexColor(p.Palette.Background), formatHexColor(p.Palette.Accent), formatHexColor(p.Palette.Text)
		doc.Palette = &paletteDoc{Background: &bg, Accent: &accent, Text: &text}
	}
	return json.Marshal(doc)
}

// UnmarshalJSON reads a versioned parameter document. Older documents are
// upgraded in place and unknown fields are ignored; a document written by a
// newer schema version is rejected so callers never guess at semantics.
func (p *Params) UnmarshalJSON(data []byte) error {
	var doc paramsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Version > ParamsSchemaVersion {
		return fmt.Errorf("unsupported effect params version %d", doc.Version)
	}
	out := Params{
		Speed:     doc.Speed,
		Intensity: doc.Intensity,
		Density:   doc.Density,
		Direction: doc.Direction,
		Seed:      doc.Seed,
	}
	if doc.Palette != nil && doc.Palette.Background != nil {
		c, err := parseHexColor(*doc.Palette.Background)
		if err != nil {
			return fmt.Errorf("palette.background: %w", err)
		}
		out.Palette.Background = c
	}
	if doc.Palette != nil && doc.Palette.Accent != nil {
		c, err := parseHexColor(*doc.Palette.Accent)
		if err != nil {
			return fmt.Errorf("palette.accent: %w", err)
		}
		out.Palette.Accent = c
	}
	if doc.Palette != nil && doc.Palette.Text != nil {
		c, err := parseHexColor(*doc.Palette.Text)
		if err != nil {
			return fmt.Errorf("palette.text: %w", err)
		}
		out.Palette.Text = c
	}
	*p = out
	return nil
}

// MarshalParams serializes a parameter document with the schema version.
func MarshalParams(p Params) ([]byte, error) { return json.Marshal(p) }

// String renders a compact, deterministic signature of the behaviour
// parameters (palette excluded, since the palette comes from the theme and is
// already part of the frame cache key). Used for LKG cache keys.
func (p Params) String() string {
	return fmt.Sprintf("%.2f|%.2f|%.2f|%s|%d", p.Speed, p.Intensity, p.Density, p.Direction, p.Seed)
}

// UnmarshalParams parses a parameter document, tolerating an empty input by
// returning zero parameters.
func UnmarshalParams(data []byte) (Params, error) {
	var p Params
	if len(trimSpace(data)) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return Params{}, err
	}
	return p, nil
}

func trimSpace(data []byte) []byte {
	start, end := 0, len(data)
	for start < end && isSpace(data[start]) {
		start++
	}
	for end > start && isSpace(data[end-1]) {
		end--
	}
	return data[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func formatHexColor(c color.RGBA) string {
	return "#" + hexPair(c.R) + hexPair(c.G) + hexPair(c.B)
}

func hexPair(v uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4], digits[v&0x0f]})
}

func parseHexColor(raw string) (color.RGBA, error) {
	s := raw
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, fmt.Errorf("invalid color %q", raw)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid color %q", raw)
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}
