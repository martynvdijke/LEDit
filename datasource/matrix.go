package datasource

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
	"sync"
	"time"

	"ledit/render"
)

// CellTheme holds optional per-cell theme overrides. Missing/empty fields
// fall back to the layout theme at render time.
type CellTheme struct {
	Accent     string  `json:"accent,omitempty"`
	Text       string  `json:"text,omitempty"`
	Background string  `json:"background,omitempty"`
	FontSize   float64 `json:"font_size,omitempty"`
}

// Validate reports whether the theme overrides are well-formed. Every
// non-empty color field must look like a hex color (#rrggbb or rrggbb);
// FontSize if non-zero must be >0 and <=100.
func (t *CellTheme) Validate() bool {
	if t == nil {
		return true
	}
	for _, c := range []string{t.Accent, t.Text, t.Background} {
		if c != "" && !isValidHexColor(c) {
			return false
		}
	}
	if t.FontSize != 0 && (t.FontSize <= 0 || t.FontSize > 100) {
		return false
	}
	return true
}

func isValidHexColor(s string) bool {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return false
	}
	for _, ch := range s {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

// PanelBinding maps one grid cell to a datasource by type and DB id.
type PanelBinding struct {
	Row        int        `json:"row"`
	Col        int        `json:"col"`
	SourceType string     `json:"source_type"`
	SourceID   int        `json:"source_id"`
	Theme      *CellTheme `json:"theme,omitempty"`
}

// MatrixDS is a composite datasource that renders a rows×cols grid of bound
// panel sources into a single image.
type MatrixDS struct {
	Name       string
	Rows       int
	Cols       int
	Gap        int
	Background string // hex color, e.g. "#282a36"
	Bindings   []PanelBinding
	// Resolve turns a binding into a concrete datasource. It may resolve
	// "matrix" source types to nested matrices; Depth guards recursion.
	Resolve func(sourceType string, sourceID int) (Datasource, string, error)
	Depth   int
}

// ParseBindings decodes a bindings JSON array, tolerating malformed input.
func ParseBindings(raw string) []PanelBinding {
	var out []PanelBinding
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		slog.Warn("invalid matrix bindings JSON", "error", err)
	}
	return out
}

// BindingsJSON serializes bindings into a JSON array string.
func BindingsJSON(bindings []PanelBinding) string {
	b, err := json.Marshal(bindings)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ValidBindings reports whether raw is a parseable bindings JSON array and
// every binding falls inside the given grid dimensions.
func ValidBindings(raw string, rows, cols int) bool {
	var bindings []PanelBinding
	if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
		return false
	}
	for _, b := range bindings {
		if b.Row < 0 || b.Row >= rows || b.Col < 0 || b.Col >= cols {
			return false
		}
		if b.Theme != nil && !b.Theme.Validate() {
			return false
		}
	}
	return true
}

func applyCellTheme(base render.Theme, ct *CellTheme) render.Theme {
	if ct == nil {
		return base
	}
	out := base
	if ct.Accent != "" {
		c := parseHexColor(ct.Accent, color.RGBA{base.AccentColor[0], base.AccentColor[1], base.AccentColor[2], 255})
		out.AccentColor = [3]uint8{c.R, c.G, c.B}
	}
	if ct.Text != "" {
		c := parseHexColor(ct.Text, color.RGBA{base.TextColor[0], base.TextColor[1], base.TextColor[2], 255})
		out.TextColor = [3]uint8{c.R, c.G, c.B}
	}
	if ct.Background != "" {
		c := parseHexColor(ct.Background, color.RGBA{base.BackgroundColor[0], base.BackgroundColor[1], base.BackgroundColor[2], 255})
		out.BackgroundColor = [3]uint8{c.R, c.G, c.B}
	}
	if ct.FontSize > 0 {
		out.FontSize = ct.FontSize
	}
	return out
}

