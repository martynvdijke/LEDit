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

// PanelBinding maps one grid cell to a datasource by type and DB id.
type PanelBinding struct {
	Row        int    `json:"row"`
	Col        int    `json:"col"`
	SourceType string `json:"source_type"`
	SourceID   int    `json:"source_id"`
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
	}
	return true
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
				panel = cachedPanelGet(b.SourceType, b.SourceID, cellW, cellH, func() (*render.RenderedImage, error) {
					return src.GetPNG(cellW, cellH)
				})
			}
		}
		if panel == nil {
			// Unbound or unresolved: empty themed cell (title-only).
			emptyTheme := theme
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

// cachedPanelGet returns a cached panel render within the TTL, otherwise
// fetches (once) and stores it. Returns nil when fetch fails.
func cachedPanelGet(sourceType string, sourceID, w, h int, fetch func() (*render.RenderedImage, error)) *render.RenderedImage {
	key := fmt.Sprintf("%s:%d:%dx%d", sourceType, sourceID, w, h)
	panelCache.Lock()
	if e, ok := panelCache.m[key]; ok && time.Since(e.at) < panelCacheTTL {
		panelCache.Unlock()
		return e.img
	}
	panelCache.Unlock()

	img, err := fetch()
	if err != nil {
		slog.Warn("matrix panel render failed", "source_type", sourceType, "source_id", sourceID, "error", err)
		return nil
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
