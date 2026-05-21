package themes

import "github.com/martynvdijke/ledit/internal/render"

var F1Theme = render.Theme{
	Name:            "f1",
	BackgroundColor: [3]uint8{33, 33, 33},
	AccentColor:     [3]uint8{255, 24, 1},
	TextColor:       [3]uint8{255, 255, 255},
	Title:           "F1 STATUS",
	FontSize:        28,
}
