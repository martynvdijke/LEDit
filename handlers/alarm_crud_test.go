package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func alarmCreateForm() url.Values {
	form := url.Values{}
	form.Set("name", "Wake up")
	form.Set("enabled", "on")
	for _, d := range []string{"1", "2", "3", "4", "5"} {
		form.Add("days", d)
	}
	form.Set("start", "06:30")
	form.Set("end", "07:00")
	form.Set("wake_source_type", "systemstats")
	form.Set("wake_source_id", "0")
	form.Set("brightness_enabled", "on")
	form.Set("brightness_start", "1")
	form.Set("brightness_end", "100")
	form.Set("brightness_ramp_seconds", "600")
	return form
}

func alarmAuthedRequest(t *testing.T, srv *Server, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	session := loginAsAdmin(t, srv)
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestAlarmCRUDRequiresAuth(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	w := doRequest(t, srv, http.MethodGet, "/admin/alarms", "")
	if w.Code != http.StatusFound {
		t.Fatalf("anonymous GET /admin/alarms: expected 302, got %d", w.Code)
	}
}

func TestAlarmCreateRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	form := alarmCreateForm()
	w := alarmAuthedRequest(t, srv, http.MethodPost, "/admin/alarms/new", form)
	if w.Code != http.StatusFound {
		t.Fatalf("create: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/admin/alarms" {
		t.Fatalf("create redirect: got %q", loc)
	}
	ctx := context.Background()
	count, _ := srv.DB.WakeAlarm.Query().Count(ctx)
	if count != 1 {
		t.Fatalf("expected 1 alarm, got %d", count)
	}
	a := srv.DB.WakeAlarm.Query().FirstX(ctx)
	if a.Name != "Wake up" || a.Start != "06:30" || a.End != "07:00" {
		t.Fatalf("unexpected alarm %+v", a)
	}
	if a.Days != "[1,2,3,4,5]" {
		t.Fatalf("days JSON = %q", a.Days)
	}
	if !a.BrightnessEnabled || a.BrightnessStart != 1 || a.BrightnessEnd != 100 || a.BrightnessRampSeconds != 600 {
		t.Fatalf("unexpected brightness %+v", a)
	}

	// List renders the persisted alarm.
	lw := alarmAuthedRequest(t, srv, http.MethodGet, "/admin/alarms", nil)
	if lw.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d body %s", lw.Code, lw.Body.String())
	}
	if !strings.Contains(lw.Body.String(), "Wake up") {
		t.Fatal("list should show the created alarm")
	}
}

func TestAlarmValidationRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(url.Values)
	}{
		{"empty days", func(f url.Values) { f.Del("days") }},
		{"invalid start", func(f url.Values) { f.Set("start", "25:00") }},
		{"equal start and end", func(f url.Values) { f.Set("end", "06:30") }},
		{"unknown source", func(f url.Values) { f.Set("wake_source_id", "999") }},
		{"brightness out of range", func(f url.Values) { f.Set("brightness_start", "150") }},
		{"ramp out of range", func(f url.Values) { f.Set("brightness_ramp_seconds", "4000") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newEventRuleAuthTestServer(t)
			form := alarmCreateForm()
			tc.mutate(form)
			w := alarmAuthedRequest(t, srv, http.MethodPost, "/admin/alarms/new", form)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body %s", w.Code, w.Body.String())
			}
			count, _ := srv.DB.WakeAlarm.Query().Count(context.Background())
			if count != 0 {
				t.Fatalf("invalid alarm must not persist, got %d", count)
			}
		})
	}
}

func TestBackupWakeAlarmRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	w := alarmAuthedRequest(t, srv, http.MethodPost, "/admin/alarms/new", alarmCreateForm())
	if w.Code != http.StatusFound {
		t.Fatalf("create: expected 302, got %d", w.Code)
	}
	bundle, err := srv.ExportBundle(false, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if arr := bundle.Entities["wakealarm"]; len(arr) != 1 {
		t.Fatalf("expected 1 exported alarm, got %d", len(arr))
	}
	ctx := context.Background()
	if _, err := srv.DB.WakeAlarm.Delete().Exec(ctx); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	res := srv.ImportBundle(bundle, false)
	if res.FailedType != "" {
		t.Fatalf("import failed at %q: %s", res.FailedType, res.Error)
	}
	got := srv.DB.WakeAlarm.Query().FirstX(ctx)
	if got.Name != "Wake up" || got.Days != "[1,2,3,4,5]" || got.Start != "06:30" || got.End != "07:00" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.BrightnessEnabled || got.BrightnessStart != 1 || got.BrightnessEnd != 100 || got.BrightnessRampSeconds != 600 {
		t.Fatalf("brightness mismatch: %+v", got)
	}
}

func TestAlarmDeleteRemovesRow(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	w := alarmAuthedRequest(t, srv, http.MethodPost, "/admin/alarms/new", alarmCreateForm())
	if w.Code != http.StatusFound {
		t.Fatalf("create: expected 302, got %d", w.Code)
	}
	a := srv.DB.WakeAlarm.Query().FirstX(context.Background())
	del := alarmAuthedRequest(t, srv, http.MethodPost, "/admin/alarms/1/delete", nil)
	if del.Code != http.StatusFound {
		t.Fatalf("delete: expected 302, got %d", del.Code)
	}
	if a.ID != 1 {
		t.Fatalf("expected id 1, got %d", a.ID)
	}
	count, _ := srv.DB.WakeAlarm.Query().Count(context.Background())
	if count != 0 {
		t.Fatalf("expected 0 alarms after delete, got %d", count)
	}
}
