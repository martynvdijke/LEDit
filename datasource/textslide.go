package datasource

import (
	"ledit/render"
)

type TextSlideDS struct {
	Content  string
	Color    string
	BgColor  string
	FontSize int
}

func (t *TextSlideDS) GetPNG() (*render.RenderedImage, error) {
	return render.RenderText(t.Content, 400, 400, t.BgColor, t.Color, float64(t.FontSize), "fonts/PixelifySans.ttf")
}
