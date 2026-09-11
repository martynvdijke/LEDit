package datasource

import (
	"time"

	"ledit/render"
)

// CountdownDS renders a countdown timer toward a fixed target time, or the
// elapsed time since it when configured to count up.
type CountdownDS struct {
	Name              string
	Label             string
	Target            time.Time
	Granularity       string
	Direction         string
	Completion        string
	CompletionMessage string
	Timezone          string
}

func (c *CountdownDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	opts := render.CountdownOptions{
		Granularity: c.Granularity,
		Direction:   c.Direction,
		Completion:  c.Completion,
		Message:     c.CompletionMessage,
	}
	return render.RenderCountdownWithOptions(c.Label, c.Target, time.Now(), width, height, opts)
}
