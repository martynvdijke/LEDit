package datasource

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"ledit/render"
)

type SystemStatsDS struct{}

func (s *SystemStatsDS) GetPNG() (*render.RenderedImage, error) {
	data := map[string]string{
		"CPU":  fmt.Sprintf("%d cores", runtime.NumCPU()),
		"GO":   runtime.Version(),
		"OS":   runtime.GOOS + "/" + runtime.GOARCH,
		"MEM":  memString(),
		"LOAD": loadString(),
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func memString() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	alloc := float64(m.Alloc) / 1024 / 1024
	total := float64(m.TotalAlloc) / 1024 / 1024
	return fmt.Sprintf("%.0f/%.0f MB", alloc, total)
}

func loadString() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "--"
	}
	parts := strings.Fields(string(data))
	if len(parts) >= 3 {
		return fmt.Sprintf("%s %s %s", parts[0], parts[1], parts[2])
	}
	return "--"
}
