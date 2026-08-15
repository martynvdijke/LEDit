package datasource

import (
	"time"

	"ledit/render"
)

// CountdownDS renders a countdown timer toward a fixed target time.
type CountdownDS struct {
	Name   string
	Label  string
	Target time.Time
}

func (c *CountdownDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	return render.RenderCountdown(c.Label, c.Target, time.Now(), width, height)
}
