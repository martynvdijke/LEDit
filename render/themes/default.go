package themes

import "ledit/render"

var DefaultTheme = render.Theme{
	Name:            "cyber",
	BackgroundColor: [3]uint8{40, 42, 54},
	AccentColor:     [3]uint8{80, 250, 123},
	TextColor:       [3]uint8{139, 233, 253},
	Title:           "SYSTEM STATUS",
	FontSize:        24,
}
