package datasource

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func uptimeKumaHandler(manifest, heartbeat string, manifestStatus, hbStatus int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status-page/myslug" {
			if manifestStatus != 0 {
				http.Error(w, "err", manifestStatus)
				return
			}
			w.Write([]byte(manifest))
			return
		}
		if r.URL.Path == "/api/status-page/heartbeat/myslug" {
			if hbStatus != 0 {
				http.Error(w, "err", hbStatus)
				return
			}
			w.Write([]byte(heartbeat))
			return
		}
		http.NotFound(w, r)
	}
}

func TestUptimeKumaDegraded(t *testing.T) {
	manifest := `{"publicGroupList":[{"monitorList":[{"id":1,"name":"Site A"},{"id":2,"name":"Site B"}]}]}`
	heartbeat := `{"heartbeatList":{"1":[{"status":1}],"2":[{"status":0}]},"uptimeList":{"1_24":0.99,"2_24":0.5}}`
	srv := httptest.NewServer(uptimeKumaHandler(manifest, heartbeat, 0, 0))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m, _ := ds.CurrentState(context.Background())
	if m["up"] != 1 || m["down"] != 1 || m["total"] != 2 || m["status"] != "DEGRADED" {
		t.Fatalf("state %v", m)
	}
}

func TestUptimeKumaAllUp(t *testing.T) {
	manifest := `{"publicGroupList":[{"monitorList":[{"id":1,"name":"A"},{"id":2,"name":"B"}]}]}`
	heartbeat := `{"heartbeatList":{"1":[{"status":1}],"2":[{"status":1}]},"uptimeList":{"1_24":0.99,"2_24":0.99}}`
	srv := httptest.NewServer(uptimeKumaHandler(manifest, heartbeat, 0, 0))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	_, _ = ds.GetPNG(64, 64)
	m, _ := ds.CurrentState(context.Background())
	if m["status"] != "UP" {
		t.Fatalf("want UP got %v", m["status"])
	}
	if m["up"] != 2 || m["down"] != 0 {
		t.Fatalf("counts %v", m)
	}
}

func TestUptimeKumaAllDown(t *testing.T) {
	manifest := `{"publicGroupList":[{"monitorList":[{"id":1,"name":"A"},{"id":2,"name":"B"}]}]}`
	heartbeat := `{"heartbeatList":{"1":[{"status":0}],"2":[{"status":0}]},"uptimeList":{"1_24":0.1,"2_24":0.1}}`
	srv := httptest.NewServer(uptimeKumaHandler(manifest, heartbeat, 0, 0))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	m, _ := ds.CurrentState(context.Background())
	if m["status"] != "DOWN" {
		t.Fatalf("want DOWN got %v", m["status"])
	}
}

func TestUptimeKuma404Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m, _ := ds.CurrentState(context.Background())
	if m["status"] != "UNKNOWN" {
		t.Fatalf("want UNKNOWN got %v", m["status"])
	}
}

func TestUptimeKumaManifestFailureFallbackToIDs(t *testing.T) {
	heartbeat := `{"heartbeatList":{"1":[{"status":1}],"2":[{"status":1}]},"uptimeList":{"1_24":0.99,"2_24":0.99}}`
	srv := httptest.NewServer(uptimeKumaHandler(`bad`, heartbeat, 500, 0))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestUptimeKumaCurrentStateValues(t *testing.T) {
	manifest := `{"publicGroupList":[{"monitorList":[{"id":10,"name":"X"}]}]}`
	heartbeat := `{"heartbeatList":{"10":[{"status":1}]},"uptimeList":{"10_24":0.95}}`
	srv := httptest.NewServer(uptimeKumaHandler(manifest, heartbeat, 0, 0))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["up"] != 1 || m["total"] != 1 {
		t.Fatalf("m %v", m)
	}
}

func TestUptimeKumaFallbackMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	ds := &UptimeKumaDS{Token: "myslug", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
