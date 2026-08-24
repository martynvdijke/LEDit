package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

func newGroupTestServer(t *testing.T) *Server {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s.db?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", name)
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { drv.Close() })
	return New(drv, nil)
}

func loginGroupTest(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	w := doRequest(t, srv, http.MethodPost, "/login", "username=admin&password=ledit")
	if w.Code != http.StatusFound {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func apiCreateGroup(t *testing.T, srv *Server, cookie *http.Cookie, payload any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/groups", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestGroup_CreateSuccess(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "Kitchen"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var g map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &g)
	if g["name"] != "Kitchen" {
		t.Fatalf("name mismatch %v", g)
	}
}

func TestGroup_DuplicateName409(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "Dup"})
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", w.Code, w.Body.String())
	}
	w2 := apiCreateGroup(t, srv, cookie, map[string]any{"name": "dup"})
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate expected 409 got %d body %s", w2.Code, w2.Body.String())
	}
	// exact same case
	w3 := apiCreateGroup(t, srv, cookie, map[string]any{"name": "Dup"})
	if w3.Code != http.StatusConflict {
		t.Fatalf("duplicate exact expected 409 got %d", w3.Code)
	}
}

func TestGroup_Cap32(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	for i := 0; i < MaxGroups; i++ {
		w := apiCreateGroup(t, srv, cookie, map[string]any{"name": fmt.Sprintf("G%d", i)})
		if w.Code != http.StatusCreated {
			t.Fatalf("create %d failed %d %s", i, w.Code, w.Body.String())
		}
	}
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "Overflow"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cap expected 400 got %d body %s", w.Code, w.Body.String())
	}
}

func TestGroup_AssignDeviceToGroup(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "ZoneA"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create group %d %s", w.Code, w.Body.String())
	}
	var g struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &g)
	dev := srv.DB.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SaveX(srv.Ctx)
	// assign via membership endpoint
	b, _ := json.Marshal(map[string]any{"device_id": dev.ID})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/api/groups/%d/members", g.ID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign expected 200 got %d body %s", rec.Code, rec.Body.String())
	}
	updated, _ := srv.DB.DeviceSettings.Get(srv.Ctx, dev.ID)
	if updated.GroupID == nil || *updated.GroupID != g.ID {
		t.Fatalf("group_id not set %v", updated.GroupID)
	}
}

func TestGroup_AssignInvalidGroupID(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	dev := srv.DB.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SaveX(srv.Ctx)
	b, _ := json.Marshal(map[string]any{"device_id": dev.ID})
	req := httptest.NewRequest(http.MethodPost, "/admin/api/groups/99999/members", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid group expected 404 or 400 got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestGroup_DeleteUngroupsMembers(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	w := apiCreateGroup(t, srv, cookie, map[string]any{"name": "ToDelete"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var g struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &g)
	d1 := srv.DB.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	d2 := srv.DB.DeviceSettings.Create().SetName("D2").SetToken("tok2").SetEnabled(true).SetGroupID(g.ID).SaveX(srv.Ctx)
	// delete group
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/api/groups/%d", g.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete expected 200 got %d body %s", rec.Code, rec.Body.String())
	}
	// verify members group_id null
	for _, id := range []int{d1.ID, d2.ID} {
		dev, err := srv.DB.DeviceSettings.Get(srv.Ctx, id)
		if err != nil {
			t.Fatalf("get device %d: %v", id, err)
		}
		if dev.GroupID != nil {
			t.Fatalf("device %d still grouped %v", id, *dev.GroupID)
		}
	}
	// group should be gone
	if _, err := srv.DB.DeviceGroup.Get(srv.Ctx, g.ID); err == nil {
		t.Fatalf("group still exists")
	}
}

func TestGroup_InvalidGroupIDReturns404OnDelete(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/groups/99999", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d", rec.Code)
	}
}

func TestGroup_UnauthRedirect(t *testing.T) {
	srv := newGroupTestServer(t)
	w := apiCreateGroup(t, srv, nil, map[string]any{"name": "NoAuth"})
	if w.Code != http.StatusFound && w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth expected 302 or 401 got %d", w.Code)
	}
}
