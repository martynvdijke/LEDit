package datasource

import (
	"time"

	"ledit/render"
)

// AnalogClockDS is a built-in, no-configuration datasource that renders an
// analog clock face driven by the current wall-clock time.
type AnalogClockDS struct{}

func (a *AnalogClockDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	return render.RenderAnalogClock(time.Now(), width, height)
}

// MatrixRainDS is a built-in, no-configuration datasource that renders the
// deterministic "matrix digital rain" animation driven by wall-clock time.
type MatrixRainDS struct{}

func (m *MatrixRainDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	return render.RenderMatrixRain(time.Now(), width, height)
}
