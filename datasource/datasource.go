package datasource

import (
	"ledit/render"
)

type Datasource interface {
	GetPNG() (*render.RenderedImage, error)
}

type RenderedBase struct{}

func DefaultGetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"name":    "Test Project",
		"version": "1.0",
		"status":  "active",
		"date":    "2024-03-25",
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func DefaultTheme() render.Theme {
	return render.Theme{
		Name:            "cyber",
		BackgroundColor: [3]uint8{40, 42, 54},
		AccentColor:     [3]uint8{80, 250, 123},
		TextColor:       [3]uint8{139, 233, 253},
		Title:           "SYSTEM STATUS",
		FontSize:        24,
	}
}
