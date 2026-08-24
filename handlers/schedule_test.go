package handlers

import (
	"testing"
	"time"
)

func mustTime(iso string) time.Time {
	// iso in "2006-01-02 15:04" local
	t, err := time.ParseInLocation("2006-01-02 15:04", iso, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestWindowMatches_Simple(t *testing.T) {
	w := ScheduleWindow{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "09:00"}
	if !WindowMatches(mustTime("2026-08-25 08:00"), w) { // Tuesday
		t.Fatalf("expected match Tuesday 08:00")
	}
	if WindowMatches(mustTime("2026-08-25 10:00"), w) {
		t.Fatalf("expected no match at 10:00")
	}
}

func TestWindowMatches_WeekdayMiss(t *testing.T) {
	w := ScheduleWindow{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "09:00"}
	sun := mustTime("2026-08-23 08:00") // Sunday
	if WindowMatches(sun, w) {
		t.Fatalf("expected no match Sunday 08:00, got match")
	}
}

func TestWindowMatches_EndExclusive(t *testing.T) {
	w := ScheduleWindow{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "09:00"}
	atEnd := mustTime("2026-08-25 09:00")
	if WindowMatches(atEnd, w) {
		t.Fatalf("end should be exclusive")
	}
	atStart := mustTime("2026-08-25 07:00")
	if !WindowMatches(atStart, w) {
		t.Fatalf("start should be inclusive")
	}
}

func TestWindowMatches_OvernightWrap(t *testing.T) {
	w := ScheduleWindow{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "06:00"}
	// Wednesday 01:30 should match (wrap) when Wednesday is in days
	if !WindowMatches(mustTime("2026-08-26 01:30"), w) {
		t.Fatalf("expected overnight wrap match Wed 01:30")
	}
	// Wednesday 23:00 should match
	if !WindowMatches(mustTime("2026-08-26 23:00"), w) {
		t.Fatalf("expected overnight wrap match Wed 23:00")
	}
	// Wednesday 07:00 should not
	if WindowMatches(mustTime("2026-08-26 07:00"), w) {
		t.Fatalf("expected no match Wed 07:00")
	}
	// Sunday 23:00 should not (Sunday not in days)
	if WindowMatches(mustTime("2026-08-23 23:00"), w) {
		t.Fatalf("expected no match Sunday 23:00")
	}
}

func TestWindowMatches_OvernightPrevDay(t *testing.T) {
	// Window only Monday 22:00-06:00 should still match Tuesday 01:00 via prev-day logic
	w := ScheduleWindow{Days: []int{1}, Start: "22:00", End: "06:00"}
	if !WindowMatches(mustTime("2026-08-25 01:00"), w) { // Tue 01:00, Mon is prev day
		t.Fatalf("expected overnight prev-day match Tue 01:00 via Mon window")
	}
}

func TestResolveScheduledPlaylist_SingleCandidate(t *testing.T) {
	a := PlaylistSchedule{ID: 1, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "09:00"}}}
	b := PlaylistSchedule{ID: 2, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "17:00"}}}
	cands := []PlaylistSchedule{a, b}
	active := ResolveScheduledPlaylist(mustTime("2026-08-25 08:00"), cands)
	if active == nil || active.ID != 1 {
		t.Fatalf("expected active 1 at 08:00, got %v", active)
	}
	active = ResolveScheduledPlaylist(mustTime("2026-08-25 12:00"), cands)
	if active == nil || active.ID != 2 {
		t.Fatalf("expected active 2 at 12:00, got %v", active)
	}
}

