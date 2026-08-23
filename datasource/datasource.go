package datasource

import (
	"context"

	"ledit/render"
)

type Datasource interface {
	GetPNG(width, height int) (*render.RenderedImage, error)
}

// StateProvider is an optional capability interface: sources that can expose
// live state for event rules implement it (mirrors the Animator pattern).
type StateProvider interface {
	CurrentState(ctx context.Context) (map[string]any, error)
}

// Ambienter is optionally implemented by datasources whose output is
// time-animated (e.g. weather precipitation). MatrixDS bypasses its panel
// TTL cache for ambient sources so cells animate.
type Ambienter interface{ Ambient() bool }

// IsAmbient reports whether ds implements Ambienter and returns true.
// Nil-safe: nil datasource returns false.
func IsAmbient(ds Datasource) bool {
	if ds == nil {
		return false
	}
	if a, ok := ds.(Ambienter); ok {
		return a.Ambient()
	}
	return false
}

// ThemedRenderer is optionally implemented by datasources that can render
// with a caller-supplied theme (enables per-cell theme overrides in MatrixDS).
type ThemedRenderer interface {
	GetPNGThemed(width, height int, theme render.Theme) (*render.RenderedImage, error)
}

type RenderedBase struct{}

func DefaultGetPNG(width, height int) (*render.RenderedImage, error) {
	data := map[string]string{
		"name":    "Test Project",
		"version": "1.0",
		"status":  "active",
		"date":    "2024-03-25",
	}
	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
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
