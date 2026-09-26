package datasource

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildAdGuardRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "flat",
			body: `{"num_blocked_filtering":10,"num_dns_queries":100,"avg_processing_time":1.5}`,
			check: func(t *testing.T, rows [][2]string) {
				t.Helper()
				m := rowsMap(rows)
				if m["BLOCKED"] != "10" || m["QUERIES"] != "100" || m["AVG MS"] != "1.5" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "wrapped stats",
			body: `{"stats":{"num_blocked_filtering":10,"num_dns_queries":100,"avg_processing_time":1.5}}`,
			check: func(t *testing.T, rows [][2]string) {
				t.Helper()
				m := rowsMap(rows)
				if m["BLOCKED"] != "10" {
					t.Fatalf("wrapped rows %v", rows)
				}
			},
		},
		{name: "missing all fields", body: `{}`, wantErr: true},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildAdGuardRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildFrigateRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "cameras avg",
			body: `{"cameras":{"front":{"camera_fps":5.0},"back":{"camera_fps":3.0}}}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["CAMS"] != "2" {
					t.Fatalf("CAMS %v", rows)
				}
				if m["FPS"] != "4.0" {
					t.Fatalf("FPS %v want 4.0 got %v", rows, m["FPS"])
				}
			},
		},
		{
			name: "stats camera_fps",
			body: `{"stats":{"camera_fps":5.1}}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["FPS"] != "5.1" {
					t.Fatalf("FPS %v", rows)
				}
			},
		},
		{
			name: "top-level camera_fps",
			body: `{"camera_fps":2.5}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["FPS"] != "2.5" {
					t.Fatalf("FPS %v", rows)
				}
			},
		},
		{
			name: "empty unavailable",
			body: `{}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["FRIGATE"] != "unavailable" {
					t.Fatalf("want unavailable %v", rows)
				}
			},
		},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildFrigateRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildZigbee2MQTTRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "3 devices low 1",
			body: `[{"friendly_name":"a","linkquality":80},{"friendly_name":"b","linkquality":40},{"friendly_name":"c","linkquality":90}]`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["DEVICES"] != "3" || m["LOW"] != "1" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "all ok",
			body: `[{"linkquality":80},{"linkquality":90}]`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["LINK"] != "OK" {
					t.Fatalf("LINK %v", rows)
				}
			},
		},
		{
			name: "empty unavailable",
			body: `[]`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["ZIGBEE"] != "unavailable" {
					t.Fatalf("want unavailable %v", rows)
				}
			},
		},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildZigbee2MQTTRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildTransmissionRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "arguments shape",
			body: `{"arguments":{"torrents":[{"name":"a","percentDone":0.5,"status":4},{"name":"b","status":0}]}}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["TORRENTS"] != "2" || m["DOWNLOADING"] != "1" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "flat torrents",
			body: `{"torrents":[{"name":"a","status":4},{"name":"b","status":4}]}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["TORRENTS"] != "2" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{name: "missing torrents", body: `{"arguments":{}}`, wantErr: true},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildTransmissionRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildProxmoxRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "happy",
			body: `{"data":[{"node":"pve","status":"online","cpu":0.25}]}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["NODES"] != "1" || m["CPU%"] != "25%" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{name: "missing data", body: `{}`, wantErr: true},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildProxmoxRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildWasteRows(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Later\r\nDTSTART;VALUE=DATE:20260401\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nSUMMARY:Earlier\r\nDTSTART:20260315T080000\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "picks earliest",
			body: ics,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["NEXT"] != "Earlier" {
					t.Fatalf("NEXT %v", rows)
				}
				if m["DATE"] != "2026-03-15" {
					t.Fatalf("DATE %v", rows)
				}
			},
		},
		{name: "no VEVENT", body: "BEGIN:VCALENDAR\nEND:VCALENDAR\n", wantErr: true},
		{name: "missing SUMMARY", body: "BEGIN:VCALENDAR\nBEGIN:VEVENT\nDTSTART;VALUE=DATE:20260401\nEND:VEVENT\nEND:VCALENDAR\n", wantErr: true},
		{name: "missing DTSTART", body: "BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:Foo\nEND:VEVENT\nEND:VCALENDAR\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildWasteRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildAirQualityRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "owm",
			body: `{"list":[{"main":{"aqi":2},"components":{"pm2_5":12.3}}]}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["AQI"] != "2" || m["PM2.5"] != "12.3" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "openaq pm25",
			body: `{"results":[{"measurements":[{"parameter":"pm25","value":9.9}]}]}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["PM2.5"] != "9.9" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "openaq fallback first",
			body: `{"results":[{"measurements":[{"parameter":"o3","value":5.5},{"parameter":"no2","value":9.9}]}]}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["PM2.5"] != "5.5" {
					t.Fatalf("fallback got %v", rows)
				}
			},
		},
		{name: "unrecognized", body: `{}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildAirQualityRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func TestBuildParcelRows(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, rows [][2]string)
	}{
		{
			name: "shape1",
			body: `{"data":[{"tracking_number":"X1","status":"delivered"}]}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["TRACKING"] != "X1" || m["STATUS"] != "DELIVERED" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "shape2",
			body: `{"data":{"items":[{"tracking_number":"X2","status":"in_transit"}]}}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["TRACKING"] != "X2" || m["STATUS"] != "IN_TRANSIT" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{
			name: "shape3",
			body: `{"status":"pending","tracking_number":"X3"}`,
			check: func(t *testing.T, rows [][2]string) {
				m := rowsMap(rows)
				if m["TRACKING"] != "X3" || m["STATUS"] != "PENDING" {
					t.Fatalf("rows %v", rows)
				}
			},
		},
		{name: "unrecognized", body: `{"unknown":1}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := BuildParcelRows([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

func rowsMap(rows [][2]string) map[string]string {
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r[0]] = r[1]
	}
	return m
}

// CurrentState via httptest

func TestCurrentState_AdGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"num_blocked_filtering":10,"num_dns_queries":100,"avg_processing_time":1.5}`)
	}))
	defer srv.Close()
	ds := &AdGuardDS{URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["blocked"] != 10 || m["queries"] != 100 {
		t.Fatalf("got %v", m)
	}
}

func TestCurrentState_Frigate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"cameras":{"front":{"camera_fps":5.0},"back":{"camera_fps":3.0}}}`)
	}))
	defer srv.Close()
	ds := &FrigateDS{URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["cameras"] != 2 {
		t.Fatalf("cameras %v", m)
	}
	if v, ok := m["fps"].(float64); !ok || v != 4.0 {
		t.Fatalf("fps %v", m["fps"])
	}
}

func TestCurrentState_Transmission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"arguments":{"torrents":[{"status":4},{"status":0}]}}`)
	}))
	defer srv.Close()
	ds := &TransmissionDS{URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["torrents"] != 2 || m["downloading"] != 1 {
		t.Fatalf("got %v", m)
	}
}

func TestCurrentState_AirQuality(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"list":[{"main":{"aqi":2},"components":{"pm2_5":12.3}}]}`)
	}))
	defer srv.Close()
	ds := &AirQualityDS{URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["aqi"] != 2 {
		t.Fatalf("aqi %v", m)
	}
	_ = strings.Contains("", "")
}

func TestCurrentState_Parcel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"tracking_number":"X1","status":"delivered"}]}`)
	}))
	defer srv.Close()
	ds := &ParcelDS{URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["tracking_number"] != "X1" || m["status"] != "DELIVERED" {
		t.Fatalf("got %v", m)
	}
}
