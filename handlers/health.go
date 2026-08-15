package handlers

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/datasource"
)

// ewmaAlpha is the smoothing factor for the exponential moving average of
// render durations. Higher = more weight on the most recent sample.
const ewmaAlpha = 0.3

// SourceHealth is the recorded health state of a single datasource or device.
type SourceHealth struct {
	LastSuccessAt    time.Time
	LastError        string
	LastDuration     time.Duration
	ConsecutiveFails int
	Renders          int64
	Failures         int64
	EWMADurationMs   float64
	CacheHits        int64
	CacheMisses      int64
}

// HealthRegistry is an in-memory registry of per-source health, keyed by
// "<type>:<id>". It is safe for concurrent use and intentionally not
// persisted across restarts.
type HealthRegistry struct {
	mu              sync.RWMutex
	entries         map[string]*SourceHealth
	matrixCacheHits int64
	matrixCacheMiss int64
}

// NewHealthRegistry creates an empty registry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{entries: make(map[string]*SourceHealth)}
}

// Health is the process-wide health registry shared by the feed, preview
// endpoints and admin pages.
var Health = NewHealthRegistry()

// init wires the datasource panel-cache counters into the registry. The
// datasource package must not import handlers, so we register a hook here.
func init() {
	datasource.PanelCacheHook = func(hit bool) {
		if hit {
			Health.RecordMatrixCacheHit()
		} else {
			Health.RecordMatrixCacheMiss()
		}
	}
}

func (r *HealthRegistry) entry(key string) *SourceHealth {
	e, ok := r.entries[key]
	if !ok {
		e = &SourceHealth{}
		r.entries[key] = e
	}
	return e
}

// RecordSuccess records a successful render for key.
func (r *HealthRegistry) RecordSuccess(key string, dur time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entry(key)
	first := e.Renders == 0 && e.Failures == 0
	e.LastSuccessAt = time.Now()
	e.LastError = ""
	e.ConsecutiveFails = 0
	e.LastDuration = dur
	e.Renders++
	if first {
		e.EWMADurationMs = float64(dur.Microseconds()) / 1000.0
	} else {
		e.EWMADurationMs = ewmaAlpha*float64(dur.Microseconds())/1000.0 + (1-ewmaAlpha)*e.EWMADurationMs
	}
}

// RecordFailure records a failed render for key.
func (r *HealthRegistry) RecordFailure(key string, err error, dur time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entry(key)
	first := e.Renders == 0 && e.Failures == 0
	if err != nil {
		e.LastError = err.Error()
	}
	e.ConsecutiveFails++
	e.Failures++
	e.LastDuration = dur
	if first {
		e.EWMADurationMs = float64(dur.Microseconds()) / 1000.0
	} else {
		e.EWMADurationMs = ewmaAlpha*float64(dur.Microseconds())/1000.0 + (1-ewmaAlpha)*e.EWMADurationMs
	}
}

// RecordMatrixCacheHit increments the matrix panel cache hit counter.
func (r *HealthRegistry) RecordMatrixCacheHit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.matrixCacheHits++
}

// RecordMatrixCacheMiss increments the matrix panel cache miss counter.
func (r *HealthRegistry) RecordMatrixCacheMiss() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.matrixCacheMiss++
}

// CacheCounters returns the matrix panel cache hit and miss counts.
func (r *HealthRegistry) CacheCounters() (hits, misses int64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.matrixCacheHits, r.matrixCacheMiss
}

// Snapshot returns a deep copy of all health entries, safe to read freely.
func (r *HealthRegistry) Snapshot() map[string]SourceHealth {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]SourceHealth, len(r.entries))
	for k, e := range r.entries {
		out[k] = *e
	}
	return out
}

// Reset clears all entries and counters. Intended for tests.
func (r *HealthRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]*SourceHealth)
	r.matrixCacheHits = 0
	r.matrixCacheMiss = 0
}

// StatusOf classifies a health snapshot entry.
// green: no consecutive failures; yellow: 1-2 consecutive failures and at
// least one prior success; red: 3+ consecutive failures or a failure with no
// prior success.
func StatusOf(sh SourceHealth) string {
	if sh.ConsecutiveFails == 0 {
		return "green"
	}
	if sh.Renders == 0 {
		return "red"
	}
	if sh.ConsecutiveFails >= 3 {
		return "red"
	}
	return "yellow"
}

// classifySummary counts green/yellow/red entries in a snapshot.
func classifySummary(snap map[string]SourceHealth) (green, yellow, red int) {
	for _, sh := range snap {
		switch StatusOf(sh) {
		case "green":
			green++
		case "yellow":
			yellow++
		default:
			red++
		}
	}
	return
}

// deviceLiveness derives a fleet-liveness label for a device. A device is
// "alive" when last seen within 3x its refresh interval, "never" when it has
// no recorded last-seen, and "stale" otherwise.
func deviceLiveness(lastSeen *time.Time, refreshInterval int) string {
	if lastSeen == nil {
		return "never"
	}
	if refreshInterval <= 0 {
		refreshInterval = 60
	}
	if time.Since(*lastSeen) <= 3*time.Duration(refreshInterval)*time.Second {
		return "alive"
	}
	return "stale"
}

// endpointHealthKey maps a dashboard endpoint + id to the health-registry key
// used by the feed. Most endpoints share their registry key, but a few admin
// endpoints use a different type name than the datasource registry.
func endpointHealthKey(endpoint string, id int) string {
	switch endpoint {
	case "matrixlayout":
		endpoint = "matrix"
	case "countdowns":
		endpoint = "countdown"
	case "aidigests":
		endpoint = "aidigest"
	}
	return fmt.Sprintf("%s:%d", endpoint, id)
}

// healthRow is one row of the analytics render-metrics table.
type healthRow struct {
	Key            string
	Status         string
	Renders        int64
	Failures       int64
	LastDuration   time.Duration
	EWMADurationMs float64
	LastError      string
}

// sortedHealthRows flattens a health snapshot into a deterministic, sorted
// slice for template rendering (map ranges are unordered).
func sortedHealthRows(snap map[string]SourceHealth) []healthRow {
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]healthRow, 0, len(keys))
	for _, k := range keys {
		sh := snap[k]
		rows = append(rows, healthRow{
			Key:            k,
			Status:         StatusOf(sh),
			Renders:        sh.Renders,
			Failures:       sh.Failures,
			LastDuration:   sh.LastDuration,
			EWMADurationMs: sh.EWMADurationMs,
			LastError:      sh.LastError,
		})
	}
	return rows
}

// APIHealth returns the process-wide health registry as JSON. It is
// intentionally unauthenticated and read-only, mirroring the other /api
// endpoints exposed to hardware clients.
func (s *Server) APIHealth(c *gin.Context) {
	snap := Health.Snapshot()
	sources := make(map[string]SourceHealth, len(snap))
	for k, v := range snap {
		sources[k] = v
	}

	devices := map[string]any{}
	if devs, err := s.DB.DeviceSettings.Query().All(s.Ctx); err == nil {
		for _, d := range devs {
			key := fmt.Sprintf("device:%d", d.ID)
			devices[key] = map[string]any{
				"name":          d.Name,
				"enabled":       d.Enabled,
				"liveness":      deviceLiveness(d.LastSeenAt, d.RefreshInterval),
				"frames_served": d.FramesServed,
			}
		}
	}

	c.JSON(200, gin.H{
		"sources": sources,
		"devices": devices,
	})
}
