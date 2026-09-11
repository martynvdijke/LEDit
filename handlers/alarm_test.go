package handlers

import (
	"testing"
	"time"
)

// Tue 2026-09-15 (Fri 2026-09-11 is "today" per environment).
func alarmTestTime(t *testing.T, hh, mm int) time.Time {
	t.Helper()
	return time.Date(2026, 9, 15, hh, mm, 0, 0, time.Local)
}

func TestResolveActiveAlarmWeekdayMatch(t *testing.T) {
	alarms := []WakeAlarm{{ID: 1, Enabled: true, Days: []int{2}, Start: "06:30", End: "07:00"}}
	if got := ResolveActiveAlarm(alarmTestTime(t, 6, 45), alarms, nil); got == nil {
		t.Fatal("expected alarm active on listed weekday")
	}
}

func TestResolveActiveAlarmWrongDay(t *testing.T) {
	alarms := []WakeAlarm{{ID: 1, Enabled: true, Days: []int{1, 2, 3, 4, 5}, Start: "06:30", End: "07:00"}}
	// Sunday 2026-09-13.
	now := time.Date(2026, 9, 13, 6, 45, 0, 0, time.Local)
	if got := ResolveActiveAlarm(now, alarms, nil); got != nil {
		t.Fatal("expected no alarm on an unlisted weekday")
	}
}

func TestResolveActiveAlarmOvernightWrap(t *testing.T) {
	alarms := []WakeAlarm{{ID: 1, Enabled: true, Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "23:00", End: "06:00"}}
	now := time.Date(2026, 9, 15, 1, 0, 0, 0, time.Local)
	if got := ResolveActiveAlarm(now, alarms, nil); got == nil {
		t.Fatal("expected overnight alarm active at 01:00")
	}
}

func TestResolveActiveAlarmEndExclusive(t *testing.T) {
	alarms := []WakeAlarm{{ID: 1, Enabled: true, Days: []int{2}, Start: "06:30", End: "07:00"}}
	if got := ResolveActiveAlarm(alarmTestTime(t, 7, 0), alarms, nil); got != nil {
		t.Fatal("expected alarm inactive exactly at end")
	}
	if got := ResolveActiveAlarm(alarmTestTime(t, 6, 30), alarms, nil); got == nil {
		t.Fatal("expected alarm active exactly at start")
	}
}

func TestResolveActiveAlarmDisabledAndEmptyDaysInert(t *testing.T) {
	if got := ResolveActiveAlarm(alarmTestTime(t, 6, 45), []WakeAlarm{{ID: 1, Enabled: false, Days: []int{2}, Start: "06:30", End: "07:00"}}, nil); got != nil {
		t.Fatal("disabled alarm must be inert")
	}
	if got := ResolveActiveAlarm(alarmTestTime(t, 6, 45), []WakeAlarm{{ID: 1, Enabled: true, Days: nil, Start: "06:30", End: "07:00"}}, nil); got != nil {
		t.Fatal("alarm with empty days must be inert")
	}
}

func TestAlarmBrightnessRampInterpolation(t *testing.T) {
	a := &WakeAlarm{Enabled: true, Days: []int{2}, Start: "06:00", End: "07:00", BrightnessEnabled: true, BrightnessStart: 0, BrightnessEnd: 100, BrightnessRampSeconds: 600}
	mid := alarmTestTime(t, 6, 5) // 300s after 06:00 -> exactly 50
	if got := AlarmBrightnessLevel(mid, a); got == nil || *got != 50 {
		t.Fatalf("midpoint: got %v want 50", got)
	}
	atStart := alarmTestTime(t, 6, 0)
	if got := AlarmBrightnessLevel(atStart, a); got == nil || *got != 0 {
		t.Fatalf("ramp start: got %v want 0", got)
	}
	atEnd := alarmTestTime(t, 6, 10) // 600s -> ramp complete
	if got := AlarmBrightnessLevel(atEnd, a); got == nil || *got != 100 {
		t.Fatalf("ramp complete: got %v want 100", got)
	}
}

func TestAlarmBrightnessHoldsAfterRamp(t *testing.T) {
	a := &WakeAlarm{Enabled: true, Days: []int{2}, Start: "06:00", End: "08:00", BrightnessEnabled: true, BrightnessStart: 1, BrightnessEnd: 100, BrightnessRampSeconds: 600}
	late := alarmTestTime(t, 7, 30) // past the 600s ramp
	if got := AlarmBrightnessLevel(late, a); got == nil || *got != 100 {
		t.Fatalf("after ramp: got %v want 100", got)
	}
	// Spec scenario: 300s into a 1->100 ramp is approximately 50.
	mid := alarmTestTime(t, 6, 5)
	if got := AlarmBrightnessLevel(mid, a); got == nil || *got < 49 || *got > 51 {
		t.Fatalf("1->100 midpoint: got %v want ~50", got)
	}
}

