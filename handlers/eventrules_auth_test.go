package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
)

func newEventRuleAuthTestServer(t *testing.T) *Server {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	srv := New(drv, nil)
	if _, err := srv.DB.GeneralSettings.Query().First(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	return srv
}

func TestEventRuleAuth_Anonymous(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   url.Values
	}{
		{"list GET", http.MethodGet, "/admin/eventrules", nil},
		{"new GET", http.MethodGet, "/admin/eventrules/new", nil},
		{"create POST", http.MethodPost, "/admin/eventrules/new", url.Values{"name": {"Test"}, "source_type": {"systemstats"}, "source_id": {"0"}, "condition": {`{"path":"cpu","operator":"gt","value":50}`}, "check_interval_seconds": {"30"}, "cooldown_seconds": {"0"}}},
		{"edit GET", http.MethodGet, "/admin/eventrules/1/edit", nil},
		{"update POST", http.MethodPost, "/admin/eventrules/1/edit", url.Values{"name": {"Test"}, "source_type": {"systemstats"}, "source_id": {"0"}, "condition": {`{"path":"cpu","operator":"gt","value":50}`}, "check_interval_seconds": {"30"}, "cooldown_seconds": {"0"}}},
		{"delete POST", http.MethodPost, "/admin/eventrules/1/delete", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newEventRuleAuthTestServer(t)
			var body string
			if tc.body != nil {
				body = tc.body.Encode()
			}
			w := doRequest(t, srv, tc.method, tc.path, body)
			if w.Code != http.StatusFound {
				t.Fatalf("%s %s anonymous: expected 302 redirect to login, got %d body %s", tc.method, tc.path, w.Code, w.Body.String())
			}
			loc := w.Header().Get("Location")
			if !strings.Contains(loc, "/login") && !strings.Contains(loc, "/setup") {
				t.Fatalf("expected redirect to /login or /setup, got %q (code %d)", loc, w.Code)
			}
		})
	}
}

func TestEventRuleAuth_AnonymousCreateNotPersisted(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	form := url.Values{}
	form.Set("name", "Anon")
	form.Set("source_type", "systemstats")
	form.Set("source_id", "0")
	form.Set("condition", `{"path":"cpu","operator":"gt","value":50}`)
	form.Set("check_interval_seconds", "30")
	form.Set("cooldown_seconds", "0")
	w := doRequest(t, srv, http.MethodPost, "/admin/eventrules/new", form.Encode())
	if w.Code != http.StatusFound {
		// Anonymous should be redirected, not create
	}
	count, _ := srv.DB.DisplayRule.Query().Count(context.Background())
	if count != 0 {
		t.Fatalf("expected 0 event rules after anonymous POST, got %d", count)
	}
}

func TestEventRuleAuth_AnonymousUpdateNotPersisted(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	// Seed a rule directly.
	ctx := context.Background()
	r := srv.DB.DisplayRule.Create().SetName("Original").SetEnabled(true).SetSourceType("systemstats").SetSourceID(0).SetCondition(`{"path":"cpu","operator":"gt","value":50}`).SetCheckIntervalSeconds(30).SetCooldownSeconds(0).SaveX(ctx)
	form := url.Values{}
	form.Set("name", "Hacked")
	form.Set("source_type", "systemstats")
	form.Set("source_id", "0")
	form.Set("condition", `{"path":"cpu","operator":"gt","value":99}`)
	form.Set("check_interval_seconds", "30")
	form.Set("cooldown_seconds", "0")
	w := doRequest(t, srv, http.MethodPost, fmt.Sprintf("/admin/eventrules/%d/edit", r.ID), form.Encode())
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	got := srv.DB.DisplayRule.GetX(ctx, r.ID)
	if got.Name != "Original" {
		t.Fatalf("anonymous update should not persist, got name %q", got.Name)
	}
}

func TestEventRuleAuth_AnonymousDeleteNotPersisted(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	ctx := context.Background()
	r := srv.DB.DisplayRule.Create().SetName("KeepMe").SetEnabled(true).SetSourceType("systemstats").SetSourceID(0).SetCondition(`{"path":"cpu","operator":"gt","value":50}`).SetCheckIntervalSeconds(30).SetCooldownSeconds(0).SaveX(ctx)
	w := doRequest(t, srv, http.MethodPost, fmt.Sprintf("/admin/eventrules/%d/delete", r.ID), "")
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	count, _ := srv.DB.DisplayRule.Query().Count(ctx)
	if count != 1 {
		t.Fatalf("anonymous delete should not remove row, count=%d", count)
	}
}

