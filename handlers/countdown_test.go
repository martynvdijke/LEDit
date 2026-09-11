package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/generalsettings"
)

func newCountdownServer(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New(), WSHub: NewWSHub(client)}
	admin := srv.Router.Group("/admin")
	admin.POST("/countdowns/new", srv.AdminCountdownCreate)
	admin.POST("/countdowns/:id/edit", srv.AdminCountdownUpdate)
	admin.POST("/countdowns/:id/delete", srv.AdminCountdownDelete)
	return srv
}

func countdownPost(t *testing.T, srv *Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func countdownRows(t *testing.T, srv *Server) []*ent.Countdown {
	t.Helper()
	rows, err := srv.DB.Countdown.Query().All(srv.Ctx)
	if err != nil {
		t.Fatalf("query countdowns: %v", err)
	}
	return rows
}

func TestCountdownCreateDefaults(t *testing.T) {
	srv := newCountdownServer(t)
	w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":        {"Launch"},
		"target_time": {"2030-01-01T00:00"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", w.Code, w.Body.String())
	}
	rows := countdownRows(t, srv)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	cd := rows[0]
	if cd.Granularity != "seconds" || cd.Direction != "down" || cd.Completion != "now" {
		t.Errorf("defaults = %s/%s/%s, want seconds/down/now", cd.Granularity, cd.Direction, cd.Completion)
	}
	if cd.CompletionMessage != "" || cd.Timezone != "" {
		t.Errorf("default message/timezone = %q/%q, want empty", cd.CompletionMessage, cd.Timezone)
	}
}

func TestCountdownCreateAllOptions(t *testing.T) {
	srv := newCountdownServer(t)
	w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":               {"Launch"},
		"target_time":        {"2030-01-01T00:00"},
		"label":              {"Go"},
		"enabled":            {"on"},
		"granularity":        {"minutes"},
		"direction":          {"up"},
		"completion":         {"message"},
		"completion_message": {"Launch!"},
		"timezone":           {"Europe/Paris"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", w.Code, w.Body.String())
	}
	rows := countdownRows(t, srv)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	cd := rows[0]
	if cd.Granularity != "minutes" || cd.Direction != "up" || cd.Completion != "message" {
		t.Errorf("options = %s/%s/%s", cd.Granularity, cd.Direction, cd.Completion)
	}
	if cd.CompletionMessage != "Launch!" || cd.Timezone != "Europe/Paris" || cd.Label != "Go" {
		t.Errorf("message/timezone/label = %q/%q/%q", cd.CompletionMessage, cd.Timezone, cd.Label)
	}
}

func TestCountdownValidationRejects(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
	}{
		{"missing target", url.Values{"name": {"x"}}},
		{"invalid enum", url.Values{"name": {"x"}, "target_time": {"2030-01-01T00:00"}, "granularity": {"weeks"}}},
		{"invalid timezone", url.Values{"name": {"x"}, "target_time": {"2030-01-01T00:00"}, "timezone": {"Mars/Olympus"}}},
		{"message without text", url.Values{"name": {"x"}, "target_time": {"2030-01-01T00:00"}, "completion": {"message"}}},
		{"message too long", url.Values{"name": {"x"}, "target_time": {"2030-01-01T00:00"}, "completion": {"message"}, "completion_message": {strings.Repeat("a", 33)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCountdownServer(t)
			w := countdownPost(t, srv, "/admin/countdowns/new", tc.form)
			if w.Code != http.StatusFound {
				t.Fatalf("status = %d, want redirect", w.Code)
			}
			if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "/admin/countdowns/new") {
				t.Errorf("redirect = %q, want new form", loc)
			}
			if rows := countdownRows(t, srv); len(rows) != 0 {
				t.Errorf("rows = %d, want 0", len(rows))
			}
		})
	}
}

func TestCountdownTimezoneInterpretation(t *testing.T) {
	srv := newCountdownServer(t)
	w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":        {"NYE"},
		"target_time": {"2030-01-01T00:00"},
		"timezone":    {"America/New_York"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	rows := countdownRows(t, srv)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	want := time.Date(2030, 1, 1, 0, 0, 0, 0, loc)
	if !rows[0].TargetTime.Equal(want) {
		t.Errorf("target = %v, want %v", rows[0].TargetTime, want)
	}
}

func TestCountdownUpdateAndDeleteRoundTrip(t *testing.T) {
	srv := newCountdownServer(t)
	if w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":        {"Old"},
		"target_time": {"2030-01-01T00:00"},
	}); w.Code != http.StatusFound {
		t.Fatalf("create status = %d", w.Code)
	}
	rows := countdownRows(t, srv)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	id := rows[0].ID

	editPath := fmt.Sprintf("/admin/countdowns/%d/edit", id)
	if w := countdownPost(t, srv, editPath, url.Values{
		"name":               {"New"},
		"target_time":        {"2031-06-15T09:30"},
		"label":              {"Updated"},
		"enabled":            {"on"},
		"granularity":        {"hours"},
		"direction":          {"down"},
		"completion":         {"hide"},
		"completion_message": {""},
		"timezone":           {"UTC"},
	}); w.Code != http.StatusFound {
		t.Fatalf("update status = %d (%s)", w.Code, w.Body.String())
	}
	updated, err := srv.DB.Countdown.Get(srv.Ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if updated.Name != "New" || updated.Label != "Updated" || !updated.Enabled {
		t.Errorf("update fields = %q/%q/%v", updated.Name, updated.Label, updated.Enabled)
	}
	if updated.Granularity != "hours" || updated.Completion != "hide" || updated.Timezone != "UTC" {
		t.Errorf("update options = %s/%s/%s", updated.Granularity, updated.Completion, updated.Timezone)
	}

	deletePath := fmt.Sprintf("/admin/countdowns/%d/delete", id)
	if w := countdownPost(t, srv, deletePath, url.Values{}); w.Code != http.StatusFound {
		t.Fatalf("delete status = %d", w.Code)
	}
	if rows := countdownRows(t, srv); len(rows) != 0 {
		t.Errorf("rows after delete = %d, want 0", len(rows))
	}
}