func TestAlarmBrightnessNilWhenDisabled(t *testing.T) {
	a := &WakeAlarm{Enabled: true, Days: []int{2}, Start: "06:00", End: "07:00", BrightnessEnabled: false}
	if got := AlarmBrightnessLevel(alarmTestTime(t, 6, 30), a); got != nil {
		t.Fatalf("disabled ramp: got %v want nil", got)
	}
	if got := AlarmBrightnessLevel(alarmTestTime(t, 6, 30), nil); got != nil {
		t.Fatalf("nil alarm: got %v want nil", got)
	}
}

func TestAlarmBrightnessRampZeroHoldsEnd(t *testing.T) {
	a := &WakeAlarm{Enabled: true, Days: []int{2}, Start: "06:00", End: "07:00", BrightnessEnabled: true, BrightnessStart: 5, BrightnessEnd: 80, BrightnessRampSeconds: 0}
	if got := AlarmBrightnessLevel(alarmTestTime(t, 6, 1), a); got == nil || *got != 80 {
		t.Fatalf("zero ramp: got %v want 80", got)
	}
}

func TestDismissalOccurrenceScoping(t *testing.T) {
	a := WakeAlarm{ID: 7, Enabled: true, Days: []int{2, 3}, Start: "06:30", End: "07:00"}
	today := alarmTestTime(t, 6, 45)
	dismissed := map[string]bool{OccurrenceKey(a.ID, today, &a): true}
	if got := ResolveActiveAlarm(today, []WakeAlarm{a}, dismissed); got != nil {
		t.Fatal("dismissed occurrence should not resolve same day")
	}
	tomorrow := time.Date(2026, 9, 16, 6, 45, 0, 0, time.Local) // Wednesday
	if got := ResolveActiveAlarm(tomorrow, []WakeAlarm{a}, dismissed); got == nil {
		t.Fatal("dismissal must not suppress the next scheduled occurrence")
	}
}

func TestOvernightOccurrenceKeyUsesStartDate(t *testing.T) {
	a := WakeAlarm{ID: 3, Enabled: true, Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "23:00", End: "06:00"}
	night := time.Date(2026, 9, 14, 23, 30, 0, 0, time.Local)
	morning := time.Date(2026, 9, 15, 1, 0, 0, 0, time.Local)
	if OccurrenceKey(a.ID, night, &a) != OccurrenceKey(a.ID, morning, &a) {
		t.Fatal("overnight occurrence key should be stable across midnight")
	}
}

func TestAlarmManagerEvaluateTransitionsAndDismiss(t *testing.T) {
	m := NewAlarmManager()
	a := WakeAlarm{ID: 1, Enabled: true, Days: []int{2}, Start: "06:30", End: "07:00"}
	m.SetAlarms([]WakeAlarm{a})
	resolveCalls := 0
	resolve := func(*WakeAlarm) (*sourceWithName, bool) {
		resolveCalls++
		return &sourceWithName{Name: "Wake", cacheKey: "clock:0"}, true
	}

	before := alarmTestTime(t, 6, 0)
	if m.Evaluate(before, resolve) {
		t.Fatal("no change expected before window")
	}
	if _, _, ok := m.Active(); ok {
		t.Fatal("no active alarm before window")
	}

	now := alarmTestTime(t, 6, 45)
	if !m.Evaluate(now, resolve) {
		t.Fatal("expected activation transition")
	}
	if _, src, ok := m.Active(); !ok || src.Name != "Wake" {
		t.Fatal("expected active wake source after onset")
	}
	if m.Evaluate(now.Add(30*time.Second), resolve) {
		t.Fatal("no transition while same alarm stays active")
	}
	if resolveCalls != 1 {
		t.Fatalf("resolve should be called once on activation, got %d", resolveCalls)
	}

	m.Dismiss(now)
	if _, _, ok := m.Active(); ok {
		t.Fatal("dismissed alarm should clear immediately")
	}
	if m.Evaluate(now, resolve) {
		t.Fatal("dismissed occurrence must not re-activate")
	}

	// Window ends -> evaluate clears (already clear) and prunes the marker.
	after := alarmTestTime(t, 7, 0)
	m.Evaluate(after, resolve)
	m.mu.Lock()
	left := len(m.dismissed)
	m.mu.Unlock()
	if left != 0 {
		t.Fatalf("expected dismissed markers pruned after window, got %d", left)
	}
}

func TestAlarmManagerUnresolvableSourceFallsBack(t *testing.T) {
	m := NewAlarmManager()
	m.SetAlarms([]WakeAlarm{{ID: 1, Enabled: true, Days: []int{2}, Start: "06:30", End: "07:00"}})
	now := alarmTestTime(t, 6, 45)
	m.Evaluate(now, func(*WakeAlarm) (*sourceWithName, bool) { return nil, false })
	if _, _, ok := m.Active(); ok {
		t.Fatal("unresolvable wake source must not activate")
	}
}
