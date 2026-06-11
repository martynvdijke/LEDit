package datasource

import (
	"log/slog"

	"ledit/render"
)

type TextSlideDS struct {
	Content  string
	Color    string
	BgColor  string
	FontSize int
}

func (t *TextSlideDS) GetPNG() (*render.RenderedImage, error) {
	slog.Info("rendering text slide", "source", "textslide", "content_length", len(t.Content))
	return render.RenderText(t.Content, 400, 400, t.BgColor, t.Color, float64(t.FontSize), "fonts/PixelifySans.ttf")
}
