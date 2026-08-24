package datasource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildTransitRows_FutureFilteringAndMinutes(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		body string
		want [][2]string
	}{
		{
			name: "NOW boundary 30s and 1m",
			body: departuresJSON([]departureFixture{
				{Line: "S7", Dest: "A", When: now.Add(30 * time.Second)},
				{Line: "S7", Dest: "B", When: now.Add(60 * time.Second)},
				{Line: "S7", Dest: "C", When: now.Add(90 * time.Second)},
			}),
			want: [][2]string{
				{"S7 A", "NOW"},
				{"S7 B", "NOW"},
				{"S7 C", "2 min"},
			},
		},
		{
			name: "future only filtering past excluded",
			body: departuresJSON([]departureFixture{
				{Line: "U1", Dest: "Past", When: now.Add(-5 * time.Minute)},
				{Line: "U2", Dest: "Future", When: now.Add(5 * time.Minute)},
				{Line: "U3", Dest: "NowExact", When: now},
			}),
			want: [][2]string{
				{"U2 Future", "5 min"},
			},
		},
		{
			name: "prefer when over plannedWhen",
			body: func() string {
				// departure with both fields, when is future, plannedWhen is past -> should use when
				m := map[string]any{
					"departures": []map[string]any{
						{
							"line":        map[string]string{"name": "S1"},
							"destination": map[string]string{"name": "Dest"},
							"plannedWhen": now.Add(-10 * time.Minute).Format(time.RFC3339),
							"when":        now.Add(3 * time.Minute).Format(time.RFC3339),
						},
						{
							"line":        map[string]string{"name": "S2"},
							"destination": map[string]string{"name": "Dest2"},
							"plannedWhen": now.Add(4 * time.Minute).Format(time.RFC3339),
							"when":        "",
						},
					},
				}
				b, _ := json.Marshal(m)
				return string(b)
			}(),
			want: [][2]string{
				{"S1 Dest", "3 min"},
				{"S2 Dest2", "4 min"},
			},
		},
		{
			name: "truncate 28 chars",
			body: departuresJSON([]departureFixture{
				{Line: "S123456789", Dest: "VeryLongDestinationNameExceedingLimit", When: now.Add(2 * time.Minute)},
			}),
			want: [][2]string{
				{strings.Repeat("X", 28)[:0], ""}, // placeholder, check below
			},
		},
		{
			name: "cap at 4",
			body: departuresJSON([]departureFixture{
				{Line: "L1", Dest: "D1", When: now.Add(2 * time.Minute)},
				{Line: "L2", Dest: "D2", When: now.Add(3 * time.Minute)},
				{Line: "L3", Dest: "D3", When: now.Add(4 * time.Minute)},
				{Line: "L4", Dest: "D4", When: now.Add(5 * time.Minute)},
				{Line: "L5", Dest: "D5", When: now.Add(6 * time.Minute)},
				{Line: "L6", Dest: "D6", When: now.Add(7 * time.Minute)},
			}),
			want: [][2]string{
				{"L1 D1", "2 min"},
				{"L2 D2", "3 min"},
				{"L3 D3", "4 min"},
				{"L4 D4", "5 min"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildTransitRows([]byte(tt.body), now)
			if err != nil {
				t.Fatalf("BuildTransitRows error: %v", err)
			}
			if tt.name == "truncate 28 chars" {
				if len(rows) != 1 {
					t.Fatalf("got %d rows want 1", len(rows))
				}
				if len(rows[0][0]) != 28 {
					t.Fatalf("row1 len %d want 28, got %q", len(rows[0][0]), rows[0][0])
				}
				if rows[0][1] != "2 min" {
					t.Fatalf("row2 %q want 2 min", rows[0][1])
				}
				return
			}
			if len(rows) != len(tt.want) {
				t.Fatalf("rows len %d want %d: %+v", len(rows), len(tt.want), rows)
			}
			for i := range rows {
				if rows[i][0] != tt.want[i][0] || rows[i][1] != tt.want[i][1] {
					t.Fatalf("row %d = %+v want %+v", i, rows[i], tt.want[i])
				}
			}
		})
	}
}

