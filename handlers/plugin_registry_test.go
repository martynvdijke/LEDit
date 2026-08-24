package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
)

func newPluginTestServer(t *testing.T) *Server {
	t.Helper()
	// unique DB filename per test to avoid sqlite shared memory contention
	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s.db?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", name)
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	datasource.ResetPluginHealth()
	return New(drv, nil)
}

func loginPluginTest(t *testing.T, srv *Server) *http.Cookie {
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

func apiPluginCreate(t *testing.T, srv *Server, cookie *http.Cookie, payload any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/plugins", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestPluginRegistry_Unauthenticated401(t *testing.T) {
	srv := newPluginTestServer(t)
	// Plugins API is under /admin (AuthMiddleware) so unauthenticated gets 302 redirect
	w := apiPluginCreate(t, srv, nil, map[string]any{"name": "x", "kind": "exec", "target": "/bin/true"})
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusFound {
		t.Fatalf("expected 401 or 302, got %d", w.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/api/plugins", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusFound {
		t.Fatalf("GET unauth expected 401/302 got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/admin/api/plugins/1/health", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusFound {
		t.Fatalf("health unauth expected 401/302 got %d", w.Code)
	}
}

func TestPluginRegistry_CreateExecSuccess(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	w := apiPluginCreate(t, srv, cookie, map[string]any{"name": "exec-ok", "kind": "exec", "target": "/bin/true", "enabled": true})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "exec-ok" {
		t.Fatalf("unexpected resp %v", resp)
	}
}

func TestPluginRegistry_CreateExecNonExistent400(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	w := apiPluginCreate(t, srv, cookie, map[string]any{"name": "bad-exec", "kind": "exec", "target": "/no/such/bin123"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body %s", w.Code, w.Body.String())
	}
}

func TestPluginRegistry_CreateHTTPNonLocalhost400(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	w := apiPluginCreate(t, srv, cookie, map[string]any{"name": "bad-http", "kind": "http", "target": "http://example.com/api", "enabled": true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-localhost, got %d %s", w.Code, w.Body.String())
	}
}

func TestPluginRegistry_CreateHTTPLocalhostSuccess(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	w := apiPluginCreate(t, srv, cookie, map[string]any{"name": "good-http", "kind": "http", "target": "http://127.0.0.1:8765/hook", "enabled": true})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", w.Code, w.Body.String())
	}
}

func TestPluginRegistry_DisabledPluginHealthAndAllowPrefix(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	// create disabled (enabled false)
	w := apiPluginCreate(t, srv, cookie, map[string]any{"name": "disabled-one", "kind": "exec", "target": "/bin/true", "enabled": false})
	if w.Code != http.StatusCreated {
		t.Fatalf("create disabled: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	// health should show enabled false before any invocation
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/api/plugins/%d/health", created.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d %s", rec.Code, rec.Body.String())
	}
	var h map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &h)
	if h["enabled"] != false {
		t.Fatalf("expected enabled false, got %v", h["enabled"])
	}
	// PLUGINS_ALLOW_PREFIX check: set prefix to /tmp, then /bin/true should be rejected
	t.Setenv("PLUGINS_ALLOW_PREFIX", "/tmp")
	w2 := apiPluginCreate(t, srv, cookie, map[string]any{"name": "prefix-fail", "kind": "exec", "target": "/bin/true"})
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for prefix violation, got %d %s", w2.Code, w2.Body.String())
	}
	// but a file under prefix should succeed
	tmpBin := filepath.Join(t.TempDir(), "mybin")
	_ = os.WriteFile(tmpBin, []byte("#!/bin/sh\necho '{\"v\":1,\"rows\":[]}'"), 0755)
	w3 := apiPluginCreate(t, srv, cookie, map[string]any{"name": "prefix-ok", "kind": "exec", "target": tmpBin})
	if w3.Code != http.StatusCreated {
		t.Fatalf("prefix ok expected 201 got %d %s", w3.Code, w3.Body.String())
	}
}

func TestPluginRegistry_PluginDatasourceCreateWithConfigAndFeedRender(t *testing.T) {
	srv := newPluginTestServer(t)
	_ = srv
	cfg := json.RawMessage(`{"sensor":"A"}`)

	// Exec mock script that ignores stdin and prints valid rows
	script := filepath.Join(t.TempDir(), "plugin.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '{\"v\":1,\"rows\":[{\"label\":\"Temp\",\"value\":\"22\",\"text\":\"ok\"}]}'\n"), 0755)
	plugin := datasource.PluginInfo{ID: 1, Kind: "exec", Target: script, Enabled: true, TimeoutMs: 2000}
	req := datasource.PluginRequest{Width: 64, Height: 32, Config: cfg}
	resp, _ := datasource.InvokePlugin(context.Background(), plugin, req)
	if resp == nil || len(resp.Rows) == 0 {
		t.Fatalf("expected rows, got %v", resp)
	}
	if resp.Rows[0].Label != "Temp" {
		t.Fatalf("unexpected row %v", resp.Rows)
	}

	// HTTP mock
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing content-type")
		}
		var pr datasource.PluginRequest
		_ = json.NewDecoder(r.Body).Decode(&pr)
		if string(pr.Config) != `{"sensor":"A"}` {
			t.Errorf("config not propagated: %s", string(pr.Config))
		}
		w.Write([]byte(`{"v":1,"rows":[{"label":"Hum","value":"55","text":"http ok"}]}`))
	}))
	defer httpSrv.Close()
	plugin2 := datasource.PluginInfo{ID: 2, Kind: "http", Target: httpSrv.URL, TimeoutMs: 2000}
	resp2, _ := datasource.InvokePlugin(context.Background(), plugin2, req)
	if resp2 == nil || len(resp2.Rows) == 0 || resp2.Rows[0].Label != "Hum" {
		t.Fatalf("http rows mismatch %v", resp2)
	}

	// Disabled plugin unresolvable
	disabled := datasource.PluginInfo{ID: 3, Kind: "exec", Target: script, Enabled: false}
	if disabled.Enabled {
		t.Fatal("should be disabled")
	}
	datasource.RecordPluginHealth(disabled.ID, disabled.Enabled, 0, nil, "disabled", "")
	hd := datasource.GetPluginHealth(disabled.ID)
	if hd == nil || hd.Enabled != false {
		t.Fatalf("disabled health mismatch %v", hd)
	}
}
