package themes

import "github.com/martynvdijke/ledit/internal/render"

var UntappdTheme = render.Theme{
	Name:            "untappd",
	BackgroundColor: [3]uint8{255, 196, 37},
	AccentColor:     [3]uint8{76, 89, 104},
	TextColor:       [3]uint8{33, 33, 33},
	Title:           "BEER STATUS",
	FontSize:        24,
}
