package datasource

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func dep(line, dest string, t time.Time) TransitDeparture {
	return TransitDeparture{Line: line, Destination: dest, Time: t}
}

func mustPNG(t *testing.T, data []byte) {
	t.Helper()
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
}

func TestBuildTransitRows_FilterCapPast(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	base := TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 4, TimeMode: "minutes"}

	tests := []struct {
		name string
		cfg  TransitConfig
		deps []TransitDeparture
		want [][2]string
	}{
		{
			name: "future only, past excluded",
			cfg:  base,
			deps: []TransitDeparture{
				dep("U1", "Past", now.Add(-5*time.Minute)),
				dep("U2", "NowExact", now),
				dep("U2", "Future", now.Add(5*time.Minute)),
			},
			want: [][2]string{{"U2 Future", "5 min"}},
		},
		{
			name: "sorted ascending and capped",
			cfg:  TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 2, TimeMode: "minutes"},
			deps: []TransitDeparture{
				dep("L4", "D4", now.Add(5*time.Minute)),
				dep("L1", "D1", now.Add(1*time.Minute)),
				dep("L3", "D3", now.Add(3*time.Minute)),
				dep("L2", "D2", now.Add(2*time.Minute)),
			},
			want: [][2]string{{"L1 D1", "NOW"}, {"L2 D2", "2 min"}},
		},
		{
			name: "route filter case-insensitive and trimmed",
			cfg:  TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 4, TimeMode: "minutes", RouteFilter: " s7,  u1 "},
			deps: []TransitDeparture{
				dep("S7", "A", now.Add(1*time.Minute)),
				dep("U2", "B", now.Add(2*time.Minute)),
				dep("u1", "C", now.Add(3*time.Minute)),
			},
			want: [][2]string{{"S7 A", "NOW"}, {"u1 C", "3 min"}},
		},
		{
			name: "empty filter keeps all",
			cfg:  base,
			deps: []TransitDeparture{
				dep("A", "1", now.Add(1*time.Minute)),
				dep("B", "2", now.Add(2*time.Minute)),
			},
			want: [][2]string{{"A 1", "NOW"}, {"B 2", "2 min"}},
		},
		{
			name: "missing line and destination becomes placeholder",
			cfg:  base,
			deps: []TransitDeparture{dep("", "", now.Add(2*time.Minute))},
			want: [][2]string{{"?", "2 min"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildTransitRows(tt.deps, tt.cfg, now)
			if err != nil {
				t.Fatalf("BuildTransitRows: %v", err)
			}
			if len(rows) != len(tt.want) {
				t.Fatalf("rows = %+v want %+v", rows, tt.want)
			}
			for i := range rows {
				if rows[i] != tt.want[i] {
					t.Fatalf("row %d = %+v want %+v", i, rows[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildTransitRows_WalkTimeAndNow(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	cfg := TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 4, TimeMode: "minutes", WalkTimeMin: 3}
	deps := []TransitDeparture{
		dep("A", "unreachable", now.Add(2*time.Minute)),
		dep("B", "now", now.Add(4*time.Minute)), // 1 min after effective now -> NOW
		dep("C", "later", now.Add(13*time.Minute)),
	}
	rows, err := BuildTransitRows(deps, cfg, now)
	if err != nil {
		t.Fatalf("BuildTransitRows: %v", err)
	}
	want := [][2]string{{"B now", "NOW"}, {"C later", "10 min"}}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v want %+v", rows, want)
	}
	for i := range rows {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %+v want %+v", i, rows[i], want[i])
		}
	}
}

func TestBuildTransitRows_ClockModeAndTimezone(t *testing.T) {
	// Server clock is UTC; agency zone is Europe/Berlin (+02:00 in August).
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	cfg := TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 4, TimeMode: "clock"}
	deps := []TransitDeparture{dep("S7", "Potsdam", time.Date(2026, 8, 23, 12, 5, 0, 0, time.UTC))}
	rows, err := BuildTransitRows(deps, cfg, now)
	if err != nil {
		t.Fatalf("BuildTransitRows: %v", err)
	}
	if len(rows) != 1 || rows[0][1] != "14:05" {
		t.Fatalf("rows = %+v want timing 14:05", rows)
	}
}

func TestBuildTransitRows_DSTTransition(t *testing.T) {
	// Europe/Berlin ends DST on 2026-10-25: 03:00 CEST -> 02:00 CET.
	cfg := TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 4, TimeMode: "clock"}
	before := time.Date(2026, 10, 25, 0, 55, 0, 0, time.UTC) // 02:55 CEST
	after := time.Date(2026, 10, 25, 1, 35, 0, 0, time.UTC)  // 02:35 CET
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	rows, err := BuildTransitRows([]TransitDeparture{
		dep("A", "before", before),
		dep("B", "after", after),
	}, cfg, now)
	if err != nil {
		t.Fatalf("BuildTransitRows: %v", err)
	}
	if len(rows) != 2 || rows[0][1] != "02:55" || rows[1][1] != "02:35" {
		t.Fatalf("rows = %+v want [02:55, 02:35]", rows)
	}
}

func TestBuildTransitRows_InvalidTimezone(t *testing.T) {
	_, err := BuildTransitRows(nil, TransitConfig{Timezone: "Not/AZone", MaxDepartures: 4, TimeMode: "minutes"}, time.Now())
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}

func TestBuildTransitRows_Truncation(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	cfg := TransitConfig{Timezone: "Europe/Berlin", MaxDepartures: 4, TimeMode: "minutes"}
	deps := []TransitDeparture{dep("S123456789", "VeryLongDestinationNameExceedingLimit", now.Add(2*time.Minute))}
	rows, err := BuildTransitRows(deps, cfg, now)
	if err != nil {
		t.Fatalf("BuildTransitRows: %v", err)
	}
	if len(rows) != 1 || len(rows[0][0]) != 28 {
		t.Fatalf("rows = %+v want 28-char label", rows)
	}
}

func TestParseTransitDepartures_Adapters(t *testing.T) {
	when := time.Date(2026, 8, 23, 12, 6, 0, 0, time.UTC).Format(time.RFC3339)

	tests := []struct {
		name     string
		provider string
		body     string
		wantLine string
		wantDest string
	}{
		{
			name:     "vbb",
			provider: "vbb",
			body:     `{"departures":[{"line":{"name":"S7"},"destination":{"name":"Potsdam"},"when":"` + when + `"}]}`,
			wantLine: "S7",
			wantDest: "Potsdam",
		},
		{
			name:     "vbb plannedWhen fallback",
			provider: "vbb",
			body:     `{"departures":[{"line":{"name":"U1"},"destination":{"name":"Warschauer"},"plannedWhen":"` + when + `"}]}`,
			wantLine: "U1",
			wantDest: "Warschauer",
		},
		{
			name:     "custom",
			provider: "custom",
			body:     `{"departures":[{"line":"S1","destination":"Oranienburg","time":"` + when + `"}]}`,
			wantLine: "S1",
			wantDest: "Oranienburg",
		},
		{
			name:     "transitland",
			provider: "transitland",
			body:     `{"stops":[{"departures":[{"route":{"route_short_name":"38"},"trip":{"trip_headsign":"Downtown"},"departure":{"scheduled":"` + when + `"}}]}]}`,
			wantLine: "38",
			wantDest: "Downtown",
		},
		{
			name:     "511",
			provider: "511",
			body:     `{"ServiceDelivery":{"StopMonitoringDelivery":[{"MonitoredStopVisit":[{"MonitoredVehicleJourney":{"LineRef":{"value":"N"},"MonitoredCall":{"DestinationDisplay":{"value":"Ocean Beach"},"ExpectedDepartureTime":"` + when + `"}}}]}]}}`,
			wantLine: "N",
			wantDest: "Ocean Beach",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, err := ParseTransitDepartures([]byte(tt.body), tt.provider)
			if err != nil {
				t.Fatalf("ParseTransitDepartures: %v", err)
			}
			if len(deps) != 1 {
				t.Fatalf("deps = %+v want 1", deps)
			}
			if deps[0].Line != tt.wantLine || deps[0].Destination != tt.wantDest {
				t.Fatalf("dep = %+v want line=%q dest=%q", deps[0], tt.wantLine, tt.wantDest)
			}
			if !deps[0].Time.Equal(mustParse(t, when)) {
				t.Fatalf("time = %v want %v", deps[0].Time, when)
			}
		})
	}
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

func TestParseTransitDepartures_MalformedJSON(t *testing.T) {
	if _, err := ParseTransitDepartures([]byte(`not json`), "vbb"); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseTransitDepartures_UnknownShapeIsEmpty(t *testing.T) {
	deps, err := ParseTransitDepartures([]byte(`{"unexpected":true}`), "vbb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("deps = %+v want empty", deps)
	}
}

func TestResolveTransitURL(t *testing.T) {
	got, err := resolveTransitURL(TransitConfig{Provider: "vbb", StopID: "900000003201"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "https://v6.vbb.transport.rest/stops/900000003201/departures" {
		t.Fatalf("default URL = %q", got)
	}

	got, err = resolveTransitURL(TransitConfig{Provider: "custom", StopID: "42", URL: "http://example.test/custom/path"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "http://example.test/custom/path" {
		t.Fatalf("verbatim URL = %q", got)
	}

	if _, err := resolveTransitURL(TransitConfig{Provider: "custom"}); err == nil {
		t.Fatal("expected error for custom provider without URL")
	}
}

func transitTestDS(srvURL, provider string) *TransitDS {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return &TransitDS{
		Token:         "900000003201",
		URL:           srvURL,
		Provider:      provider,
		MaxDepartures: 4,
		Timezone:      "UTC",
		TimeMode:      "minutes",
		now:           func() time.Time { return now },
	}
}

func TestTransitDS_GetPNG_URLSubstitution(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"departures":[{"line":{"name":"S7"},"destination":{"name":"Potsdam"},"when":"2026-08-23T12:05:00Z"}]}`)
	}))
	defer srv.Close()

	ds := transitTestDS(srv.URL+"/stops/%s/departures", "vbb")
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	mustPNG(t, img.Data)
	if gotPath != "/stops/900000003201/departures" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestTransitDS_GetPNG_CustomURLVerbatim(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"departures":[{"line":"S7","destination":"X","time":"2026-08-23T12:05:00Z"}]}`)
	}))
	defer srv.Close()

	ds := transitTestDS(srv.URL+"/custom/path", "custom")
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	mustPNG(t, img.Data)
	if gotPath != "/custom/path" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestTransitDS_GetPNG_APIKeyHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		fmt.Fprint(w, `{"departures":[]}`)
	}))
	defer srv.Close()

	ds := transitTestDS(srv.URL, "vbb")
	ds.APIKey = "secret-key"
	_, _ = ds.GetPNG(64, 64)
	if gotKey != "secret-key" {
		t.Fatalf("X-API-Key = %q", gotKey)
	}
}