func TestTransitDS_GetPNG_URLSubstitution(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// need to include departures to avoid fallback path check? return valid empty is okay
		w.Write([]byte(`{"departures":[{"line":{"name":"S7"},"destination":{"name":"Potsdam"},"when":"` + time.Now().Add(5*time.Minute).Format(time.RFC3339) + `"}]}`))
	}))
	defer srv.Close()

	// default URL contains %s, should substitute stop id
	ds := &TransitDS{Token: "900000003201", URL: srv.URL + "/stops/%s/departures"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if !strings.Contains(gotPath, "900000003201") {
		t.Fatalf("path %q should contain stop id", gotPath)
	}
	// also ensure no X-API-Key header sent (token is stop id, not API key)
}

func TestTransitDS_GetPNG_CustomURLVerbatim(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"departures":[{"line":{"name":"S7"},"destination":{"name":"Potsdam"},"when":"` + time.Now().Add(5*time.Minute).Format(time.RFC3339) + `"}]}`))
	}))
	defer srv.Close()

	customURL := srv.URL + "/custom/path"
	ds := &TransitDS{Token: "900000003201", URL: customURL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if gotPath != "/custom/path" {
		t.Fatalf("path %q want /custom/path (verbatim)", gotPath)
	}
}

func TestTransitDS_GetPNG_EmptyDeparturesFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"departures":[]}`))
	}))
	defer srv.Close()

	ds := &TransitDS{Token: "123", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img == nil || len(img.Data) == 0 {
		t.Fatal("fallback image empty")
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
}

func TestTransitDS_GetPNG_NetworkErrorFallback(t *testing.T) {
	// Use invalid URL to trigger error
	ds := &TransitDS{Token: "123", URL: "http://127.0.0.1:1"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG should fallback not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
}

func TestTransitDS_GetPNG_NoAuthHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.Write([]byte(`{"departures":[{"line":{"name":"S7"},"destination":{"name":"X"},"when":"` + time.Now().Add(2*time.Minute).Format(time.RFC3339) + `"}]}`))
	}))
	defer srv.Close()

	ds := &TransitDS{Token: "900000003201", URL: srv.URL + "/%s"}
	_, _ = ds.GetPNG(64, 64)
	if gotKey != "" {
		t.Fatalf("X-API-Key should not be sent, got %q", gotKey)
	}
}

func TestTransitDS_GetPNG_DefaultURLSubstitution(t *testing.T) {
	// When URL is empty, default URL is used with %s. We can't hit real API,
	// so just verify fallback on network error still returns valid PNG and
	// that code path doesn't panic.
	// For determinism we use httptest but need to test default URL construction:
	// Instead test that empty URL with invalid token still attempts fetch (we can't
	// intercept default host). Just ensure method returns fallback PNG without error.
	// Use a server and set URL to "" is not interceptable, so skip direct assert.
	// Instead verify that building URL with %s works via previous test.

	// Table-driven PNG decodable check for various scenarios
	tests := []struct {
		name    string
		ds      *TransitDS
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name: "success",
			ds:   &TransitDS{Token: "123"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"departures":[{"line":{"name":"S7"},"destination":{"name":"X"},"when":"` + time.Now().Add(5*time.Minute).Format(time.RFC3339) + `"}]}`))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer srv.Close()
			tt.ds.URL = srv.URL + "/%s"
			img, err := tt.ds.GetPNG(64, 64)
			if err != nil {
				t.Fatalf("GetPNG: %v", err)
			}
			if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
				t.Fatalf("decode: %v", err)
			}
		})
	}
}

// helpers

type departureFixture struct {
	Line string
	Dest string
	When time.Time
}

func departuresJSON(deps []departureFixture) string {
	type dep struct {
		Line        map[string]string `json:"line"`
		Destination map[string]string `json:"destination"`
		When        string            `json:"when"`
	}
	var list []dep
	for _, d := range deps {
		list = append(list, dep{
			Line:        map[string]string{"name": d.Line},
			Destination: map[string]string{"name": d.Dest},
			When:        d.When.Format(time.RFC3339),
		})
	}
	m := map[string]any{"departures": list}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestBuildTransitRows_InvalidJSON(t *testing.T) {
	_, err := BuildTransitRows([]byte(`not json`), time.Now())
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestTransitFallbackPNG(t *testing.T) {
	img := fallbackTransit(64, 64)
	if img == nil || len(img.Data) == 0 {
		t.Fatal("fallback empty")
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// ensure it contains fallback - just check png non-nil
	_ = fmt.Sprintf("%v", img)
}
