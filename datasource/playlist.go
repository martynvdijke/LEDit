package datasource

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PlaylistItem references a single datasource by endpoint type and DB id.
//
// Item shape mirrors matrix layout bindings minus row/col:
// {"source_type": "weather", "source_id": 3}
//
// Built-in sources are keyed via buildSourceIndex as "<type>:0", e.g.
// "analog-clock:0", "matrix-rain:0", and (after task 2.1) "systemstats:0".
// They use source_id 0, e.g. {"source_type":"analog-clock","source_id":0}
// or {"source_type":"systemstats","source_id":0}.
type PlaylistItem struct {
	SourceType string `json:"source_type"`
	SourceID   int    `json:"source_id"`
}

// MaxPlaylistItems caps the number of items in a playlist.
const MaxPlaylistItems = 64

// KnownSourceTypes enumerates every valid source_type endpoint key used by
// buildSourceIndex/bindingOptions (handlers/sources.go) and dsRegistry
// (handlers/datasource_registry.go). Keys are exactly as they appear in
// sourceIndex.byKey ("<endpoint>:<id>").
var KnownSourceTypes = map[string]bool{
	"analog-clock":   true,
	"matrix-rain":    true,
	"systemstats":    true,
	"sonarr":         true,
	"radarr":         true,
	"f1":             true,
	"weather":        true,
	"homeassistant":  true,
	"untappd":        true,
	"images":         true,
	"videos":         true,
	"crypto":         true,
	"stock":          true,
	"rssfeed":        true,
	"calendar":       true,
	"textslides":     true,
	"googlecalendar": true,
	"newsfeed":       true,
	"genericapi":     true,
	"matrix":         true,
	"countdown":      true,
	"aidigest":       true,
	"pixelart":       true,
}

// IsValidSourceType reports whether sourceType is a known endpoint key.
func IsValidSourceType(sourceType string) bool {
	return KnownSourceTypes[sourceType]
}

// ParsePlaylistItems parses raw as a JSON array of PlaylistItem, validates
// each item's source_type, and enforces the 64-item cap. Plain errors are
// returned (no panics), following ValidatePixelDoc / parseBindings style.
//
// Empty or whitespace-only input is treated as an empty playlist ("[]") to
// match how matrix bindings handle empty input (ParseBindings returns empty on
// blank). This lets callers store "" or "[]" interchangeably for "no items".
func ParsePlaylistItems(raw string) ([]PlaylistItem, error) {
	if strings.TrimSpace(raw) == "" {
		return []PlaylistItem{}, nil
	}
	var items []PlaylistItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("playlist items is not valid JSON: %w", err)
	}
	if items == nil {
		items = []PlaylistItem{}
	}
	if len(items) > MaxPlaylistItems {
		return nil, fmt.Errorf("too many playlist items (%d > %d)", len(items), MaxPlaylistItems)
	}
	for i, it := range items {
		if !IsValidSourceType(it.SourceType) {
			return nil, fmt.Errorf("playlist item %d: unknown source_type %q", i, it.SourceType)
		}
	}
	return items, nil
}
