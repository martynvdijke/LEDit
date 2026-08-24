package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGroupFeed_PauseCounts(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "BcastG"})
	if w.Code != 201 {
		t.Fatalf("create group %d %s", w.Code, w.Body.String())
	}
	var g struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &g)
	d1 := srv.DB.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	d2 := srv.DB.DeviceSettings.Create().SetName("D2").SetToken("tok2").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	_ = srv.DB.DeviceSettings.Create().SetName("D3").SetToken("tok3").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	// register 2 online
	fc1 := &FeedController{}
	fc2 := &FeedController{}
	registerDeviceFeed(d1.ID, fc1)
	registerDeviceFeed(d2.ID, fc2)
	t.Cleanup(func() { unregisterDeviceFeed(d1.ID); unregisterDeviceFeed(d2.ID) })

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/api/groups/%d/feed/pause", g.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("pause got %d body %s", rec.Code, rec.Body.String())
	}
	var res groupFeedResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Total != 3 || res.Sent != 2 || res.Offline != 1 {
		t.Fatalf("counts mismatch %+v", res)
	}
	if !fc1.IsPaused() || !fc2.IsPaused() {
		t.Fatalf("feed not paused")
	}
}

func TestGroupFeed_NextAdvances(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "NextG"})
	var g struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &g)
	d1 := srv.DB.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	d2 := srv.DB.DeviceSettings.Create().SetName("D2").SetToken("tok2").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	fc1 := &FeedController{}
	fc2 := &FeedController{}
	registerDeviceFeed(d1.ID, fc1)
	registerDeviceFeed(d2.ID, fc2)
	t.Cleanup(func() { unregisterDeviceFeed(d1.ID); unregisterDeviceFeed(d2.ID) })
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/api/groups/%d/feed/next", g.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("next %d %s", rec.Code, rec.Body.String())
	}
	var res groupFeedResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Total != 2 || res.Sent != 2 {
		t.Fatalf("next counts %+v", res)
	}
	if !fc1.ShouldSkip() || !fc2.ShouldSkip() {
		t.Fatalf("next did not set Skip")
	}
}

func TestGroupFeed_InvalidGroup404(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/groups/99999/feed/pause", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestGroupFeed_Viewer403(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	// create group as admin
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "AuthG"})
	var g struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &g)
	// unauthenticated request should redirect or 401
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/api/groups/%d/feed/pause", g.ID), nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound && rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer/unauth expected 302 or 401 got %d", rec.Code)
	}
}
