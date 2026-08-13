package datasource

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"ledit/render"
)

// SystemStats is the shared system statistics snapshot used by both the
// System Stats datasource (LED feed) and the TRMNL stats endpoint so values
// never diverge.
type SystemStats struct {
	CPUCores  int    `json:"cpu_cores"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Memory    string `json:"memory"`
	Load      string `json:"load"`
}

// GetSystemStats collects the current system statistics.
func GetSystemStats() SystemStats {
	return SystemStats{
		CPUCores:  runtime.NumCPU(),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS + "/" + runtime.GOARCH,
		Memory:    memString(),
		Load:      loadString(),
	}
}

type SystemStatsDS struct{}

func (s *SystemStatsDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	slog.Info("collecting system stats", "source", "systemstats")
	st := GetSystemStats()
	data := map[string]string{
		"CPU":  fmt.Sprintf("%d cores", st.CPUCores),
		"GO":   st.GoVersion,
		"OS":   st.OS,
		"MEM":  st.Memory,
		"LOAD": st.Load,
	}
	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
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
