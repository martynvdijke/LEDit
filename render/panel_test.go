package render

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

func TestCellSize(t *testing.T) {
	tests := []struct {
		name        string
		rows, cols  int
		gap         int
		width, hgt  int
		wantW, want int
	}{
		{"64x64 2x2 gap2", 2, 2, 2, 64, 64, 31, 31},
		{"32x32 2x2 gap2", 2, 2, 2, 32, 32, 15, 15},
		{"64x32 3x4 gap1", 3, 4, 1, 64, 32, 15, 10},
		{"64x64 1x1 gap0", 1, 1, 0, 64, 64, 64, 64},
		{"tiny canvas floors at 1", 4, 4, 4, 8, 8, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := CellSize(tt.rows, tt.cols, tt.gap, tt.width, tt.hgt)
			if w != tt.wantW || h != tt.want {
				t.Fatalf("CellSize(%d,%d,%d,%d,%d) = %dx%d, want %dx%d",
					tt.rows, tt.cols, tt.gap, tt.width, tt.hgt, w, h, tt.wantW, tt.want)
			}
		})
	}
}

func TestPanelOrigin(t *testing.T) {
	x, y := PanelOrigin(1, 2, 2, 31, 31)
	if x != 66 || y != 33 {
		t.Fatalf("PanelOrigin(1,2,2,31,31) = (%d,%d), want (66,33)", x, y)
	}
	x, y = PanelOrigin(0, 0, 2, 31, 31)
	if x != 0 || y != 0 {
		t.Fatalf("PanelOrigin(0,0,...) = (%d,%d), want (0,0)", x, y)
	}
}

func TestPanelFontSize(t *testing.T) {
	tests := []struct {
		panelH int
		want   float64
	}{
		{64, 16}, {32, 8}, {20, 8}, {100, 25}, {8, 8},
	}
	for _, tt := range tests {
		if got := PanelFontSize(tt.panelH); got != tt.want {
			t.Fatalf("PanelFontSize(%d) = %v, want %v", tt.panelH, got, tt.want)
		}
	}
}

func TestRenderPanel(t *testing.T) {
	theme := Theme{
		Name:            "test",
		BackgroundColor: [3]uint8{40, 42, 54},
		AccentColor:     [3]uint8{80, 250, 123},
		TextColor:       [3]uint8{139, 233, 253},
		Title:           "PANEL",
		FontSize:        0,
	}
	// With data.
	img, err := RenderPanel(map[string]string{"k1": "v1", "k2": "v2"}, 64, 64, theme, "../../fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderPanel with data: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG", img.Format)
	}
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := decoded.Bounds().Dx(); got != 64 {
		t.Fatalf("width = %d, want 64", got)
	}
	if got := decoded.Bounds().Dy(); got != 64 {
		t.Fatalf("height = %d, want 64", got)
	}

	// Empty data (unbound cell): title-only render must still succeed.
	empty, err := RenderPanel(nil, 32, 32, theme, "../../fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("RenderPanel empty: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(empty.Data)); err != nil {
		t.Fatalf("decode empty panel: %v", err)
	}
}

func TestTemplateGrid(t *testing.T) {
	bg := color.RGBA{40, 42, 54, 255}
	names := [][]string{{"WEATHER", ""}, {"F1", "EMPTY"}}
	img, err := TemplateGrid(2, 2, 2, bg, names, 64, 64)
	if err != nil {
		t.Fatalf("TemplateGrid: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG", img.Format)
	}
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
		t.Fatalf("bounds = %v, want 64x64", decoded.Bounds())
	}

	// Invalid dimensions must error.
	if _, err := TemplateGrid(0, 2, 2, bg, names, 64, 64); err == nil {
		t.Fatal("TemplateGrid(0,2,...) should error")
	}
}
