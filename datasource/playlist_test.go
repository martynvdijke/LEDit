package datasource

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePlaylistItems(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		wantLen int
		check   func(t *testing.T, items []PlaylistItem)
	}{
		{
			name:    "valid multi-item parse",
			raw:     `[{"source_type":"weather","source_id":3},{"source_type":"sonarr","source_id":1},{"source_type":"analog-clock","source_id":0}]`,
			wantLen: 3,
			check: func(t *testing.T, items []PlaylistItem) {
				if items[0].SourceType != "weather" || items[0].SourceID != 3 {
					t.Errorf("item 0 = %+v, want weather:3", items[0])
				}
				if items[2].SourceType != "analog-clock" || items[2].SourceID != 0 {
					t.Errorf("item 2 = %+v, want analog-clock:0", items[2])
				}
			},
		},
		{
			name:    "builtin systemstats valid",
			raw:     `[{"source_type":"systemstats","source_id":0}]`,
			wantLen: 1,
		},
		{
			name:    "empty array OK",
			raw:     `[]`,
			wantLen: 0,
		},
		{
			name:    "empty string treated as empty array",
			raw:     ``,
			wantLen: 0,
		},
		{
			name:    "whitespace treated as empty array",
			raw:     `   `,
			wantLen: 0,
		},
		{
			name:    "whitespace newline treated as empty array",
			raw:     "\n\t ",
			wantLen: 0,
		},
		{
			name:    "unknown source_type rejected",
			raw:     `[{"source_type":"unknown","source_id":1}]`,
			wantErr: true,
		},
		{
			name:    "unknown source_type among valid rejected",
			raw:     `[{"source_type":"weather","source_id":1},{"source_type":"bogus","source_id":2}]`,
			wantErr: true,
		},
		{
			name:    "malformed JSON rejected",
			raw:     `[{`,
			wantErr: true,
		},
		{
			name:    "not an array rejected",
			raw:     `{"source_type":"weather","source_id":1}`,
			wantErr: true,
		},
		{
			name:    "over-cap rejected",
			raw:     overCapJSON(65),
			wantErr: true,
		},
		{
			name:    "exactly cap OK",
			raw:     overCapJSON(64),
			wantLen: 64,
		},
		{
			name:    "empty string documented handling matches ParseBindings convention",
			raw:     `[]`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := ParsePlaylistItems(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got items %+v", items)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(items), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, items)
			}
		})
	}
}

func TestIsValidSourceType(t *testing.T) {
	for k := range KnownSourceTypes {
		if !IsValidSourceType(k) {
			t.Errorf("IsValidSourceType(%q) = false, want true", k)
		}
	}
	if IsValidSourceType("unknown") {
		t.Error("IsValidSourceType(unknown) = true, want false")
	}
	if IsValidSourceType("") {
		t.Error("IsValidSourceType empty = true, want false")
	}
	// Ensure canonical keys match codebase spellings (plural vs singular)
	if IsValidSourceType("image") {
		t.Error("image singular should not be valid; use images")
	}
	if !IsValidSourceType("images") {
		t.Error("images should be valid")
	}
	if IsValidSourceType("video") {
		t.Error("video singular should not be valid; use videos")
	}
}

func TestKnownSourceTypesNotEmpty(t *testing.T) {
	if len(KnownSourceTypes) == 0 {
		t.Fatal("KnownSourceTypes empty")
	}
	// Spot-check required types
	required := []string{"sonarr", "radarr", "weather", "analog-clock", "matrix-rain", "systemstats", "matrix", "pixelart", "genericapi", "aidigest"}
	for _, r := range required {
		if !KnownSourceTypes[r] {
			t.Errorf("KnownSourceTypes missing %q", r)
		}
	}
}

func overCapJSON(n int) string {
	items := make([]PlaylistItem, n)
	for i := range items {
		items[i] = PlaylistItem{SourceType: "weather", SourceID: i + 1}
	}
	b, _ := json.Marshal(items)
	return string(b)
}

// Ensure error messages are plain (no panic) and contain context.
func TestParsePlaylistItemsErrorMessages(t *testing.T) {
	_, err := ParsePlaylistItems(`[{"source_type":"nope","source_id":1}]`)
	if err == nil || !strings.Contains(err.Error(), "unknown source_type") {
		t.Fatalf("want unknown source_type error, got %v", err)
	}
	_, err = ParsePlaylistItems(`bad`)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("want JSON error, got %v", err)
	}
	_, err = ParsePlaylistItems(overCapJSON(65))
	if err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("want too many error, got %v", err)
	}
}
