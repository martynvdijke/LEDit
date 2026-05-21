package datasource

import (
	"github.com/martynvdijke/ledit/internal/render"
)

type RadarrDS struct {
	Token string
	URL   string
}

func (r *RadarrDS) GetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"name":   "Radarr",
		"status": "active",
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}
