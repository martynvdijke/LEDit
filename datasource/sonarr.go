package datasource

import (
	"ledit/render"
)

type SonarrDS struct {
	Token string
	URL   string
}

func (s *SonarrDS) GetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"name":    "Test Project",
		"version": "1.0",
		"status":  "active",
		"date":    "2024-03-25",
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}
