package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ledit/datasource"
	"ledit/ent"
)

func pluginPostForm(t *testing.T, srv *Server, cookie *http.Cookie, path string, vals url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestPluginConfigFormRoundTrip(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	manifest := `{"name":"sensor","kind":"exec","target":"/bin/true","fields":[{"key":"host","type":"text","required":true},{"key":"port","type":"number"},{"key":"verbose","type":"bool"}]}`
	vals := url.Values{}
	vals.Set("name", "sensor")
	vals.Set("kind", "exec")
	vals.Set("target", "/bin/true")
	vals.Set("timeout_ms", "3000")
	vals.Set("manifest", manifest)
	vals.Set("cfg_host", "myhost")
	vals.Set("cfg_port", "8080")
	vals.Set("cfg_verbose", "on")

	if w := pluginPostForm(t, srv, cookie, "/admin/plugins/new", vals); w.Code != http.StatusFound {
		t.Fatalf("create expected 302, got %d %s", w.Code, w.Body.String())
	}
	p, err := srv.DB.DatasourcePlugin.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("plugin not persisted: %v", err)
	}
	if p.Manifest != manifest {
		t.Fatalf("manifest mismatch: %q", p.Manifest)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil {
		t.Fatalf("config not JSON: %q", p.Config)
	}
	if cfg["host"] != "myhost" || cfg["port"].(float64) != 8080 || cfg["verbose"] != true {
		t.Fatalf("config mismatch: %v", cfg)
	}
}

func TestPluginConfigFormRejectsMissingRequired(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	vals := url.Values{}
	vals.Set("name", "sensor")
	vals.Set("kind", "exec")
	vals.Set("target", "/bin/true")
	vals.Set("manifest", `{"fields":[{"key":"host","type":"text","required":true}]}`)

	if w := pluginPostForm(t, srv, cookie, "/admin/plugins/new", vals); w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if n, _ := srv.DB.DatasourcePlugin.Query().Count(context.Background()); n != 0 {
		t.Fatalf("invalid config should not persist, count=%d", n)
	}
}

func TestPluginInvalidManifestRejected(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)
	w := apiPluginCreate(t, srv, cookie, map[string]any{
		"name": "bad", "kind": "exec", "target": "/bin/true",
		"manifest": `{"fields":[{"key":"a","type":"bogus"}]}`,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestPluginCatalogResolvesAndRenders(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)

	var gotConfig string
	pluginSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var pr datasource.PluginRequest
		_ = json.NewDecoder(r.Body).Decode(&pr)
		gotConfig = string(pr.Config)
		w.Write([]byte(`{"v":1,"rows":[{"label":"Temp","value":"22","text":"ok"}]}`))
	}))
	defer pluginSrv.Close()

	w := apiPluginCreate(t, srv, cookie, map[string]any{
		"name": "cat", "kind": "http", "target": pluginSrv.URL, "enabled": true,
		"manifest": `{"fields":[{"key":"x","type":"text"}]}`,
		"config":   map[string]any{"x": "y"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	settings := &ent.GeneralSettings{}
	idx := buildSourceIndex(settings, datasource.AIConfig{})
	src, name, err := idx.Resolve("plugin", created.ID)
	if err != nil {
		t.Fatalf("plugin should be resolvable: %v", err)
	}
	if name != "Plugin: cat" {
		t.Fatalf("name mismatch: %q", name)
	}
	img, err := src.GetPNG(64, 32)
	if err != nil || img == nil || len(img.Data) == 0 {
		t.Fatalf("render via catalog: %v", err)
	}
	if gotConfig != `{"x":"y"}` {
		t.Fatalf("config not forwarded: %s", gotConfig)
	}

	hub := &WSHub{Client: srv.DB}
	found := false
	for _, s := range hub.loadSources(settings) {
		if s.cacheKey == fmt.Sprintf("plugin:%d", created.ID) {
			found = true
			if s.Name != "Plugin: cat" {
				t.Fatalf("loadSources name mismatch: %q", s.Name)
			}
		}
	}
	if !found {
		t.Fatal("enabled plugin missing from loadSources rotation")
	}
}

func TestPluginInstallFromURL(t *testing.T) {
	srv := newPluginTestServer(t)
	cookie := loginPluginTest(t, srv)

	manifest := `{"name":"installed","kind":"http","target":"http://127.0.0.1:9/plugin","fields":[{"key":"x","type":"text","default":"d"}]}`
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(manifest))
	}))
	defer manifestSrv.Close()

	vals := url.Values{}
	vals.Set("url", manifestSrv.URL)
	if w := pluginPostForm(t, srv, cookie, "/admin/plugins/install", vals); w.Code != http.StatusFound {
		t.Fatalf("install expected 302, got %d %s", w.Code, w.Body.String())
	}
	p, err := srv.DB.DatasourcePlugin.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("installed plugin not persisted: %v", err)
	}
	if p.Name != "installed" || p.Enabled {
		t.Fatalf("installed plugin should be disabled: %+v", p)
	}
	var cfg map[string]any
	_ = json.Unmarshal([]byte(p.Config), &cfg)
	if cfg["x"] != "d" {
		t.Fatalf("default config not applied: %v", cfg)
	}
}

func TestPluginCatalogFetch(t *testing.T) {
	catalogSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"plugins":[{"name":"a","description":"d","url":"http://h/x.json"}]}`))
	}))
	defer catalogSrv.Close()
	entries, err := fetchPluginCatalog(context.Background(), catalogSrv.URL)
	if err != nil {
		t.Fatalf("fetch catalog: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "a" || entries[0].URL != "http://h/x.json" {
		t.Fatalf("entries mismatch: %+v", entries)
	}
}
