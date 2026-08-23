package datasource

import (
	"bytes"
	"image"
	"testing"

	"ledit/render"
)

func decodeRGBA(t *testing.T, img *render.RenderedImage) *image.RGBA {
	t.Helper()
	decoded, _, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// convert to RGBA
	b := decoded.Bounds()
	rgba := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rgba.Set(x, y, decoded.At(x, y))
		}
	}
	return rgba
}

func TestApplyCellThemeOverrides(t *testing.T) {
	base := render.Theme{BackgroundColor: [3]uint8{10, 10, 10}, AccentColor: [3]uint8{20, 20, 20}, TextColor: [3]uint8{30, 30, 30}, FontSize: 12}
	ct := &CellTheme{Background: "#ff0000", FontSize: 18}
	got := applyCellTheme(base, ct)
	if got.BackgroundColor != [3]uint8{255, 0, 0} {
		t.Fatalf("bg %v", got.BackgroundColor)
	}
	if got.AccentColor != base.AccentColor {
		t.Fatalf("accent should fallback")
	}
	if got.FontSize != 18 {
		t.Fatalf("fontsize %v", got.FontSize)
	}
	// nil theme falls back
	got2 := applyCellTheme(base, nil)
	if got2 != base {
		t.Fatalf("nil theme should return base")
	}
}

func TestMatrixCellThemeEmptyCellPixels(t *testing.T) {
	clearPanelCache()
	m := &MatrixDS{
		Name: "test", Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "missing", SourceID: 0, Theme: &CellTheme{Background: "#ff0000"}}},
		Resolve:  func(string, int) (Datasource, string, error) { return nil, "", nil },
	}
	img, err := m.GetPNG(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	rgba := decodeRGBA(t, img)
	// corner pixel should be background override red, not layout bg {40,42,54}
	r, g, b, _ := rgba.At(1, 1).RGBA()
	if uint8(r>>8) != 255 || uint8(g>>8) != 0 || uint8(b>>8) != 0 {
		t.Fatalf("expected red bg, got %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestMatrixCellThemePartialKeepsOtherChannels(t *testing.T) {
	clearPanelCache()
	m := &MatrixDS{
		Name: "test", Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "missing", SourceID: 0, Theme: &CellTheme{Background: "#00ff00"}}},
		Resolve:  func(string, int) (Datasource, string, error) { return nil, "", nil },
	}
	img, _ := m.GetPNG(32, 32)
	rgba := decodeRGBA(t, img)
	r, g, b, _ := rgba.At(1, 1).RGBA()
	if uint8(r>>8) != 0 || uint8(g>>8) != 255 || uint8(b>>8) != 0 {
		t.Fatalf("expected green, got %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestMatrixThemedRendererPath(t *testing.T) {
	clearPanelCache()
	ds := &SystemStatsDS{}
	m := &MatrixDS{
		Name: "test", Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "systemstats", SourceID: 1, Theme: &CellTheme{Background: "#ff0000"}}},
		Resolve:  func(string, int) (Datasource, string, error) { return ds, "", nil },
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	// nil theme falls back to layout theme (not red)
	m2 := &MatrixDS{
		Name: "test", Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "systemstats", SourceID: 1}},
		Resolve:  func(string, int) (Datasource, string, error) { return ds, "", nil },
	}
	clearPanelCache()
	img2, _ := m2.GetPNG(64, 64)
	if bytes.Equal(img.Data, img2.Data) {
		t.Fatal("themed vs non-themed should differ")
	}
}

func TestClockThemedRenderer(t *testing.T) {
	ds := &ClockDS{}
	theme := DefaultTheme()
	theme.BackgroundColor = [3]uint8{10, 20, 30}
	img, err := ds.GetPNGThemed(64, 64, theme)
	if err != nil {
		t.Fatal(err)
	}
	rgba := decodeRGBA(t, img)
	r, g, b, _ := rgba.At(0, 0).RGBA()
	if uint8(r>>8) != 10 || uint8(g>>8) != 20 || uint8(b>>8) != 30 {
		t.Fatalf("clock themed bg %d %d %d", r>>8, g>>8, b>>8)
	}
}