func (m *MatrixDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if m.Rows < 1 {
		m.Rows = 1
	}
	if m.Cols < 1 {
		m.Cols = 1
	}
	cellW, cellH := render.CellSize(m.Rows, m.Cols, m.Gap, width, height)

	theme := render.Theme{
		Name:            "cyber",
		BackgroundColor: [3]uint8{40, 42, 54},
		AccentColor:     [3]uint8{80, 250, 123},
		TextColor:       [3]uint8{139, 233, 253},
		Title:           strings.ToUpper(m.Name),
		FontSize:        0, // scaled per panel by RenderPanel
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := parseHexColor(m.Background, color.RGBA{theme.BackgroundColor[0], theme.BackgroundColor[1], theme.BackgroundColor[2], 255})
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	used := map[string]bool{}
	for _, b := range m.Bindings {
		key := fmt.Sprintf("%d:%d", b.Row, b.Col)
		if used[key] {
			continue
		}
		used[key] = true
		if b.Row < 0 || b.Row >= m.Rows || b.Col < 0 || b.Col >= m.Cols {
			// Out-of-range bindings are rejected on save; skip defensively.
			continue
		}
		x, y := render.PanelOrigin(b.Row, b.Col, m.Gap, cellW, cellH)

		var panel *render.RenderedImage
		if m.Resolve != nil {
			if src, _, err := m.Resolve(b.SourceType, b.SourceID); err == nil && src != nil {
				// Themed path takes precedence when binding has a theme.
				if b.Theme != nil {
					if tr, ok := src.(ThemedRenderer); ok {
						themed := applyCellTheme(theme, b.Theme)
						// Themed + ambient/scrolling sources bypass cache.
						if IsAmbient(src) {
							panel, _ = tr.GetPNGThemed(cellW, cellH, themed)
						} else {
							switch src.(type) {
							case *AnalogClockDS, *MatrixRainDS, *CountdownDS:
								panel, _ = tr.GetPNGThemed(cellW, cellH, themed)
							default:
								panel = cachedPanelGet(b.SourceType, b.SourceID, cellW, cellH, func() (*render.RenderedImage, error) {
									return tr.GetPNGThemed(cellW, cellH, themed)
								})
							}
						}
					}
				}
				if panel == nil {
					// Ambience sources are time-driven: always re-render so cells
					// animate instead of freezing for the panel cache TTL.
					if IsAmbient(src) {
						panel, _ = src.GetPNG(cellW, cellH)
					} else {
						switch src.(type) {
						case *AnalogClockDS, *MatrixRainDS, *CountdownDS:
							panel, _ = src.GetPNG(cellW, cellH)
						default:
							panel = cachedPanelGet(b.SourceType, b.SourceID, cellW, cellH, func() (*render.RenderedImage, error) {
								return src.GetPNG(cellW, cellH)
							})
						}
					}
				}
			}
		}
		if panel == nil {
			// Unbound or unresolved: empty themed cell (title-only).
			emptyTheme := applyCellTheme(theme, b.Theme)
			if m.Name == "" {
				emptyTheme.Title = "EMPTY"
			}
			panel, _ = render.RenderPanel(nil, cellW, cellH, emptyTheme, "fonts/PixelifySans.ttf")
		}
		drawPanel(canvas, panel, x, y, cellW, cellH)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

// drawPanel decodes the panel PNG and composites it at (x, y), scaled to fit
// the cell rect.
func drawPanel(canvas *image.RGBA, panel *render.RenderedImage, x, y, cellW, cellH int) {
	img, _, err := image.Decode(bytes.NewReader(panel.Data))
	if err != nil {
		return
	}
	dst := image.Rect(x, y, x+cellW, y+cellH)
	src := image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy())
	draw.Draw(canvas, dst, img, src.Min, draw.Src)
}

// parseHexColor converts "#rrggbb" to RGBA, falling back when malformed.
func parseHexColor(hex string, fallback color.RGBA) color.RGBA {
	if len(hex) < 6 {
		return fallback
	}
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return fallback
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		return fallback
	}
	return color.RGBA{r, g, b, 255}
}

// ---------------------------------------------------------------------------
// Panel render cache: protects upstream APIs from being hammered by the
// matrix feed and live previews. Keyed by source identity + panel size.
// ---------------------------------------------------------------------------

var (
	panelCacheTTL = 60 * time.Second

	panelCache = struct {
		sync.Mutex
		m map[string]panelCacheEntry
	}{m: map[string]panelCacheEntry{}}
)

type panelCacheEntry struct {
	img *render.RenderedImage
	at  time.Time
}

// PanelCacheHook is invoked on every panel cache access (true=hit,
// false=miss). It must be nil-safe; the handlers package wires it to health
// counters via init(). This indirection avoids an import cycle.
var PanelCacheHook func(hit bool)

// cachedPanelGet returns a cached panel render within the TTL, otherwise
// fetches (once) and stores it. Returns nil when fetch fails.
// Scrolling panels (Scrolls==true) bypass the cache entirely.
func cachedPanelGet(sourceType string, sourceID, w, h int, fetch func() (*render.RenderedImage, error)) *render.RenderedImage {
	key := fmt.Sprintf("%s:%d:%dx%d", sourceType, sourceID, w, h)
	panelCache.Lock()
	if e, ok := panelCache.m[key]; ok && time.Since(e.at) < panelCacheTTL {
		if e.img != nil && e.img.Scrolls {
			// Scrolling content must re-render; treat as miss.
			panelCache.Unlock()
			if PanelCacheHook != nil {
				PanelCacheHook(false)
			}
			img, err := fetch()
			if err != nil {
				slog.Warn("matrix panel render failed", "source_type", sourceType, "source_id", sourceID, "error", err)
				return nil
			}
			// Do not cache scrolling results.
			if img != nil && img.Scrolls {
				return img
			}
			panelCache.Lock()
			panelCache.m[key] = panelCacheEntry{img: img, at: time.Now()}
			panelCache.Unlock()
			return img
		}
		panelCache.Unlock()
		if PanelCacheHook != nil {
			PanelCacheHook(true)
		}
		return e.img
	}
	panelCache.Unlock()
	if PanelCacheHook != nil {
		PanelCacheHook(false)
	}

	img, err := fetch()
	if err != nil {
		slog.Warn("matrix panel render failed", "source_type", sourceType, "source_id", sourceID, "error", err)
		return nil
	}
	if img != nil && img.Scrolls {
		return img
	}
	panelCache.Lock()
	panelCache.m[key] = panelCacheEntry{img: img, at: time.Now()}
	panelCache.Unlock()
	return img
}

// clearPanelCache resets the cache; used by tests.
func clearPanelCache() {
	panelCache.Lock()
	panelCache.m = map[string]panelCacheEntry{}
	panelCache.Unlock()
}
