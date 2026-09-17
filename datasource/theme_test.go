package datasource

import (
	"testing"

	"ledit/render"
	"ledit/render/themes"
)

func TestDefaultTheme(t *testing.T) {
	got := DefaultTheme()
	if got != themes.DefaultTheme {
		t.Fatalf("DefaultTheme mismatch: got %+v want %+v", got, themes.DefaultTheme)
	}
	if got.BackgroundColor != [3]uint8{40, 42, 54} {
		t.Errorf("BG got %v", got.BackgroundColor)
	}
	if got.AccentColor != [3]uint8{80, 250, 123} {
		t.Errorf("Accent got %v", got.AccentColor)
	}
	if got.TextColor != [3]uint8{139, 233, 253} {
		t.Errorf("Text got %v", got.TextColor)
	}
	if got.Title != "SYSTEM STATUS" {
		t.Errorf("Title got %q", got.Title)
	}
	if got.FontSize != 24 {
		t.Errorf("FontSize got %v", got.FontSize)
	}
}

func TestRenderThemed(t *testing.T) {
	clock := &ClockDS{}
	th := themes.DefaultTheme
	img, err := RenderThemed(clock, 64, 64, th)
	if err != nil || img == nil {
		t.Fatalf("RenderThemed clock failed: %v", err)
	}
	// non-themed path
	plain := &plainDS{}
	img2, err := RenderThemed(plain, 64, 64, th)
	if err != nil || img2 == nil {
		t.Fatalf("RenderThemed plain failed: %v", err)
	}
}

type plainDS struct{}

func (p *plainDS) GetPNG(w, h int) (*render.RenderedImage, error) {
	return render.RenderDict(map[string]string{"x": "1"}, w, h, DefaultTheme(), "fonts/PixelifySans.ttf")
}
