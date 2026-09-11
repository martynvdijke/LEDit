package handlers

import "testing"

func TestResolveEffectiveBrightnessManualOverrideWins(t *testing.T) {
	schedules := []BrightnessWindow{{Days: []int{2}, Start: "06:00", End: "07:00", Level: 30}}
	sensor := 50
	override := 80
	alarm := 20
	now := mustTime("2026-09-15 06:30")
	if got := ResolveEffectiveBrightness(now, schedules, &sensor, &override, &alarm); got != 80 {
		t.Fatalf("manual override must win over alarm ramp, got %d", got)
	}
}

func TestResolveEffectiveBrightnessAlarmBeatsSensor(t *testing.T) {
	schedules := []BrightnessWindow{{Days: []int{2}, Start: "06:00", End: "07:00", Level: 30}}
	sensor := 70
	alarm := 40
	now := mustTime("2026-09-15 06:30")
	if got := ResolveEffectiveBrightness(now, schedules, &sensor, nil, &alarm); got != 40 {
		t.Fatalf("alarm ramp must beat sensor, got %d", got)
	}
}

func TestResolveEffectiveBrightnessNoAlarmUnchanged(t *testing.T) {
	schedules := []BrightnessWindow{{Days: []int{2}, Start: "06:00", End: "07:00", Level: 30}}
	now := mustTime("2026-09-15 06:30")
	if got := ResolveEffectiveBrightness(now, schedules, nil, nil, nil); got != 30 {
		t.Fatalf("without alarm the schedule must apply, got %d", got)
	}
	sensor := 70
	if got := ResolveEffectiveBrightness(now, schedules, &sensor, nil, nil); got != 70 {
		t.Fatalf("without alarm the sensor must apply, got %d", got)
	}
}

func TestResolveEffectiveBrightnessHoldsRampEnd(t *testing.T) {
	a := &WakeAlarm{Enabled: true, Days: []int{2}, Start: "06:00", End: "08:00", BrightnessEnabled: true, BrightnessStart: 1, BrightnessEnd: 100, BrightnessRampSeconds: 600}
	now := mustTime("2026-09-15 07:30") // well past the ramp
	lvl := AlarmBrightnessLevel(now, a)
	if lvl == nil || *lvl != 100 {
		t.Fatalf("expected held end 100, got %v", lvl)
	}
	if got := ResolveEffectiveBrightness(now, nil, nil, nil, lvl); got != 100 {
		t.Fatalf("effective brightness should hold 100, got %d", got)
	}
}

func TestResolveEffectiveBrightnessAlarmClamps(t *testing.T) {
	high := 150
	low := -5
	if got := ResolveEffectiveBrightness(mustTime("2026-09-15 06:30"), nil, nil, nil, &high); got != 100 {
		t.Fatalf("clamp high: got %d", got)
	}
	if got := ResolveEffectiveBrightness(mustTime("2026-09-15 06:30"), nil, nil, nil, &low); got != 0 {
		t.Fatalf("clamp low: got %d", got)
	}
}

func TestAlarmManagerBrightnessDisabledInert(t *testing.T) {
	m := NewAlarmManager()
	now := mustTime("2026-09-15 06:30")
	resolve := func(*WakeAlarm) (*sourceWithName, bool) {
		return &sourceWithName{Name: "Wake", cacheKey: "clock:0"}, true
	}

	// Ramp disabled: no brightness level even while the alarm is active.
	m.SetAlarms([]WakeAlarm{{ID: 1, Enabled: true, Days: []int{2}, Start: "06:00", End: "07:00", BrightnessEnabled: false}})
	m.Evaluate(now, resolve)
	if lvl := m.BrightnessLevel(now); lvl != nil {
		t.Fatalf("brightness-disabled alarm must not contribute a level, got %v", lvl)
	}

	// Ramp enabled: the level is present.
	m.SetAlarms([]WakeAlarm{{ID: 2, Enabled: true, Days: []int{2}, Start: "06:00", End: "07:00", BrightnessEnabled: true, BrightnessStart: 0, BrightnessEnd: 100, BrightnessRampSeconds: 600}})
	m.Evaluate(now, resolve)
	// now is 30 min into a 10 min ramp -> held at end 100.
	if lvl := m.BrightnessLevel(now); lvl == nil || *lvl != 100 {
		t.Fatalf("expected held ramp end 100, got %v", lvl)
	}
}
