package handlers

import (
	"archive/zip"
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

func newBackupTestServer(t *testing.T) *Server {
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
func loginBackup(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	w := doRequest(t, srv, http.MethodPost, "/login", "username=admin&password=ledit")
	if w.Code != http.StatusFound {
		t.Fatalf("login failed %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("no cookie")
	return nil
}
func doBackupRequest(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost && body != "" && !strings.HasPrefix(body, "{") {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestExportExcludesSecretsByDefault(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("secret123").SetModel("gpt4").SaveX(srv.Ctx)
	srv.DB.PixelArt.Create().SetName("art1").SetFrames("{}").SetAPIToken("tok123").SaveX(srv.Ctx)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/backup/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("export %d %s", w.Code, w.Body.String())
	}
	var b Bundle
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("unmarshal %v", err)
	}
	for _, arr := range b.Entities {
		for _, m := range arr {
			if _, ok := m["api_key"]; ok {
				t.Fatalf("api_key leaked %v", m)
			}
			if _, ok := m["api_token"]; ok {
				t.Fatalf("api_token leaked")
			}
		}
	}
	// with secrets includes
	req2 := httptest.NewRequest(http.MethodGet, "/admin/api/backup/export?include_secrets=true", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Header().Get("Content-Disposition"), "WITH-SECRETS") {
		t.Fatalf("filename missing WITH-SECRETS %s", w2.Header().Get("Content-Disposition"))
	}
	var b2 Bundle
	_ = json.Unmarshal(w2.Body.Bytes(), &b2)
	found := false
	for _, arr := range b2.Entities {
		for _, m := range arr {
			if v, ok := m["api_key"]; ok && v == "secret123" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("secret not included with flag")
	}
}

func TestExportIncludesScheduleWindows(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	srv.DB.Playlist.Create().SetName("pl1").SetItems("[]").SetScheduleWindows(`[{"days":[1,2],"start":"07:00","end":"09:00"}]`).SaveX(srv.Ctx)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/backup/export", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var b Bundle
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	arr := b.Entities["playlists"]
	if len(arr) == 0 {
		t.Fatalf("no playlists")
	}
	if arr[0]["schedule_windows"] == nil {
		t.Fatalf("schedule_windows missing %v", arr[0])
	}
}

func TestMediaZipManifest(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/backup/export?include_media=true", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("expected zip got %s", w.Header().Get("Content-Type"))
	}
	zr, _ := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	found := false
	for _, f := range zr.File {
		if f.Name == "bundle.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bundle.json missing in zip")
	}
}

func TestForwardMajorRejected(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	b := Bundle{Version: "2.0", Entities: map[string][]map[string]any{"playlists": {{"id": 1, "name": "x"}}}}
	data, _ := json.Marshal(b)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import?dry_run=true", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported_version") {
		t.Fatalf("missing unsupported_version %s", w.Body.String())
	}
}

func TestDanglingFK(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	b := Bundle{Version: "1.0", Entities: map[string][]map[string]any{"device_settings": {{"id": 1, "name": "d1", "playlist_id": 999}}}}
	data, _ := json.Marshal(b)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import?dry_run=true", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 dangling %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "dangling") {
		t.Fatalf("no dangling %s", w.Body.String())
	}
}

func TestDryRunCountsAndNoMutate(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	p := srv.DB.Playlist.Create().SetName("orig").SetItems("[]").SetScheduleWindows("[]").SaveX(srv.Ctx)
	b := Bundle{Version: "1.0", Entities: map[string][]map[string]any{"playlists": {{"id": float64(p.ID), "name": "changed", "items": "[]", "schedule_windows": "[]"}}}}
	data, _ := json.Marshal(b)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import?dry_run=true", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("dryrun %d %s", w.Code, w.Body.String())
	}
	// DB unchanged
	ex, _ := srv.DB.Playlist.Get(srv.Ctx, p.ID)
	if ex.Name != "orig" {
		t.Fatalf("db mutated")
	}
}

func TestZipBombRejected(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for i := 0; i < 1001; i++ {
		fw, _ := zw.Create(fmt.Sprintf("media/file%d.txt", i))
		fw.Write([]byte("x"))
	}
	zw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/zip")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 zip bomb got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "bundle_too_large") {
		t.Fatalf("no bomb error %s", w.Body.String())
	}
}

func TestPathTraversalRejected(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	b, _ := json.Marshal(Bundle{Version: "1.0", Entities: map[string][]map[string]any{}})
	fw, _ := zw.Create("bundle.json")
	fw.Write(b)
	fw2, _ := zw.Create("../../etc/passwd")
	fw2.Write([]byte("evil"))
	zw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/zip")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 traversal got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_path") {
		t.Fatalf("no traversal error %s", w.Body.String())
	}
}

func TestImportCreatesRows(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	b := Bundle{Version: "1.0", Entities: map[string][]map[string]any{"playlists": {{"name": "newpl", "items": "[]", "schedule_windows": "[]"}}}}
	data, _ := json.Marshal(b)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("import %d %s", w.Code, w.Body.String())
	}
	count, _ := srv.DB.Playlist.Query().Count(srv.Ctx)
	if count == 0 {
		t.Fatalf("no playlist created")
	}
}

func TestPartialFailureLeavesEarlierCommitted(t *testing.T) {
	srv := newBackupTestServer(t)
	b := Bundle{Version: "1.0", Entities: map[string][]map[string]any{
		"generalsettings": {{"timeout": 60.0, "random": false}},
		"playlists":       {{"name": "fail", "__fail": true}},
	}}
	res := srv.ImportBundle(b, false)
	if res.FailedType != "playlists" {
		t.Fatalf("expected playlists failed got %v", res)
	}
	if len(res.CompletedTypes) == 0 || res.CompletedTypes[0] != "generalsettings" {
		t.Fatalf("expected generalsettings completed %v", res.CompletedTypes)
	}
}

func TestSecretsAbsentLeavesDBSecretsIntact(t *testing.T) {
	srv := newBackupTestServer(t)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("keepme").SetModel("gpt4").SaveX(srv.Ctx)
	b := Bundle{Version: "1.0", Entities: map[string][]map[string]any{"aisettings": {{"provider": "openai", "model": "gpt4"}}}}
	// import without secrets
	srv.ImportBundle(b, false)
	list, _ := srv.DB.AISettings.Query().All(srv.Ctx)
	if list[0].APIKey != "keepme" {
		t.Fatalf("secret overwritten %s", list[0].APIKey)
	}
}

func TestFKOrderRespected(t *testing.T) {
	// ensure playlists before device_settings in importOrder
	idx := func(typ string) int {
		for i, v := range importOrder {
			if v == typ {
				return i
			}
		}
		return 999
	}
	if idx("playlists") > idx("device_settings") {
		t.Fatalf("FK order violated")
	}
}

func TestImportZipWithMedia(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)
	b := Bundle{Version: "1.0", Entities: map[string][]map[string]any{"playlists": {{"name": "zpl", "items": "[]", "schedule_windows": "[]"}}}}
	bdata, _ := json.Marshal(b)
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, _ := zw.Create("bundle.json")
	fw.Write(bdata)
	zw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/backup/import", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/zip")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("zip import %d %s", w.Code, w.Body.String())
	}
}
