package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ScheduleWindow defines when a playlist is eligible for display.
// Days: 0=Sunday ... 6=Saturday (matching time.Weekday).
// Start inclusive, End exclusive, both HH:MM. When End <= Start the window
// wraps past midnight (e.g. 22:00-06:00).
type ScheduleWindow struct {
	Days     []int  `json:"days"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Priority int    `json:"priority"`
}

// PlaylistSchedule is the minimal view needed to resolve schedules. It mirrors
// the ent Playlist fields used by the resolver so the pure function can be
// tested without DB types.
type PlaylistSchedule struct {
	ID      int              `json:"id"`
	Name    string           `json:"name"`
	Enabled bool             `json:"enabled"`
	Windows []ScheduleWindow `json:"windows"`
	Order   int              `json:"order"` // candidate order for tie-break
}

// Clock abstracts time for testing. Production uses SystemClock.
type Clock interface {
	Now() time.Time
}

// SystemClock returns wall time in server-local zone.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// FixedClock returns a fixed time for tests.
type FixedClock struct {
	T time.Time
}

func (f FixedClock) Now() time.Time { return f.T }

// ScheduleNow is the function used by feed resolution. Overridable in tests.
var ScheduleNow = func() time.Time { return time.Now() }

func serverZoneLabel() string {
	z := time.Now().Location().String()
	if z == "" {
		return "Local"
	}
	return z
}

// ParseScheduleWindows decodes a JSON array string into windows.
func ParseScheduleWindows(s string) ([]ScheduleWindow, error) {
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "[]" {
		return []ScheduleWindow{}, nil
	}
	var out []ScheduleWindow
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("invalid schedule_windows JSON: %w", err)
	}
	if out == nil {
		return []ScheduleWindow{}, nil
	}
	return out, nil
}

// ValidateWindows checks window semantics and caps (32 windows max).
func ValidateWindows(windows []ScheduleWindow) error {
	if len(windows) > 32 {
		return fmt.Errorf("too many schedule windows: %d (max 32)", len(windows))
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
		startMin, err := parseHM(w.Start)
		if err != nil {
			return fmt.Errorf("window %d: invalid start %q: %w", i, w.Start, err)
		}
		endMin, err := parseHM(w.End)
		if err != nil {
			return fmt.Errorf("window %d: invalid end %q: %w", i, w.End, err)
		}
		if startMin == endMin {
			return fmt.Errorf("window %d: start and end must differ", i)
		}
	}
	return nil
}

// ValidateScheduledCandidates checks device candidate cap (16 max) and duplicate IDs.
func ValidateScheduledCandidates(ids []int) error {
	if len(ids) > 16 {
		return fmt.Errorf("too many scheduled candidates: %d (max 16)", len(ids))
	}
	seen := map[int]bool{}
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("duplicate scheduled candidate %d", id)
		}
		seen[id] = true
	}
	return nil
}

func parseHM(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

// WindowMatches reports whether now (in server-local zone) matches w.
func WindowMatches(now time.Time, w ScheduleWindow) bool {
	startMin, err := parseHM(w.Start)
	if err != nil {
		return false
	}
	endMin, err := parseHM(w.End)
	if err != nil {
		return false
	}
	wd := int(now.Weekday())
	nowMin := now.Hour()*60 + now.Minute()
	containsDay := func(d int) bool {
		for _, v := range w.Days {
			if v == d {
				return true
			}
		}
		return false
	}
	if endMin > startMin {
		// Non-wrapping: [start, end) on listed days.
		if !containsDay(wd) {
			return false
		}
		return nowMin >= startMin && nowMin < endMin
	}
	// Wrapping: 22:00-06:00 . Matches if (today in days && time >= start)
	// OR (prev day in days && time < end) OR (today in days && time < end)
	// The latter covers the forgiving case where user lists the morning day.
	// We implement OR of both interpretations to be user-friendly.
	if containsDay(wd) && (nowMin >= startMin || nowMin < endMin) {
		return true
	}
	prevWd := (wd + 6) % 7
	if containsDay(prevWd) && nowMin < endMin {
		return true
	}
	return false
}

// ResolveScheduledPlaylist picks the active playlist for now among candidates.
// Empty Windows means always eligible (priority 0). Disabled playlists are
// skipped. Returns nil if nothing matches.
func ResolveScheduledPlaylist(now time.Time, candidates []PlaylistSchedule) *PlaylistSchedule {
	var best *PlaylistSchedule
	bestPriority := -1 << 30
	bestOrder := 1 << 30
	bestWindowIdx := 1 << 30

	for idx, c := range candidates {
		if !c.Enabled {
			continue
		}
		// Empty windows = always eligible.
		if len(c.Windows) == 0 {
			prio := 0
			// candidate order is idx, window idx 0.
			if best == nil || prio > bestPriority || (prio == bestPriority && idx < bestOrder) {
				cp := c
				cp.Order = idx
				best = &cp
				bestPriority = prio
				bestOrder = idx
				bestWindowIdx = 0
			}
			continue
		}
		for wi, w := range c.Windows {
			if !WindowMatches(now, w) {
				continue
			}
			prio := w.Priority
			replace := false
			if best == nil {
				replace = true
			} else if prio > bestPriority {
				replace = true
			} else if prio == bestPriority {
				if idx < bestOrder {
					replace = true
				} else if idx == bestOrder && wi < bestWindowIdx {
					replace = true
				}
			}
			if replace {
				cp := c
				cp.Order = idx
				best = &cp
				bestPriority = prio
				bestOrder = idx
				bestWindowIdx = wi
			}
		}
	}
	return best
}

// NextSwitchTime computes the next time after now where the active playlist
// could change (earliest start or end of any window in next 7 days). Returns
// zero time if no windows. Used for debug/badge "until HH:MM".
func NextSwitchTime(now time.Time, candidates []PlaylistSchedule) time.Time {
	if len(candidates) == 0 {
		return time.Time{}
	}
	// Scan next 7*24*60 minutes for a change in active playlist.
	// Cheap: at most 10080 iterations with trivial compares; only called for
	// debug/badge rendering, not per-feed tick. We can optimize by scanning
	// window boundaries directly, but minute scan is simple and correct.
	activeNow := ResolveScheduledPlaylist(now, candidates)
	for i := 1; i <= 7*24*60; i++ {
		t := now.Add(time.Duration(i) * time.Minute)
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location())
		an := ResolveScheduledPlaylist(t, candidates)
		if (activeNow == nil) != (an == nil) {
			return t
		}
		if activeNow != nil && an != nil && an.ID != activeNow.ID {
			return t
		}
	}
	return time.Time{}
}
