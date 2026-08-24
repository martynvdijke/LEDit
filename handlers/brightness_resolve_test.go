package handlers

import (
	"testing"
	"time"
)

func TestResolveBrightness_SimpleMatch(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	// 2026-08-25 is Tuesday (2)
	now := mustTime("2026-08-25 22:30")
	if got := ResolveBrightness(now, windows, nil, nil); got != 30 {
		t.Fatalf("got %d want 30", got)
	}
}

func TestResolveBrightness_OvernightWrap(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{1}, Start: "22:00", End: "06:00", Level: 20}}
	// Monday window should match Tue 01:00
	if got := ResolveBrightness(mustTime("2026-08-25 01:00"), windows, nil, nil); got != 20 {
		t.Fatalf("overnight wrap got %d want 20", got)
	}
	if got := ResolveBrightness(mustTime("2026-08-24 23:00"), windows, nil, nil); got != 20 {
		t.Fatalf("got %d", got)
	}
}

func TestResolveBrightness_EndExclusive(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	if got := ResolveBrightness(mustTime("2026-08-25 23:00"), windows, nil, nil); got != 100 {
		t.Fatalf("end exclusive got %d want 100", got)
	}
	if got := ResolveBrightness(mustTime("2026-08-25 22:00"), windows, nil, nil); got != 30 {
		t.Fatalf("start inclusive got %d", got)
	}
}

func TestResolveBrightness_WeekdayMiss(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	// Sunday
	if got := ResolveBrightness(mustTime("2026-08-23 22:30"), windows, nil, nil); got != 100 {
		t.Fatalf("weekday miss got %d want 100", got)
	}
}

func TestResolveBrightness_Precedence(t *testing.T) {
	windows := []BrightnessWindow{{Days: []int{2}, Start: "22:00", End: "23:00", Level: 30}}
	sensor := 60
	override := 80
	if got := ResolveBrightness(mustTime("2026-08-25 22:30"), windows, &sensor, &override); got != 80 {
		t.Fatalf("override wins got %d", got)
	}
	if got := ResolveBrightness(mustTime("2026-08-25 22:30"), windows, &sensor, nil); got != 60 {
		t.Fatalf("sensor wins got %d", got)
	}
	// stale sensor: nil sensor degrades to schedule
	if got := ResolveBrightness(mustTime("2026-08-25 22:30"), windows, nil, nil); got != 30 {
		t.Fatalf("schedule got %d", got)
	}
	if got := ResolveBrightness(mustTime("2026-08-25 10:00"), windows, nil, nil); got != 100 {
		t.Fatalf("none got %d", got)
	}
}

func TestSensorLevelForLux(t *testing.T) {
	cfg := &SensorConfig{EntityID: "sensor.lux", LuxLevels: []LuxLevel{{MaxLux: 100, Level: 20}, {MaxLux: 500, Level: 60}}}
	if got := SensorLevelForLux(50, cfg); got == nil || *got != 20 {
		t.Fatalf("got %v", got)
	}
	if got := SensorLevelForLux(200, cfg); got == nil || *got != 60 {
		t.Fatalf("got %v", got)
	}
	if got := SensorLevelForLux(600, cfg); got != nil {
		t.Fatalf("beyond max should return nil, got %v", *got)
	}
}

func TestBrightnessRampLerp(t *testing.T) {
	r := NewBrightnessRamp(100)
	r.SetTarget(30)
	// Advance 10 steps should reach 30
	for i := 0; i < 10; i++ {
		r.Advance()
	}
	if got := r.EffectiveLevel(); got != 30 {
		t.Fatalf("ramp final got %d want 30", got)
	}
	// intermediate not jump
	r2 := NewBrightnessRamp(100)
	r2.SetTarget(0)
	v1 := r2.Advance()
	if v1 >= 100 || v1 <= 0 {
		t.Fatalf("first step %d", v1)
	}
	// monotonic decreasing
	prev := v1
	for i := 1; i < 10; i++ {
		cur := r2.Advance()
		if cur > prev {
			t.Fatalf("not monotonic %d > %d", cur, prev)
		}
		prev = cur
	}
	_ = time.Now
}

func TestIsStaleAndShouldFetch(t *testing.T) {
	s := SensorFetchState{LastTime: time.Now().Add(-70 * time.Second)}
	if !s.IsStale(time.Now()) {
		t.Fatalf("should be stale")
	}
	s2 := SensorFetchState{LastTime: time.Now()}
	if s2.IsStale(time.Now()) {
		t.Fatalf("not stale")
	}
}