func TestResolveScheduledPlaylist_PriorityWins(t *testing.T) {
	a := PlaylistSchedule{ID: 1, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00", Priority: 0}}}
	b := PlaylistSchedule{ID: 2, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00", Priority: 10}}}
	active := ResolveScheduledPlaylist(mustTime("2026-08-24 08:00"), []PlaylistSchedule{a, b}) // Monday
	if active == nil || active.ID != 2 {
		t.Fatalf("expected priority win 2, got %v", active)
	}
}

func TestResolveScheduledPlaylist_CandidateOrderTieBreak(t *testing.T) {
	a := PlaylistSchedule{ID: 1, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00", Priority: 5}}}
	b := PlaylistSchedule{ID: 2, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00", Priority: 5}}}
	active := ResolveScheduledPlaylist(mustTime("2026-08-24 08:00"), []PlaylistSchedule{a, b})
	if active == nil || active.ID != 1 {
		t.Fatalf("expected candidate order tie-break 1, got %v", active)
	}
	active2 := ResolveScheduledPlaylist(mustTime("2026-08-24 08:00"), []PlaylistSchedule{b, a})
	if active2 == nil || active2.ID != 2 {
		t.Fatalf("expected candidate order tie-break with reversed input 2, got %v", active2)
	}
}

func TestResolveScheduledPlaylist_NoMatch(t *testing.T) {
	a := PlaylistSchedule{ID: 1, Enabled: true, Windows: []ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00"}}}
	if got := ResolveScheduledPlaylist(mustTime("2026-08-24 10:00"), []PlaylistSchedule{a}); got != nil {
		t.Fatalf("expected nil no match, got %v", got)
	}
}

func TestResolveScheduledPlaylist_DisabledSkipped(t *testing.T) {
	a := PlaylistSchedule{ID: 1, Enabled: false, Windows: []ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00"}}}
	if got := ResolveScheduledPlaylist(mustTime("2026-08-24 08:00"), []PlaylistSchedule{a}); got != nil {
		t.Fatalf("expected disabled skipped, got %v", got)
	}
}

func TestResolveScheduledPlaylist_EmptyWindowsAlwaysEligible(t *testing.T) {
	a := PlaylistSchedule{ID: 1, Enabled: true, Windows: []ScheduleWindow{}}
	if got := ResolveScheduledPlaylist(mustTime("2026-08-24 03:00"), []PlaylistSchedule{a}); got == nil || got.ID != 1 {
		t.Fatalf("expected empty windows always eligible, got %v", got)
	}
}

func TestValidateWindows(t *testing.T) {
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{1}, Start: "07:00", End: "09:00"}}); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{}, Start: "07:00", End: "09:00"}}); err == nil {
		t.Fatalf("expected error empty days")
	}
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{7}, Start: "07:00", End: "09:00"}}); err == nil {
		t.Fatalf("expected error day out of range")
	}
	if err := ValidateWindows([]ScheduleWindow{{Days: []int{1}, Start: "25:00", End: "09:00"}}); err == nil {
		t.Fatalf("expected error invalid start")
	}
	// too many windows
	many := make([]ScheduleWindow, 33)
	for i := range many {
		many[i] = ScheduleWindow{Days: []int{1}, Start: "07:00", End: "08:00"}
	}
	if err := ValidateWindows(many); err == nil {
		t.Fatalf("expected error too many windows")
	}
}

func TestValidateScheduledCandidates(t *testing.T) {
	ids := make([]int, 16)
	for i := range ids {
		ids[i] = i + 1
	}
	if err := ValidateScheduledCandidates(ids); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	ids17 := make([]int, 17)
	for i := range ids17 {
		ids17[i] = i + 1
	}
	if err := ValidateScheduledCandidates(ids17); err == nil {
		t.Fatalf("expected cap error")
	}
	if err := ValidateScheduledCandidates([]int{1, 1}); err == nil {
		t.Fatalf("expected duplicate error")
	}
}

func TestParseScheduleWindows(t *testing.T) {
	ws, err := ParseScheduleWindows(`[{"days":[1],"start":"07:00","end":"09:00","priority":5}]`)
	if err != nil {
		t.Fatalf("parse err %v", err)
	}
	if len(ws) != 1 || ws[0].Priority != 5 {
		t.Fatalf("unexpected ws %v", ws)
	}
	ws2, err := ParseScheduleWindows("[]")
	if err != nil || len(ws2) != 0 {
		t.Fatalf("empty parse failed %v %v", ws2, err)
	}
	if _, err := ParseScheduleWindows("invalid"); err == nil {
		t.Fatalf("expected parse error")
	}
}