func TestTransitDS_GetPNG_APIKeyAsQuery(t *testing.T) {
	for _, tc := range []struct{ provider, param string }{
		{"511", "api_key"},
		{"transitland", "apikey"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			var gotKey, gotHeader string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotKey = r.URL.Query().Get(tc.param)
				gotHeader = r.Header.Get("X-API-Key")
				fmt.Fprint(w, `{"departures":[]}`)
			}))
			defer srv.Close()

			ds := transitTestDS(srv.URL, tc.provider)
			ds.APIKey = "secret-key"
			_, _ = ds.GetPNG(64, 64)
			if gotKey != "secret-key" {
				t.Fatalf("%s = %q", tc.param, gotKey)
			}
			if gotHeader != "" {
				t.Fatalf("X-API-Key should not be set, got %q", gotHeader)
			}
		})
	}
}

func TestTransitDS_GetPNG_Fallbacks(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		ds     func(url string) *TransitDS
	}{
		{
			name: "empty departures",
			body: `{"departures":[]}`,
			ds:   func(u string) *TransitDS { return transitTestDS(u, "vbb") },
		},
		{
			name: "malformed JSON",
			body: `not json`,
			ds:   func(u string) *TransitDS { return transitTestDS(u, "vbb") },
		},
		{
			name:   "unauthorized",
			status: http.StatusUnauthorized,
			body:   `{}`,
			ds:     func(u string) *TransitDS { return transitTestDS(u, "vbb") },
		},
		{
			name:   "forbidden",
			status: http.StatusForbidden,
			body:   `{}`,
			ds:     func(u string) *TransitDS { return transitTestDS(u, "vbb") },
		},
		{
			name: "all departures in the past",
			body: `{"departures":[{"line":"S7","destination":"X","time":"2026-08-23T11:00:00Z"}]}`,
			ds:   func(u string) *TransitDS { return transitTestDS(u, "custom") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			img, err := tc.ds(srv.URL).GetPNG(64, 64)
			if err != nil {
				t.Fatalf("GetPNG should fall back, not error: %v", err)
			}
			if img == nil {
				t.Fatal("nil image")
			}
			mustPNG(t, img.Data)
		})
	}
}

func TestTransitDS_GetPNG_UnreachableHost(t *testing.T) {
	ds := &TransitDS{Token: "123", URL: "http://127.0.0.1:1", Provider: "vbb", Timezone: "UTC"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG should fall back: %v", err)
	}
	mustPNG(t, img.Data)
}

func TestTransitFallbackMessages(t *testing.T) {
	for _, msg := range []string{"unavailable", "no departures"} {
		img := fallbackTransit(64, 64, msg)
		if img == nil || len(img.Data) == 0 {
			t.Fatalf("fallback %q empty", msg)
		}
		mustPNG(t, img.Data)
	}
}
