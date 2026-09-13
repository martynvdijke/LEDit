package handlers

import (
	"testing"
	"time"
)

func TestSunTimes_Golden(t *testing.T) {
	ResetSunState()
	lat, lon := 51.5074, -0.1278
	SetSunLocation(&lat, &lon)
	// 2026-06-21 in London: sunrise ~04:43 BST (03:43 UTC), sunset ~21:21 BST (20:21 UTC)
	// Use Europe/London zone if available else Local; test with explicit location
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.Local
	}
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, loc)
	rise, set, ok := SunTimes(now)
	if !ok {
		t.Fatalf("expected ok")
	}
	// expected approx 04:43 = 283, 21:21 = 1281 local BST
	if abs(rise-283) > 10 {
		t.Fatalf("sunrise %d expected ~283", rise)
	}
	if abs(set-1281) > 10 {
		t.Fatalf("sunset %d expected ~1281", set)
	}
	// cache per day: second call same day should return same
	r2, s2, _ := SunTimes(now.Add(2 * time.Hour))
	if r2 != rise || s2 != set {
		t.Fatalf("cache failed")
	}
	ResetSunState()
	// after reset, no location => fallback
	r3, s3, ok3 := SunTimes(now)
	if ok3 || r3 != 7*60 || s3 != 19*60 {
		t.Fatalf("fallback after reset %d %d %v", r3, s3, ok3)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
