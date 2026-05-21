package datasource

import (
	"ledit/render"
)

type HomeAssistantDS struct {
	Token string
	URL   string
}

func (h *HomeAssistantDS) GetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"temperature": "21.5°C",
		"lights":      "on",
		"alarm":       "disarmed",
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}