func TestEventRuleAuth_AuthenticatedList(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/admin/eventrules", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated GET /admin/eventrules: expected 200, got %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Event Rules") {
		t.Fatalf("expected page heading 'Event Rules', got body %q", body[:min(500, len(body))])
	}
}

func TestEventRuleAuth_CreateAndRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	form := url.Values{}
	form.Set("name", "CPU Watch")
	form.Set("source_type", "systemstats")
	form.Set("source_id", "0")
	form.Set("condition", `{"path":"cpu","operator":"gt","value":50}`)
	form.Set("check_interval_seconds", "30")
	form.Set("cooldown_seconds", "0")
	form.Set("enabled", "on")

	req := httptest.NewRequest(http.MethodPost, "/admin/eventrules/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("create: expected 302 redirect, got %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/admin/eventrules" {
		t.Fatalf("create: expected redirect to /admin/eventrules, got %q", loc)
	}

	count, _ := srv.DB.DisplayRule.Query().Count(ctx)
	if count != 1 {
		t.Fatalf("expected 1 event rule after create, got %d", count)
	}
	rule := srv.DB.DisplayRule.Query().FirstX(ctx)
	if rule.Name != "CPU Watch" {
		t.Fatalf("expected name CPU Watch, got %q", rule.Name)
	}
	if rule.SourceType != "systemstats" || rule.SourceID != 0 {
		t.Fatalf("unexpected source %s:%d", rule.SourceType, rule.SourceID)
	}
	if rule.CheckIntervalSeconds != 30 || rule.CooldownSeconds != 0 {
		t.Fatalf("unexpected intervals %d/%d", rule.CheckIntervalSeconds, rule.CooldownSeconds)
	}
	cond, err := datasource.ParseCondition(rule.Condition)
	if err != nil {
		t.Fatalf("stored condition failed to parse: %v (raw %q)", err, rule.Condition)
	}
	if cond.Path != "cpu" || cond.Operator != "gt" {
		t.Fatalf("unexpected parsed condition %+v", cond)
	}
	// numeric value may be float64 after JSON unmarshal
	val, ok := cond.Value.(float64)
	if !ok || val != 50 {
		t.Fatalf("unexpected condition value %v (%T)", cond.Value, cond.Value)
	}

	// Edit page should render.
	editReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/eventrules/%d/edit", rule.ID), nil)
	editReq.AddCookie(session)
	ew := httptest.NewRecorder()
	srv.ServeHTTP(ew, editReq)
	if ew.Code != http.StatusOK {
		t.Fatalf("edit page: expected 200, got %d body %s", ew.Code, ew.Body.String())
	}
}

func TestEventRuleAuth_InvalidConditionRejected(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	form := url.Values{}
	form.Set("name", "Bad Rule")
	form.Set("source_type", "systemstats")
	form.Set("source_id", "0")
	form.Set("condition", `not-json`)
	form.Set("check_interval_seconds", "30")
	form.Set("cooldown_seconds", "0")
	form.Set("enabled", "on")

	req := httptest.NewRequest(http.MethodPost, "/admin/eventrules/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	// Invalid condition should render form with 200 (not redirect), and flash + no persist.
	if w.Code != http.StatusOK {
		t.Fatalf("invalid condition: expected 200 (form re-render), got %d body %s", w.Code, w.Body.String())
	}
	count, _ := srv.DB.DisplayRule.Query().Count(ctx)
	if count != 0 {
		t.Fatalf("invalid condition should not persist, got %d rows", count)
	}
}

func TestEventRuleAuth_DeleteRemovesRow(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	// Create via authenticated POST.
	form := url.Values{}
	form.Set("name", "ToDelete")
	form.Set("source_type", "systemstats")
	form.Set("source_id", "0")
	form.Set("condition", `{"path":"cpu","operator":"gt","value":50}`)
	form.Set("check_interval_seconds", "30")
	form.Set("cooldown_seconds", "0")
	form.Set("enabled", "on")
	req := httptest.NewRequest(http.MethodPost, "/admin/eventrules/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("create for delete: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	rule := srv.DB.DisplayRule.Query().FirstX(ctx)

	delReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/eventrules/%d/delete", rule.ID), nil)
	delReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	delReq.AddCookie(session)
	dw := httptest.NewRecorder()
	srv.ServeHTTP(dw, delReq)
	if dw.Code != http.StatusFound {
		t.Fatalf("delete: expected 302, got %d body %s", dw.Code, dw.Body.String())
	}
	count, _ := srv.DB.DisplayRule.Query().Count(ctx)
	if count != 0 {
		t.Fatalf("expected 0 rules after delete, got %d", count)
	}
}
