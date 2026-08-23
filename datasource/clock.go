package datasource

import (
	"time"

	"ledit/render"
)

// ClockDS renders the current server-local time. Built-in: no DB row, no network.
type ClockDS struct{}

func (c *ClockDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	return render.RenderClock(time.Now(), width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func (c *ClockDS) GetPNGThemed(width, height int, theme render.Theme) (*render.RenderedImage, error) {
	return render.RenderClock(time.Now(), width, height, theme, "fonts/PixelifySans.ttf")
}
