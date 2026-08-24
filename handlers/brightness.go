package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// BrightnessWindow mirrors schedule window but with Level 0-100.
type BrightnessWindow struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
	Level int    `json:"level"`
}

// LuxLevel maps maxLux -> level.
type LuxLevel struct {
	MaxLux float64 `json:"maxLux"`
	Level  int     `json:"level"`
}

// SensorConfig holds HA sensor binding.
type SensorConfig struct {
	EntityID  string     `json:"entity_id"`
	LuxLevels []LuxLevel `json:"lux_levels"`
}

// ParseBrightnessWindows decodes JSON array.
func ParseBrightnessWindows(s string) ([]BrightnessWindow, error) {
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "[]" {
		return []BrightnessWindow{}, nil
	}
	var out []BrightnessWindow
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("invalid brightness_schedules JSON: %w", err)
	}
	if out == nil {
		return []BrightnessWindow{}, nil
	}
	return out, nil
}

// ParseSensorConfig decodes nullable JSON.
func ParseSensorConfig(s *string) (*SensorConfig, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	var c SensorConfig
	if err := json.Unmarshal([]byte(*s), &c); err != nil {
		return nil, fmt.Errorf("invalid brightness_sensor_config JSON: %w", err)
	}
	return &c, nil
}

// ValidateBrightnessWindows validates per spec.
func ValidateBrightnessWindows(windows []BrightnessWindow) error {
	if len(windows) > 16 {
		return fmt.Errorf("too many brightness windows: %d (max 16)", len(windows))
	}
	for i, w := range windows {
		if len(w.Days) == 0 {
			return fmt.Errorf("window %d: days must be non-empty", i)
		}
		seen := map[int]bool{}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return fmt.Errorf("window %d: day %d out of range 0-6", i, d)
			}
			if seen[d] {
				return fmt.Errorf("window %d: duplicate day %d", i, d)
			}
			seen[d] = true
		}
		if _, err := parseHM(w.Start); err != nil {
			return fmt.Errorf("window %d: invalid start %q: %w", i, w.Start, err)
		}
		if _, err := parseHM(w.End); err != nil {
			return fmt.Errorf("window %d: invalid end %q: %w", i, w.End, err)
		}
		if w.Start == w.End {
			return fmt.Errorf("window %d: start and end must differ", i)
		}
		if w.Level < 0 || w.Level > 100 {
			return fmt.Errorf("window %d: level %d out of range 0-100", i, w.Level)
		}
	}
	return nil
}

// ValidateSensorConfig validates sensor config.
func ValidateSensorConfig(c *SensorConfig) error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.EntityID) == "" {
		return fmt.Errorf("sensor_config: entity_id required")
	}
	if len(c.LuxLevels) == 0 {
		return fmt.Errorf("sensor_config: lux_levels must be non-empty")
	}
	for i, ll := range c.LuxLevels {
		if ll.Level < 0 || ll.Level > 100 {
			return fmt.Errorf("sensor_config lux_levels[%d]: level %d out of range 0-100", i, ll.Level)
		}
		if i > 0 && c.LuxLevels[i].MaxLux <= c.LuxLevels[i-1].MaxLux {
			return fmt.Errorf("sensor_config lux_levels must be sorted ascending by maxLux")
		}
	}
	// also ensure sorted
	sorted := sort.SliceIsSorted(c.LuxLevels, func(i, j int) bool { return c.LuxLevels[i].MaxLux < c.LuxLevels[j].MaxLux })
	if !sorted {
		return fmt.Errorf("sensor_config lux_levels must be sorted ascending by maxLux")
	}
	return nil
}

// brightnessWindowMatches uses same logic as WindowMatches.
func brightnessWindowMatches(now time.Time, w BrightnessWindow) bool {
	sw := ScheduleWindow{Days: w.Days, Start: w.Start, End: w.End}
	return WindowMatches(now, sw)
}

