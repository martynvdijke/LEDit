package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func enableMetrics(srv *Server) {
	st := EnsureOutboundSettings(srv.DB)
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetMetricsEnabled(true).Exec(context.Background())
	GlobalMetricsSink.SetEnabled(true)
}

func fetchMetrics(t *testing.T, srv *Server) (int, string, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/metrics", nil)
	srv.MetricsHandler(c)
	return w.Code, w.Body.String(), w.Header()
}

func TestMetricsDisabled404(t *testing.T) {
	srv := newTestServerWithDB(t)
	Health.Reset()
	GlobalMetricsSink.Reset()
	t.Cleanup(func() { Health.Reset(); GlobalMetricsSink.Reset() })
	st := EnsureOutboundSettings(srv.DB)
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetMetricsEnabled(false).Exec(context.Background())
	GlobalMetricsSink.SetEnabled(false)
	code, _, _ := fetchMetrics(t, srv)
	if code != 404 {
		t.Fatalf(" want 404 got %d", code)
	}
}

func TestMetricsEnabled200AndContentType(t *testing.T) {
	srv := newTestServerWithDB(t)
	Health.Reset()
	GlobalMetricsSink.Reset()
	t.Cleanup(func() { Health.Reset(); GlobalMetricsSink.Reset() })
	enableMetrics(srv)
	code, _, hdr := fetchMetrics(t, srv)
	if code != 200 {
		t.Fatalf("want 200 got %d", code)
	}
	if ct := hdr.Get("Content-Type"); ct != "text/plain; version=0.0.4" {
		t.Fatalf("content-type %q", ct)
	}
}

func TestMetricsParseableAndHelpType(t *testing.T) {
	srv := newTestServerWithDB(t)
	Health.Reset()
	GlobalMetricsSink.Reset()
	t.Cleanup(func() { Health.Reset(); GlobalMetricsSink.Reset() })
	enableMetrics(srv)
	Health.RecordSuccess("weather:1", 10*time.Millisecond)
	srv.DB.DeviceSettings.Create().SetName("dev1").SetFramesServed(5).SaveX(context.Background())

	_, body, _ := fetchMetrics(t, srv)
	lines := strings.Split(strings.TrimSpace(body), "\n")
	reSeries := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{[^}]*\})? [-+]?[0-9.eE+\-]+$`)
	helpSeen := map[string]bool{}
	typeSeen := map[string]bool{}
	seriesSeen := map[string]bool{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "# HELP ") {
			parts := strings.SplitN(l, " ", 4)
			if len(parts) < 3 {
				t.Fatalf("bad HELP line %q", l)
			}
			name := parts[2]
			if helpSeen[name] {
				t.Fatalf("duplicate HELP for %s", name)
			}
			helpSeen[name] = true
			continue
		}
		if strings.HasPrefix(l, "# TYPE ") {
			parts := strings.SplitN(l, " ", 4)
			name := parts[2]
			if typeSeen[name] {
				t.Fatalf("duplicate TYPE for %s", name)
			}
			typeSeen[name] = true
			if !helpSeen[name] {
				t.Fatalf("TYPE without HELP for %s", name)
			}
			continue
		}
		if strings.HasPrefix(l, "#") {
			t.Fatalf("unexpected comment %q", l)
		}
		if !reSeries.MatchString(l) {
			t.Fatalf("unparseable series %q", l)
		}
		if seriesSeen[l] {
			t.Fatalf("duplicate series %q", l)
		}
		seriesSeen[l] = true
		name := l
		if idx := strings.Index(l, "{"); idx != -1 {
			name = l[:idx]
		} else if idx := strings.Index(l, " "); idx != -1 {
			name = l[:idx]
		}
		if !helpSeen[name] || !typeSeen[name] {
			t.Fatalf("series %q without HELP/TYPE", l)
		}
	}
	for k := range helpSeen {
		if !typeSeen[k] {
			t.Fatalf("HELP without TYPE %s", k)
		}
	}
}

func TestMetricsSourceAndDeviceSeries(t *testing.T) {
	srv := newTestServerWithDB(t)
	Health.Reset()
	GlobalMetricsSink.Reset()
	t.Cleanup(func() { Health.Reset(); GlobalMetricsSink.Reset() })
	enableMetrics(srv)

	Health.RecordSuccess("weather:1", 20*time.Millisecond)
	Health.RecordFailure("clock:2", fmt.Errorf("fail"), 5*time.Millisecond)
	Health.RecordMatrixCacheHit()
	Health.RecordMatrixCacheHit()
	Health.RecordMatrixCacheMiss()
	GlobalMetricsSink.Handle(Event{Type: "test.event"})

	now := time.Now().Add(-10 * time.Second)
	dev := srv.DB.DeviceSettings.Create().SetName("my dev").SetFramesServed(42).SetNillableLastSeenAt(&now).SetFirmwareVersion("1.2.0").SaveX(context.Background())
	Health.RecordSuccess(fmt.Sprintf("device:%d", dev.ID), 1*time.Millisecond)

	_, body, _ := fetchMetrics(t, srv)
	id := fmt.Sprint(dev.ID)
	expect := []string{
		`ledit_source_frames_total{source="weather:1"} 1`,
		`ledit_source_errors_total{source="clock:2"} 1`,
		`ledit_source_render_duration_seconds{source="weather:1"}`,
		`ledit_source_last_success_timestamp_seconds{source="weather:1"}`,
		`ledit_matrix_cache_hits_total 2`,
		`ledit_matrix_cache_misses_total 1`,
		`ledit_device_frames_served_total{device="` + id + `"} 42`,
		`ledit_device_last_seen_timestamp_seconds{device="` + id + `"}`,
		`ledit_device_firmware_info{device="` + id + `",version="1.2.0"} 1`,
		`ledit_devices_total 1`,
		`ledit_device_online{device="` + id + `"} 1`,
		`ledit_events_total{event_type="test.event"} 1`,
	}
	for _, e := range expect {
		if !strings.Contains(body, e) {
			t.Fatalf("missing expected substring %q in body:\n%s", e, body)
		}
	}
}

func TestMetricsNoDeviceNameLeak(t *testing.T) {
	srv := newTestServerWithDB(t)
	Health.Reset()
	GlobalMetricsSink.Reset()
	t.Cleanup(func() { Health.Reset(); GlobalMetricsSink.Reset() })
	enableMetrics(srv)
	tricky := `a b "quoted"`
	srv.DB.DeviceSettings.Create().SetName(tricky).SetFirmwareVersion("9.9.9").SaveX(context.Background())
	_, body, _ := fetchMetrics(t, srv)
	if strings.Contains(body, tricky) || strings.Contains(body, "a b") {
		t.Fatalf("device name leaked into metrics: %q", body)
	}
	if !strings.Contains(body, `version="9.9.9"`) {
		t.Fatalf("firmware version missing")
	}
}

func TestMetricsStableOutput(t *testing.T) {
	srv := newTestServerWithDB(t)
	Health.Reset()
	GlobalMetricsSink.Reset()
	t.Cleanup(func() { Health.Reset(); GlobalMetricsSink.Reset() })
	enableMetrics(srv)
	Health.RecordSuccess("weather:1", 5*time.Millisecond)
	srv.DB.DeviceSettings.Create().SetName("d1").SaveX(context.Background())
	_, body1, _ := fetchMetrics(t, srv)
	_, body2, _ := fetchMetrics(t, srv)
	if body1 != body2 {
		t.Fatalf("unstable output diff:\n---1\n%s\n---2\n%s", body1, body2)
	}
}