func TestCountdownDisabledExcludedFromFeed(t *testing.T) {
	srv := newCountdownServer(t)
	if w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":        {"Enabled"},
		"target_time": {"2030-01-01T00:00"},
		"enabled":     {"on"},
	}); w.Code != http.StatusFound {
		t.Fatalf("create enabled status = %d", w.Code)
	}
	if w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":        {"Disabled"},
		"target_time": {"2030-01-01T00:00"},
	}); w.Code != http.StatusFound {
		t.Fatalf("create disabled status = %d", w.Code)
	}

	gs, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithCountdowns().Only(srv.Ctx)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	keys := map[string]bool{}
	for _, s := range srv.WSHub.loadSources(gs) {
		keys[s.cacheKey] = true
	}
	enabled, _ := srv.DB.Countdown.Query().All(srv.Ctx)
	if len(enabled) != 2 {
		t.Fatalf("countdown rows = %d, want 2", len(enabled))
	}
	foundEnabled := false
	for _, cd := range enabled {
		key := fmt.Sprintf("countdown:%d", cd.ID)
		if cd.Enabled {
			foundEnabled = keys[key]
		} else if keys[key] {
			t.Errorf("disabled countdown %d should be excluded from feed", cd.ID)
		}
	}
	if !foundEnabled {
		t.Error("enabled countdown should be present in feed")
	}
}

func TestCountdownIntegrationFeedPlaylistMatrix(t *testing.T) {
	srv := newCountdownServer(t)

	// Enabled countdown with non-default options.
	w := countdownPost(t, srv, "/admin/countdowns/new", url.Values{
		"name":               {"Launch"},
		"target_time":        {"2030-01-01T00:00"},
		"label":              {"Go"},
		"enabled":            {"on"},
		"granularity":        {"minutes"},
		"direction":          {"down"},
		"completion":         {"message"},
		"completion_message": {"Doors open"},
		"timezone":           {"UTC"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d", w.Code)
	}
	cd := countdownRows(t, srv)[0]
	wantKey := fmt.Sprintf("countdown:%d", cd.ID)

	load := func() *ent.GeneralSettings {
		t.Helper()
		gs, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithCountdowns().WithMatrixLayouts().Only(srv.Ctx)
		if err != nil {
			t.Fatalf("load settings: %v", err)
		}
		return gs
	}

	// Global feed carries the configured options.
	gs := load()
	var feedSrc datasource.Datasource
	for _, s := range srv.WSHub.loadSources(gs) {
		if s.cacheKey == wantKey {
			feedSrc = s.Source
		}
	}
	cds, ok := feedSrc.(*datasource.CountdownDS)
	if !ok {
		t.Fatalf("countdown source not found in global feed")
	}
	if cds.Granularity != "minutes" || cds.Direction != "down" || cds.Completion != "message" || cds.CompletionMessage != "Doors open" {
		t.Errorf("feed options = %s/%s/%s/%q", cds.Granularity, cds.Direction, cds.Completion, cds.CompletionMessage)
	}

	// Device playlist resolves the countdown in authored order.
	pl := srv.DB.Playlist.Create().
		SetName("cd playlist").
		SetItems(fmt.Sprintf(`[{"source_type":"countdown","source_id":%d}]`, cd.ID)).
		SetEnabled(true).
		SaveX(srv.Ctx)
	dev := srv.DB.DeviceSettings.Create().
		SetName("CD Device").SetToken("cd-token").SetEnabled(true).
		SetContentMode("playlist").SetPlaylistID(pl.ID).
		SaveX(srv.Ctx)
	composed := srv.WSHub.composeDeviceSources(dev, load())
	found := false
	for _, s := range composed {
		if s.cacheKey == wantKey {
			found = true
		}
	}
	if !found {
		t.Error("playlist feed should include the countdown source")
	}

	// Matrix cell binding renders at a small cell size (forcing unit fallback
	// inside the cell renderer) without error.
	ml := srv.DB.MatrixLayout.Create().
		SetName("CD grid").SetRows(1).SetCols(3).SetGap(1).SetBackground("#282a36").
		SetBindings(fmt.Sprintf(`[{"row":0,"col":0,"source_type":"countdown","source_id":%d}]`, cd.ID)).
		SetEnabled(true).
		SaveX(srv.Ctx)
	mds := srv.WSHub.buildMatrixDS(load(), ml, 0)
	if mds == nil {
		t.Fatal("buildMatrixDS returned nil")
	}
	img, err := mds.GetPNG(30, 12)
	if err != nil {
		t.Fatalf("matrix render error: %v", err)
	}
	if img == nil || img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatalf("matrix render = %+v", img)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("matrix PNG decode: %v", err)
	}
}