// ResolveBrightness implements precedence: override > sensor > schedule >100.
// sensorLevel nil means no fresh reading or sensor disabled.
// now is server-local time.
func ResolveBrightness(now time.Time, schedules []BrightnessWindow, sensorLevel *int, override *int) int {
	if override != nil {
		if *override < 0 {
			return 0
		}
		if *override > 100 {
			return 100
		}
		return *override
	}
	if sensorLevel != nil {
		if *sensorLevel < 0 {
			return 0
		}
		if *sensorLevel > 100 {
			return 100
		}
		return *sensorLevel
	}
	for _, w := range schedules {
		if brightnessWindowMatches(now, w) {
			return w.Level
		}
	}
	return 100
}

// SensorLevelForLux maps lux to level using sorted table.
func SensorLevelForLux(lux float64, cfg *SensorConfig) *int {
	if cfg == nil || len(cfg.LuxLevels) == 0 {
		return nil
	}
	for _, ll := range cfg.LuxLevels {
		if lux <= ll.MaxLux {
			v := ll.Level
			return &v
		}
	}
	return nil
}

// BrightnessRamp holds per-device ramp state.
type BrightnessRamp struct {
	Current float64
	Target  int
	Steps   int
	StepIdx int
}

// NewBrightnessRamp creates ramp at initial level.
func NewBrightnessRamp(initial int) *BrightnessRamp {
	return &BrightnessRamp{Current: float64(initial), Target: initial, Steps: 10}
}

// SetTarget updates target; resets step counter if changed.
func (r *BrightnessRamp) SetTarget(target int) {
	if r.Target != target {
		r.Target = target
		r.StepIdx = 0
	}
}

// Advance moves one step toward target over 10 steps (30s). Returns current rounded level.
func (r *BrightnessRamp) Advance() int {
	if r.Current == float64(r.Target) {
		return r.Target
	}
	r.StepIdx++
	if r.StepIdx >= r.Steps {
		r.Current = float64(r.Target)
		return r.Target
	}
	// lerp from start to target linearly across Steps
	// We need start value; track via interpolation: step fraction
	// Current is updated incrementally: delta per step
	// Simpler: compute delta per step from current at target change? Use linear interpolation per step.
	// Instead recompute based on remaining steps.
	// We'll do incremental: need initial before ramp. Use stored current at step 0: we can compute step size as (target - initial)/Steps
	// For simplicity, use exponential approach: move 1/remaining linearly.
	// Implement as: current = current + (target - current)/remaining
	// where remaining = Steps - StepIdx +1
	remaining := float64(r.Steps - r.StepIdx + 1)
	if remaining < 1 {
		remaining = 1
	}
	r.Current = r.Current + (float64(r.Target)-r.Current)/remaining
	// Round for display but keep float for next step
	return int(r.Current + 0.5)
}

// EffectiveLevel returns rounded current level.
func (r *BrightnessRamp) EffectiveLevel() int {
	return int(r.Current + 0.5)
}

// FetchSensorLux is overridable for tests; default returns error.
var FetchSensorLux = func(entityID string) (float64, error) {
	return 0, fmt.Errorf("sensor fetch not configured")
}

// SensorFetchState tracks staleness and jitter. Used per WS connection.
type SensorFetchState struct {
	LastValue float64
	LastTime  time.Time
	LastErr   time.Time
	Warned    bool
}

// ShouldFetch returns true if ≥5s ±10% jitter since last attempt.
func (s *SensorFetchState) ShouldFetch(now time.Time, lastFetch time.Time) bool {
	if lastFetch.IsZero() {
		return true
	}
	base := 5 * time.Second
	j := (rand.Float64()*2 - 1) * float64(base) * 0.1
	interval := base + time.Duration(j)
	return now.Sub(lastFetch) >= interval
}

// IsStale reports if last reading older than 60s.
func (s *SensorFetchState) IsStale(now time.Time) bool {
	if s.LastTime.IsZero() {
		return true
	}
	return now.Sub(s.LastTime) > 60*time.Second
}

var _ = slog.Info
