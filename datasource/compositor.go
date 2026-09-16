package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"strings"

	"ledit/render"
)

// Region is one child placement inside a composition. It is either a grid cell
// (Row/Col plus optional spans) or an explicit absolute rectangle (X/Y/W/H);
// a region with positive W and H is treated as absolute, otherwise as a grid
// cell. Theme reuses CellTheme so applyCellTheme works unchanged.
type Region struct {
	ID         string     `json:"id,omitempty"` // stable key for state namespacing
	Row        int        `json:"row,omitempty"`
	Col        int        `json:"col,omitempty"`
	RowSpan    int        `json:"row_span,omitempty"`
	ColSpan    int        `json:"col_span,omitempty"`
	X          int        `json:"x,omitempty"`
	Y          int        `json:"y,omitempty"`
	W          int        `json:"w,omitempty"`
	H          int        `json:"h,omitempty"`
	SourceType string     `json:"source_type"`
	SourceID   int        `json:"source_id"`
	Theme      *CellTheme `json:"theme,omitempty"`
	Inset      int        `json:"inset,omitempty"`
	Border     bool       `json:"border,omitempty"`
}

// CompositorDS lays out regions onto one canvas and composites each region's
// child render. Children resolve through the same Resolve seam as MatrixDS, so
// any datasource (DB-backed, built-in, plugin, or another composition) is a
// valid child.
type CompositorDS struct {
	Name       string
	Mode       string // "grid" | "absolute" (informational; geometry is inferred per region)
	Background string
	Rows, Cols int
	Gap        int
	Padding    int
	Regions    []Region
	Resolve    func(sourceType string, sourceID int) (Datasource, string, error)
	Depth      int
}

// ParseRegions decodes a regions JSON array, tolerating malformed input.
func ParseRegions(raw string) []Region {
	var out []Region
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		slog.Warn("invalid composition regions JSON", "error", err)
	}
	return out
}

