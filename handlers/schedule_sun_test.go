package handlers

import (
	"encoding/json"
	"testing"
	"time"
)

func mustSunWindow(mode string, offset int) ScheduleWindow {
	return ScheduleWindow{Days: []int{1, 2, 3, 4, 5}, TimeMode: mode, OffsetMinutes: offset}
}

func TestWindowMatches_SunriseSunset(t *testing.T) {
	ResetSunState()
	ResetHolidays()
	lat, lon := 51.5074, -0.1278
	SetSunLocation(&lat, &lon)
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, loc) // Monday
	rise, set, _ := SunTimes(now)
	// sunrise window: should match at sunrise+5m
	wSunrise := mustSunWindow("sunrise", 0)
	atRise := time.Date(2026, 6, 22, 0, 0, 0, 0, loc).Add(time.Duration(rise+5) * time.Minute)
	if !WindowMatches(atRise, wSunrise) {
		t.Fatalf("sunrise window should match after sunrise rise=%d atRise=%v", rise, atRise)
	}
	beforeRise := time.Date(2026, 6, 22, 0, 0, 0, 0, loc).Add(time.Duration(rise-30) * time.Minute)
	if WindowMatches(beforeRise, wSunrise) {
		t.Fatalf("sunrise window should not match before sunrise")
	}

	// offset: sunrise -30 -> effective start 30m earlier, so beforeRise should now match with offset -30
	wOff := mustSunWindow("sunrise", -30)
	if !WindowMatches(beforeRise, wOff) {
		t.Fatalf("offset -30 should make beforeRise match")
	}

	// sunset window with wrap: after sunset should match
	wSunset := mustSunWindow("sunset", 0)
	afterSet := time.Date(2026, 6, 22, 0, 0, 0, 0, loc).Add(time.Duration(set+10) * time.Minute)
	if !WindowMatches(afterSet, wSunset) {
		t.Fatal("sunset should match after sunset")
	}
	// end-exclusive: exactly at next-day sunrise should not match (wrap end)
	riseNext, _, _ := SunTimes(now.Add(24 * time.Hour))
	atEnd := time.Date(2026, 6, 23, 0, 0, 0, 0, loc).Add(time.Duration(riseNext) * time.Minute)
	if WindowMatches(atEnd, wSunset) {
		t.Fatal("end exclusive should not match at next sunrise")
	}

	// holiday exclusion
	SetHolidays([]string{"2026-06-22"}, nil)
	if WindowMatches(atRise, wSunrise) {
		t.Fatal("holiday should suppress")
	}
	ResetHolidays()

	// fallback when location unset
	ResetSunState()
	wFallback := mustSunWindow("sunset", 0)
	insideFallback := time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local)
	if !WindowMatches(insideFallback, wFallback) {
		t.Fatal("fallback 07-19 should match 10:00")
	}
	outsideFallback := time.Date(2026, 6, 22, 20, 0, 0, 0, time.Local)
	if WindowMatches(outsideFallback, wFallback) {
		t.Fatal("fallback should not match 20:00")
	}

	// backward compat fixed JSON
	raw := `{"days":[1],"start":"07:00","end":"09:00"}`
	var w ScheduleWindow
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	if w.TimeMode != "" {
		t.Fatal("time_mode should be empty")
	}
	mon := time.Date(2026, 8, 24, 8, 0, 0, 0, time.Local) // Monday
	if !WindowMatches(mon, w) {
		t.Fatal("fixed compat should match")
	}
}

func TestValidateWindowsSun(t *testing.T) {
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{1}, TimeMode: "sunrise", OffsetMinutes: 200}}); err == nil {
		t.Fatal("expected offset error")
	}
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{1}, TimeMode: "bad"}}); err == nil {
		t.Fatal("expected mode error")
	}
	// sun mode ignores Start/End
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{1}, TimeMode: "sunrise"}}); err != nil {
		t.Fatalf("sun mode should not require start/end %v", err)
	}
}
