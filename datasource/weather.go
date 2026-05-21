package datasource

import (
	"ledit/render"
)

type WeatherDS struct {
	Token string
	URL   string
}

func (w *WeatherDS) GetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"condition": "sunny",
		"temp":      "22°C",
		"humidity":  "45%",
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}