// RegionsJSON serializes regions into a JSON array string.
func RegionsJSON(regions []Region) string {
	b, err := json.Marshal(regions)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ValidRegions reports whether raw is a parseable regions JSON array with
// well-formed geometry: grid cells within rows/cols (spans included), absolute
// rectangles with positive dimensions, non-negative inset, and valid themes.
func ValidRegions(raw string, rows, cols int) bool {
	var regions []Region
	if err := json.Unmarshal([]byte(raw), &regions); err != nil {
		return false
	}
	for _, r := range regions {
		if r.Inset < 0 {
			return false
		}
		if r.Theme != nil && !r.Theme.Validate() {
			return false
		}
		if r.W > 0 || r.H > 0 {
			if r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 {
				return false
			}
			continue
		}
		if r.Row < 0 || r.Row >= rows || r.Col < 0 || r.Col >= cols {
			return false
		}
		rs, cs := r.RowSpan, r.ColSpan
		if rs <= 0 {
			rs = 1
		}
		if cs <= 0 {
			cs = 1
		}
		if r.Row+rs > rows || r.Col+cs > cols {
			return false
		}
	}
	return true
}

// GetPNG renders the composition with a default theme.
func (m *CompositorDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	return m.render(width, height, nil)
}

// GetPNGThemed renders the composition using the caller theme as the base,
// with each region's own theme layered on top.
func (m *CompositorDS) GetPNGThemed(width, height int, theme render.Theme) (*render.RenderedImage, error) {
	return m.render(width, height, &theme)
}

// Ambient is true when any resolvable child is ambient.
func (m *CompositorDS) Ambient() bool {
	if m.Resolve == nil {
		return false
	}
	for _, r := range m.Regions {
		if r.SourceType == "" {
			continue
		}
		src, _, err := m.Resolve(r.SourceType, r.SourceID)
		if err != nil || src == nil {
			continue
		}
		if IsAmbient(src) {
			return true
		}
	}
	return false
}

// CurrentState merges resolvable children's state under region-qualified keys
// (region ID when set, else "type:id"). Failing or non-state children are
// skipped without failing the aggregate.
func (m *CompositorDS) CurrentState(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	if m.Resolve == nil {
		return out, nil
	}
	for _, r := range m.Regions {
		if r.SourceType == "" {
			continue
		}
		src, _, err := m.Resolve(r.SourceType, r.SourceID)
		if err != nil || src == nil {
			continue
		}
		sp, ok := src.(StateProvider)
		if !ok {
			continue
		}
		st, err := sp.CurrentState(ctx)
		if err != nil {
			slog.Warn("composition child state failed", "composition", m.Name, "region", r.ID, "error", err)
			continue
		}
		prefix := r.ID
		if prefix == "" {
			prefix = fmt.Sprintf("%s:%d", r.SourceType, r.SourceID)
		}
		for k, v := range st {
			out[prefix+"."+k] = v
		}
	}
	return out, nil
}

func (m *CompositorDS) baseTheme(caller *render.Theme) render.Theme {
	if caller != nil {
		return *caller
	}
	return render.Theme{
		Name:            "cyber",
		BackgroundColor: [3]uint8{40, 42, 54},
		AccentColor:     [3]uint8{80, 250, 123},
		TextColor:       [3]uint8{139, 233, 253},
		Title:           strings.ToUpper(m.Name),
	}
}

func (m *CompositorDS) render(width, height int, caller *render.Theme) (*render.RenderedImage, error) {
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("invalid composition dimensions %dx%d", width, height)
	}
	theme := m.baseTheme(caller)

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	bgFallback := color.RGBA{theme.BackgroundColor[0], theme.BackgroundColor[1], theme.BackgroundColor[2], 255}
	bg := parseHexColor(m.Background, bgFallback)
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	for _, r := range m.Regions {
		x, y, w, h, ok := m.regionRect(r, width, height)
		if !ok {
			slog.Warn("composition region out of range, skipping", "composition", m.Name, "region", r.ID)
			continue
		}
		regionTheme := applyCellTheme(theme, r.Theme)
		themed := caller != nil || r.Theme != nil
		panel := m.renderRegion(r, w, h, regionTheme, themed)
		if panel == nil {
			panel = m.placeholder(r, w, h, regionTheme)
		}
		drawPanel(canvas, panel, x, y, w, h)
		if r.Border {
			drawBorder(canvas, x, y, w, h, regionTheme.AccentColor)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

// regionRect computes a region's pixel rectangle, applying grid spans or the
// absolute rectangle, canvas padding for grid mode, and inset. ok is false for
// out-of-range or degenerate rectangles.
func (m *CompositorDS) regionRect(r Region, width, height int) (x, y, w, h int, ok bool) {
	if r.W > 0 || r.H > 0 {
		if r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 {
			return 0, 0, 0, 0, false
		}
		x, y, w, h = r.X, r.Y, r.W, r.H
	} else {
		rows, cols := m.Rows, m.Cols
		if rows < 1 {
			rows = 1
		}
		if cols < 1 {
			cols = 1
		}
		if r.Row < 0 || r.Row >= rows || r.Col < 0 || r.Col >= cols {
			return 0, 0, 0, 0, false
		}
		rs, cs := r.RowSpan, r.ColSpan
		if rs <= 0 {
			rs = 1
		}
		if cs <= 0 {
			cs = 1
		}
		if r.Row+rs > rows || r.Col+cs > cols {
			return 0, 0, 0, 0, false
		}
		contentW, contentH := width-2*m.Padding, height-2*m.Padding
		if contentW < 1 || contentH < 1 {
			return 0, 0, 0, 0, false
		}
		cellW, cellH := render.CellSize(rows, cols, m.Gap, contentW, contentH)
		ox, oy := render.PanelOrigin(r.Row, r.Col, m.Gap, cellW, cellH)
		x = m.Padding + ox
		y = m.Padding + oy
		w = cs*cellW + (cs-1)*m.Gap
		h = rs*cellH + (rs-1)*m.Gap
	}
	if r.Inset > 0 {
		x += r.Inset
		y += r.Inset
		w -= 2 * r.Inset
		h -= 2 * r.Inset
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x >= width || y >= height {
		return 0, 0, 0, 0, false
	}
	if x+w > width {
		w = width - x
	}
	if y+h > height {
		h = height - y
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	return x, y, w, h, true
}

// renderRegion resolves and renders one region's child, returning nil when the
// child cannot be resolved or rendered (the caller substitutes a placeholder).
func (m *CompositorDS) renderRegion(r Region, w, h int, regionTheme render.Theme, themed bool) *render.RenderedImage {
	if m.Resolve == nil || r.SourceType == "" {
		return nil
	}
	src, _, err := m.Resolve(r.SourceType, r.SourceID)
	if err != nil || src == nil {
		slog.Warn("composition region resolve failed", "composition", m.Name, "region", r.ID,
			"source", fmt.Sprintf("%s:%d", r.SourceType, r.SourceID), "error", err)
		return nil
	}
	fetch := func() (*render.RenderedImage, error) {
		if themed {
			if tr, ok := src.(ThemedRenderer); ok {
				return tr.GetPNGThemed(w, h, regionTheme)
			}
		}
		return src.GetPNG(w, h)
	}
	// Ambient and always-animated sources bypass the panel cache so regions
	// animate instead of freezing for the TTL.
	if IsAmbient(src) || isAlwaysFresh(src) {
		img, err := fetch()
		if err != nil {
			slog.Warn("composition region render failed", "composition", m.Name, "region", r.ID,
				"source", fmt.Sprintf("%s:%d", r.SourceType, r.SourceID), "error", err)
			return nil
		}
		return img
	}
	return cachedPanelGet(r.SourceType, r.SourceID, w, h, fetch)
}

// isAlwaysFresh lists the time-driven built-ins MatrixDS re-renders every frame.
func isAlwaysFresh(src Datasource) bool {
	switch src.(type) {
	case *AnalogClockDS, *MatrixRainDS, *CountdownDS:
		return true
	}
	return false
}

// placeholder renders a labeled region for an unresolved/failing child, or an
// empty themed region when the region is unbound.
func (m *CompositorDS) placeholder(r Region, w, h int, regionTheme render.Theme) *render.RenderedImage {
	var data map[string]string
	if r.SourceType == "" {
		regionTheme.Title = ""
	} else {
		regionTheme.Title = strings.ToUpper(fmt.Sprintf("%s:%d", r.SourceType, r.SourceID))
		data = map[string]string{"state": "unavailable"}
	}
	img, _ := render.RenderPanel(data, w, h, regionTheme, "fonts/PixelifySans.ttf")
	return img
}

// drawBorder draws a one-pixel accent frame around a region rectangle.
func drawBorder(canvas *image.RGBA, x, y, w, h int, accent [3]uint8) {
	c := color.RGBA{accent[0], accent[1], accent[2], 255}
	for px := x; px < x+w; px++ {
		canvas.Set(px, y, c)
		canvas.Set(px, y+h-1, c)
	}
	for py := y; py < y+h; py++ {
		canvas.Set(x, py, c)
		canvas.Set(x+w-1, py, c)
	}
}
