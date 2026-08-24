package handlers

import (
	"sync"
	"time"
)

type displayEvent struct {
	Source   string    `json:"source"`
	Time     time.Time `json:"time"`
	Duration float64   `json:"duration"`
}

var (
	analyticsMu sync.Mutex
	events      []displayEvent
	skipEvents  []skipEvent
	startTime   = time.Now()
)

type skipEvent struct {
	SourceType  string
	SourceID    int
	SourceLabel string
	Time        time.Time
}

var unattributedSkips int

func RecordSkip(sourceType string, sourceID int, label string) {
	analyticsMu.Lock()
	defer analyticsMu.Unlock()
	if sourceType == "" && label == "" {
		unattributedSkips++
		return
	}
	skipEvents = append(skipEvents, skipEvent{SourceType: sourceType, SourceID: sourceID, SourceLabel: label, Time: time.Now()})
	if len(skipEvents) > 1000 {
		skipEvents = skipEvents[len(skipEvents)-1000:]
	}
}

func GetUnattributedSkips() int {
	analyticsMu.Lock()
	defer analyticsMu.Unlock()
	return unattributedSkips
}

func TrackDisplay(source string, duration float64) {
	analyticsMu.Lock()
	defer analyticsMu.Unlock()
	events = append(events, displayEvent{
		Source:   source,
		Time:     time.Now(),
		Duration: duration,
	})
	if len(events) > 1000 {
		events = events[len(events)-1000:]
	}
}

type AnalyticsStats struct {
	TotalDisplays int            `json:"total_displays"`
	Uptime        string         `json:"uptime"`
	BySource      map[string]int `json:"by_source"`
	Recent        []displayEvent `json:"recent"`
}

func GetAnalytics() AnalyticsStats {
	analyticsMu.Lock()
	defer analyticsMu.Unlock()

	bySource := map[string]int{}
	for _, e := range events {
		bySource[e.Source]++
	}

	uptime := time.Since(startTime).Round(time.Second).String()

	recent := events
	if len(recent) > 20 {
		recent = recent[len(recent)-20:]
	}

	return AnalyticsStats{
		TotalDisplays: len(events),
		Uptime:        uptime,
		BySource:      bySource,
		Recent:        recent,
	}
}
