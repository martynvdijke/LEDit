package datasource

import (
	"github.com/martynvdijke/ledit/internal/render"
	"github.com/martynvdijke/ledit/internal/render/themes"
)

type F1DS struct {
	Token string
	URL   string
}

func (f *F1DS) GetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"Next Race": "Monaco GP",
		"Time":      "14:00",
		"Leader":    "Max Verstappen",
		"Points":    "150",
	}
	return render.RenderDict(data, 400, 400, themes.F1Theme, "fonts/PixelifySans.ttf")
}
