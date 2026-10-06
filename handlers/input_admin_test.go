package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newInputAdminTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv := newTestServerWithDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/input-bindings", srv.APIInputBindingList)
	r.POST("/admin/api/input-bindings", srv.APIInputBindingCreate)
	r.PUT("/admin/api/input-bindings/:id", srv.APIInputBindingUpdate)
	r.DELETE("/admin/api/input-bindings/:id", srv.APIInputBindingDelete)
	r.POST("/admin/api/input-bindings/:id/toggle", srv.APIInputBindingToggle)
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return srv, ts
}

func postInputBindingJSON(t *testing.T, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	var out map[string]any
	if resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return resp, out
}

func TestAPIInputBindingCRUD(t *testing.T) {
	srv, ts := newInputAdminTestServer(t)

	resp, created := postInputBindingJSON(t, ts.URL+"/admin/api/input-bindings",
		`{"source":"nfc","event":"tap","match":"04a1b2c3","action":"{\"kind\":\"notification\",\"message\":\"tag seen\"}","order":2}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	id, ok := created["id"].(float64)
	if !ok || id == 0 {
		t.Fatalf("created id missing: %v", created)
	}
	if got := created["enabled"]; got != true {
		t.Fatalf("created enabled = %v, want true", got)
	}

	listResp, err := http.Get(ts.URL + "/admin/api/input-bindings")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/input-bindings/1",
		strings.NewReader(`{"source":"nfc","event":"tap","match":"04a1b2c3","action":"{\"kind\":\"notification\",\"message\":\"updated\"}","order":5,"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", putResp.StatusCode)
	}

	toggleResp, _ := postInputBindingJSON(t, ts.URL+"/admin/api/input-bindings/1/toggle", "")
	if toggleResp.StatusCode != http.StatusOK {
		t.Fatalf("toggle status = %d, want 200", toggleResp.StatusCode)
	}
	row, err := srv.DB.InputBinding.Get(srv.Ctx, 1)
	if err != nil {
		t.Fatalf("get after toggle: %v", err)
	}
	if row.Enabled {
		t.Fatalf("toggle should disable the binding")
	}

	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/input-bindings/1", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", delResp.StatusCode)
	}
	delAgain, _ := http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/input-bindings/1", nil)
	delAgainResp, err := http.DefaultClient.Do(delAgain)
	if err != nil {
		t.Fatalf("delete again: %v", err)
	}
	if delAgainResp.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", delAgainResp.StatusCode)
	}
}

func TestAPIInputBindingValidation(t *testing.T) {
	_, ts := newInputAdminTestServer(t)

	cases := []struct {
		name string
		body string
	}{
		{"unknown source", `{"source":"mystery","event":"tap","action":"{\"kind\":\"notification\",\"message\":\"x\"}"}`},
		{"unknown event", `{"source":"nfc","event":"explode","action":"{\"kind\":\"notification\",\"message\":\"x\"}"}`},
		{"invalid action", `{"source":"nfc","event":"tap","action":"{\"kind\":\"nope\"}"}`},
		{"missing target", `{"source":"nfc","event":"tap","action":"{\"kind\":\"scene\",\"scene_id\":99999}"}`},
	}
	for _, tc := range cases {
		resp, _ := postInputBindingJSON(t, ts.URL+"/admin/api/input-bindings", tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", tc.name, resp.StatusCode)
		}
	}

	listResp, _ := http.Get(ts.URL + "/admin/api/input-bindings")
	var list []map[string]any
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	if len(list) != 0 {
		t.Fatalf("rejected payloads must not be stored, got %d rows", len(list))
	}
}
