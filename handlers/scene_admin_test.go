package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func sceneFormRequest(t *testing.T, srv *Server, cookie *http.Cookie, method, path, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form))
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestAdminSceneCreateRejectsMissingName(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := sceneFormRequest(t, srv, cookie, http.MethodPost, "/admin/scenes/new", "name=&enabled=on&triggers=%5B%5D")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", w.Code)
	}
}

func TestAdminSceneCreateRejectsInvalidOperator(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	form := url.Values{}
	form.Set("name", "Bad")
	form.Set("enabled", "on")
	form.Set("triggers", `[{"op":"all-of","conditions":[{"entity_id":"sensor.x","operator":"~=","value":"1"}]}]`)
	w := sceneFormRequest(t, srv, cookie, http.MethodPost, "/admin/scenes/new", form.Encode())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid operator, got %d", w.Code)
	}
}

func TestAdminSceneCreateRoundTrip(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	srv.DB.GeneralSettings.Create().SetTimeout(60).SetRandom(false).SaveX(srv.Ctx)
	triggers := `[{"op":"all-of","conditions":[{"entity_id":"sensor.temp","operator":">","value":"28"}]}]`
	form := url.Values{}
	form.Set("name", "Hot Room")
	form.Set("enabled", "on")
	form.Set("priority", "10")
	form.Set("ttl_seconds", "300")
	form.Set("triggers", triggers)
	form.Set("source_type", "clock")
	form.Set("source_id", "0")
	form.Set("brightness_level", "40")
	form.Set("overlay_text", "Cool down")
	w := sceneFormRequest(t, srv, cookie, http.MethodPost, "/admin/scenes/new", form.Encode())
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d body %s", w.Code, w.Body.String())
	}
	rows, err := srv.DB.Scene.Query().All(srv.Ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected one scene row, got %d err %v", len(rows), err)
	}
	sc, perr := sceneFromEnt(rows[0])
	if perr != nil {
		t.Fatalf("parse scene: %v", perr)
	}
	if sc.Name != "Hot Room" || sc.Priority != 10 || sc.TTLSeconds == nil || *sc.TTLSeconds != 300 {
		t.Fatalf("unexpected scene fields: %+v", sc)
	}
	if sc.Actions.BrightnessLevel == nil || *sc.Actions.BrightnessLevel != 40 || sc.Actions.OverlayText != "Cool down" {
		t.Fatalf("unexpected scene actions: %+v", sc.Actions)
	}
	if len(sc.Triggers) != 1 || len(sc.Triggers[0].Conditions) != 1 {
		t.Fatalf("unexpected triggers: %+v", sc.Triggers)
	}

	// List page renders the scene with a per-condition badge.
	lw := sceneFormRequest(t, srv, cookie, http.MethodGet, "/admin/scenes", "")
	if lw.Code != http.StatusOK {
		t.Fatalf("list page status %d", lw.Code)
	}
	if !strings.Contains(lw.Body.String(), "Hot Room") {
		t.Fatal("list page should show the scene name")
	}
}

func TestAdminScenePreview(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	row := srv.DB.Scene.Create().
		SetName("Previewable").SetEnabled(true).SetPriority(1).
		SetTriggers(`[]`).SetActions(`{"source_type":"clock","source_id":0}`).SaveX(srv.Ctx)

	old := SceneSourceResolver
	SceneSourceResolver = func(s *Scene) (*sourceWithName, bool) {
		return &sourceWithName{Name: "Clock", cacheKey: "clock:0"}, true
	}
	t.Cleanup(func() { SceneSourceResolver = old })

	w := sceneFormRequest(t, srv, cookie, http.MethodPost, "/api/scenes/"+itoa(row.ID)+"/preview", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected preview 200, got %d body %s", w.Code, w.Body.String())
	}
	if GlobalFeed.GetSceneSource() == nil {
		// Preview broadcasts to every registered controller; none may be
		// registered in this test, so just assert the manager state.
		if src := ActiveSceneSource(); src == nil {
			t.Fatal("expected preview source in the scene manager")
		}
	}
}

func TestSceneConditionStatus(t *testing.T) {
	states := map[string]string{"sensor.temp": "26", "binary_sensor.motion": "on", "sensor.unknown": "unavailable"}
	cases := []struct {
		cond   SceneCondition
		status string
	}{
		{SceneCondition{EntityID: "sensor.temp", Operator: ">", Value: "28"}, "false"},
		{SceneCondition{EntityID: "binary_sensor.motion", Operator: "==", Value: "detected"}, "false"},
		{SceneCondition{EntityID: "binary_sensor.motion", Operator: "==", Value: "on"}, "true"},
		{SceneCondition{EntityID: "sensor.unknown", Operator: "==", Value: "x"}, "unknown"},
		{SceneCondition{EntityID: "missing.entity", Operator: "==", Value: "x"}, "unknown"},
	}
	for _, c := range cases {
		if got, _ := sceneConditionStatus(states, c.cond); got != c.status {
			t.Errorf("sceneConditionStatus(%+v) = %q, want %q", c.cond, got, c.status)
		}
	}
}
