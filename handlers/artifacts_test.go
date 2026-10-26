package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"ledit/ent/generalsettings"
	"ledit/ent/logentry"
	"ledit/ent/playlist"
	"ledit/ent/theme"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func artifactTestServer(t *testing.T) *Server {
	t.Helper()
	return newAPITokenTestServer(t)
}

// artifactDo issues a request with an optional session cookie and form body.
func artifactDo(t *testing.T, srv *Server, method, path, form string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != "" {
		body = strings.NewReader(form)
	}
	req := httptest.NewRequest(method, path, body)
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

// artifactPostBundle uploads a bundle as a multipart file plus extra fields.
func artifactPostBundle(t *testing.T, srv *Server, path string, data []byte, fields map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("file", "bundle.zip")
	if err != nil {
		t.Fatalf("multipart file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("multipart write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func artifactErrCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, w.Body.String())
	}
	return body.Error
}

// ensureArtifactGeneralSettings creates the singleton settings row the admin
// create handlers attach artifacts to.
func ensureArtifactGeneralSettings(t *testing.T, srv *Server) {
	t.Helper()
	exists, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Exist(srv.Ctx)
	if err != nil {
		t.Fatalf("check general settings: %v", err)
	}
	if !exists {
		if _, err := srv.DB.GeneralSettings.Create().
			SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
			Save(srv.Ctx); err != nil {
			t.Fatalf("seed general settings: %v", err)
		}
	}
}

func seedArtifactTheme(t *testing.T, srv *Server, name string) int {
	t.Helper()
	row, err := srv.DB.Theme.Create().
		SetName(name).
		SetBgColor("#101010").
		SetAccentColor("#20ff20").
		SetTextColor("#ffffff").
		SetTitle("CUSTOM").
		SetFontSize(24).
		Save(srv.Ctx)
	if err != nil {
		t.Fatalf("seed theme: %v", err)
	}
	return row.ID
}

func makeBundleZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func makeManifestBundle(t *testing.T, manifest ArtifactManifest, entity map[string]any, preview []byte) []byte {
	t.Helper()
	data, err := writeArtifactBundle(&manifest, entity, preview)
	if err != nil {
		t.Fatalf("writeArtifactBundle: %v", err)
	}
	return data
}

func exportArtifactBundle(t *testing.T, srv *Server, cookie *http.Cookie, kind string, id int) []byte {
	t.Helper()
	w := artifactDo(t, srv, http.MethodGet, "/admin/api/artifacts/export/"+kind+"/"+strconv.Itoa(id), "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("export %s/%d: status %d body %s", kind, id, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

func TestArtifactExportRequiresAdminSession(t *testing.T) {
	srv := artifactTestServer(t)
	ensureArtifactGeneralSettings(t, srv)
	// A completed setup (non-default admin password) means anonymous /admin
	// requests hit the auth middleware instead of the setup wizard redirect.
	if admin, err := srv.DB.AdminSettings.Query().First(srv.Ctx); err == nil {
		hash, _ := bcrypt.GenerateFromPassword([]byte("not-the-default"), bcrypt.DefaultCost)
		_ = srv.DB.AdminSettings.UpdateOneID(admin.ID).SetPasswordHash(string(hash)).Exec(srv.Ctx)
	}
	id := seedArtifactTheme(t, srv, "shareable")

	w := artifactDo(t, srv, http.MethodGet, "/admin/api/artifacts/export/theme/"+strconv.Itoa(id), "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous export: want 401, got %d (%s)", w.Code, w.Body.String())
	}
	if code := artifactErrCode(t, w); code != errCodeUnauthorized {
		t.Fatalf("anonymous export code: want %q, got %q", errCodeUnauthorized, code)
	}

	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", []byte("PK\x03\x04"), nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous import: want 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestArtifactThemeExportRoundTrip(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	id := seedArtifactTheme(t, srv, "shareable-round-trip")

	bundle := exportArtifactBundle(t, srv, cookie, "theme", id)
	zr, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatalf("exported bundle is not a zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{artifactEntryManifest, artifactEntryPayload} {
		if !names[want] {
			t.Fatalf("exported bundle is missing %s (has %v)", want, names)
		}
	}

	// Import under a new name.
	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", bundle, map[string]string{
		"name":        "shareable-round-trip-copy",
		"on_conflict": "rename",
	}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("import: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var result ArtifactImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("import result: %v", err)
	}
	if !result.Imported || result.Kind != artifactKindTheme || result.Name != "shareable-round-trip-copy" {
		t.Fatalf("unexpected import result: %+v", result)
	}
	count, err := srv.DB.Theme.Query().Where(theme.NameEQ("shareable-round-trip-copy")).Count(srv.Ctx)
	if err != nil || count != 1 {
		t.Fatalf("imported theme not persisted (count=%d err=%v)", count, err)
	}
}

func TestArtifactBuiltInThemeRefused(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	row, err := srv.DB.Theme.Create().SetName("builtin-theme").SetBuiltIn(true).Save(srv.Ctx)
	if err != nil {
		t.Fatalf("seed built-in theme: %v", err)
	}
	w := artifactDo(t, srv, http.MethodGet, "/admin/api/artifacts/export/theme/"+strconv.Itoa(row.ID), "", cookie)
	if w.Code == http.StatusOK {
		t.Fatalf("built-in theme export should fail, got 200")
	}
	if code := artifactErrCode(t, w); code != errCodeValidationFailed {
		t.Fatalf("built-in export code: want %q, got %q", errCodeValidationFailed, code)
	}
}

func TestArtifactPixelArtStripsSecret(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	row, err := srv.DB.PixelArt.Create().
		SetName("secret-art").
		SetGridWidth(8).SetGridHeight(8).
		SetFrames(`{"frames":[]}`).
		SetBindings(`{"x":1}`).
		SetAPIURL("https://internal.example/api").
		SetAPIToken("super-secret-token").
		Save(srv.Ctx)
	if err != nil {
		t.Fatalf("seed pixel art: %v", err)
	}
	bundle := exportArtifactBundle(t, srv, cookie, artifactKindPixelArt, row.ID)
	if bytes.Contains(bundle, []byte("super-secret-token")) {
		t.Fatalf("exported bundle leaked the api token")
	}
}

// ---------------------------------------------------------------------------
// import validation
// ---------------------------------------------------------------------------

func TestArtifactImportConflictMatrix(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	seedArtifactTheme(t, srv, "duplicate-name")
	source := seedArtifactTheme(t, srv, "conflict-source")
	bundle := exportArtifactBundle(t, srv, cookie, "theme", source)

	// Manifest carries the source name; rewrite it to the duplicate by hand.
	var payload ArtifactPayload
	zr, _ := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	for _, f := range zr.File {
		if f.Name == artifactEntryPayload {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			_ = json.Unmarshal(raw, &payload)
		}
	}

	dup := makeManifestBundle(t, ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "duplicate-name", Version: "1.0.0",
	}, payload.Entity, nil)

	// default → 409 name_conflict
	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", dup, nil, cookie)
	if w.Code != http.StatusConflict || artifactErrCode(t, w) != errCodeNameConflict {
		t.Fatalf("default conflict: want 409 name_conflict, got %d %s", w.Code, w.Body.String())
	}

	// skip → 200, skipped
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", dup, map[string]string{"on_conflict": "skip"}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("skip: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var skipped ArtifactImportResult
	_ = json.Unmarshal(w.Body.Bytes(), &skipped)
	if !skipped.Skipped || skipped.Imported {
		t.Fatalf("skip result should report skipped only: %+v", skipped)
	}

	// replace → 200, replaced, same id preserved
	before, _ := srv.DB.Theme.Query().Where(theme.NameEQ("duplicate-name")).Only(srv.Ctx)
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", dup, map[string]string{"on_conflict": "replace"}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("replace: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var replaced ArtifactImportResult
	_ = json.Unmarshal(w.Body.Bytes(), &replaced)
	if !replaced.Replaced || replaced.ID != before.ID {
		t.Fatalf("replace should preserve the id: %+v (was %d)", replaced, before.ID)
	}

	// rename → 200, new row
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", dup, map[string]string{
		"on_conflict": "rename", "name": "duplicate-name-renamed",
	}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	count, _ := srv.DB.Theme.Query().Where(theme.NameEQ("duplicate-name-renamed")).Count(srv.Ctx)
	if count != 1 {
		t.Fatalf("renamed theme not created (count=%d)", count)
	}
}

func TestArtifactImportChecksumMismatch(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	bundle := makeManifestBundle(t, ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "checksum-theme", Version: "1.0.0",
	}, map[string]any{"name": "checksum-theme", "bg_color": "#000000"}, nil)

	// Caller-supplied whole-file digest that does not match.
	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", bundle, map[string]string{
		"sha256": strings.Repeat("a", 64),
	}, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeChecksumMismatch {
		t.Fatalf("whole-file digest: want checksum_mismatch, got %d %s", w.Code, w.Body.String())
	}

	// Manifest payload_sha256 that does not match the payload bytes.
	payload, _ := json.Marshal(map[string]any{"kind": artifactKindTheme, "entity": map[string]any{"name": "checksum-theme"}})
	manifest := ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "checksum-theme",
		Version: "1.0.0", PayloadSHA256: strings.Repeat("b", 64),
	}
	manifestRaw, _ := json.Marshal(manifest)
	tampered := makeBundleZip(t, map[string][]byte{
		artifactEntryManifest: manifestRaw,
		artifactEntryPayload:  payload,
	})
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", tampered, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeChecksumMismatch {
		t.Fatalf("manifest digest: want checksum_mismatch, got %d %s", w.Code, w.Body.String())
	}
}

func TestArtifactImportRejectsUnsafeAndUnknownEntries(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	payload, _ := json.Marshal(map[string]any{"kind": artifactKindTheme, "entity": map[string]any{"name": "x"}})
	manifest := ArtifactManifest{Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "x", Version: "1.0.0", PayloadSHA256: payloadSHA256(payload)}
	manifestRaw, _ := json.Marshal(manifest)

	// traversal
	traversal := makeBundleZip(t, map[string][]byte{
		"../evil.json":        []byte("{}"),
		artifactEntryManifest: manifestRaw,
		artifactEntryPayload:  payload,
	})
	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", traversal, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeInvalidPath {
		t.Fatalf("traversal: want invalid_path, got %d %s", w.Code, w.Body.String())
	}

	// unknown entry
	unknown := makeBundleZip(t, map[string][]byte{
		"extra.txt":           []byte("hi"),
		artifactEntryManifest: manifestRaw,
		artifactEntryPayload:  payload,
	})
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", unknown, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeInvalidPath {
		t.Fatalf("unknown entry: want invalid_path, got %d %s", w.Code, w.Body.String())
	}

	// not a zip
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", []byte("not a zip at all"), nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeInvalidZip {
		t.Fatalf("invalid zip: got %d %s", w.Code, w.Body.String())
	}

	// missing payload
	missingPayload := makeBundleZip(t, map[string][]byte{artifactEntryManifest: manifestRaw})
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", missingPayload, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeMissingPayload {
		t.Fatalf("missing payload: got %d %s", w.Code, w.Body.String())
	}

	// unsupported kind / newer schema
	badKind := ArtifactManifest{Schema: artifactSchemaVersion, Kind: "not-a-kind", Name: "x", Version: "1.0.0", PayloadSHA256: payloadSHA256(payload)}
	badKindRaw, _ := json.Marshal(badKind)
	unsupported := makeBundleZip(t, map[string][]byte{artifactEntryManifest: badKindRaw, artifactEntryPayload: payload})
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", unsupported, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeUnsupportedKind {
		t.Fatalf("unsupported kind: got %d %s", w.Code, w.Body.String())
	}
	newer := ArtifactManifest{Schema: artifactSchemaVersion + 1, Kind: artifactKindTheme, Name: "x", Version: "1.0.0", PayloadSHA256: payloadSHA256(payload)}
	newerRaw, _ := json.Marshal(newer)
	tooNew := makeBundleZip(t, map[string][]byte{artifactEntryManifest: newerRaw, artifactEntryPayload: payload})
	w = artifactPostBundle(t, srv, "/admin/api/artifacts/import", tooNew, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeUnsupportedVer {
		t.Fatalf("newer schema: got %d %s", w.Code, w.Body.String())
	}
}

func TestArtifactImportRejectsExecutableFields(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	bundle := makeManifestBundle(t, ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "exec-theme", Version: "1.0.0",
	}, map[string]any{"name": "exec-theme", "script": "rm -rf /"}, nil)
	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", bundle, nil, cookie)
	if w.Code != http.StatusBadRequest || artifactErrCode(t, w) != errCodeValidationFailed {
		t.Fatalf("executable field: want validation_failed, got %d %s", w.Code, w.Body.String())
	}
	count, _ := srv.DB.Theme.Query().Where(theme.NameEQ("exec-theme")).Count(srv.Ctx)
	if count != 0 {
		t.Fatalf("a rejected bundle must not create rows")
	}
}

func TestArtifactImportDryRunWritesNothing(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	bundle := makeManifestBundle(t, ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "dry-run-theme", Version: "1.0.0",
	}, map[string]any{"name": "dry-run-theme", "bg_color": "#010203"}, nil)

	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", bundle, map[string]string{"dry_run": "1"}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("dry run: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var result ArtifactImportResult
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if result.Imported {
		t.Fatalf("dry run must not import: %+v", result)
	}
	count, _ := srv.DB.Theme.Query().Where(theme.NameEQ("dry-run-theme")).Count(srv.Ctx)
	if count != 0 {
		t.Fatalf("dry run created a row (count=%d)", count)
	}
}

func TestArtifactImportPlaylistAttachesEdge(t *testing.T) {
	srv := artifactTestServer(t)
	ensureArtifactGeneralSettings(t, srv)
	cookie := loginAsAdmin(t, srv)
	bundle := makeManifestBundle(t, ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindPlaylist, Name: "shared-playlist", Version: "1.0.0",
	}, map[string]any{
		"name":    "shared-playlist",
		"enabled": true,
		"items":   `[{"source_type":"clock","source_id":0}]`,
	}, nil)

	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", bundle, nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("playlist import: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	row, err := srv.DB.Playlist.Query().Where(playlist.NameEQ("shared-playlist")).Only(srv.Ctx)
	if err != nil {
		t.Fatalf("playlist not created: %v", err)
	}
	gs, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithPlaylists().Only(srv.Ctx)
	if err != nil {
		t.Fatalf("load general settings: %v", err)
	}
	found := false
	for _, p := range gs.Edges.Playlists {
		if p.ID == row.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("imported playlist was not attached to GeneralSettings")
	}
}

func TestArtifactImportRecordsAuditEntry(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	bundle := makeManifestBundle(t, ArtifactManifest{
		Schema: artifactSchemaVersion, Kind: artifactKindTheme, Name: "audited-theme", Version: "1.0.0",
	}, map[string]any{"name": "audited-theme"}, nil)
	w := artifactPostBundle(t, srv, "/admin/api/artifacts/import", bundle, nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("audited import: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		count, err := srv.DB.LogEntry.Query().
			Where(logentry.SourceEQ("artifacts")).
			Count(srv.Ctx)
		if err == nil && count > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no artifacts audit entry was recorded")
}

// ---------------------------------------------------------------------------
// catalog + remote fetch
// ---------------------------------------------------------------------------

func TestArtifactCatalogGuidanceWhenUnconfigured(t *testing.T) {
	t.Setenv("ARTIFACT_CATALOG_URL", "")
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	w := artifactDo(t, srv, http.MethodGet, "/admin/artifacts/catalog", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("catalog page: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ARTIFACT_CATALOG_URL") {
		t.Fatalf("catalog page should render setup guidance")
	}
}

func TestArtifactPagePrefillsDeepLink(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	target := "/admin/artifacts?url=" + url.QueryEscape("https://example.com/a.zip") + "&sha256=abc123&name=Renamed&on_conflict=rename"
	w := artifactDo(t, srv, http.MethodGet, target, "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("artifacts page: want 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"https://example.com/a.zip", "abc123", "Renamed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prefill %q missing from the page", want)
		}
	}
}

func TestFetchArtifactURLPolicy(t *testing.T) {
	ctx := t.Context()

	if _, aerr := fetchArtifactURL(ctx, "file:///etc/passwd", 1024); aerr == nil || aerr.Code != errCodeInvalidURL {
		t.Fatalf("file scheme: want invalid_url, got %v", aerr)
	}

	t.Setenv("ARTIFACT_CATALOG_HOSTS", "")
	if _, aerr := fetchArtifactURL(ctx, "http://127.0.0.1:9/bundle.zip", 1024); aerr == nil || aerr.Code != errCodeHostNotAllowed {
		t.Fatalf("loopback without allowlist: want host_not_allowed, got %v", aerr)
	}

	// Redirects must not be followed: the target handler must never run.
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	host := strings.TrimPrefix(redirector.URL, "http://")
	host = host[:strings.LastIndex(host, ":")]
	t.Setenv("ARTIFACT_CATALOG_HOSTS", host)
	if _, aerr := fetchArtifactURL(ctx, redirector.URL+"/bundle.zip", 1024); aerr == nil || aerr.Code != errCodeFetchFailed {
		t.Fatalf("redirect: want fetch_failed, got %v", aerr)
	}
	if hit {
		t.Fatalf("a redirect target must never be fetched")
	}
}

func TestArtifactCatalogInstall(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)
	source := seedArtifactTheme(t, srv, "catalog-source")
	bundle := exportArtifactBundle(t, srv, cookie, "theme", source)
	digest := sha256.Sum256(bundle)

	mux := http.NewServeMux()
	bundleServed := false
	mux.HandleFunc("/bundle.zip", func(w http.ResponseWriter, r *http.Request) {
		bundleServed = true
		_, _ = w.Write(bundle)
	})
	catalog := httptest.NewServer(mux)
	defer catalog.Close()

	hostPort := strings.TrimPrefix(catalog.URL, "http://")
	host := hostPort[:strings.LastIndex(hostPort, ":")]
	t.Setenv("ARTIFACT_CATALOG_HOSTS", host)

	// Mismatched integrity must abort before writing.
	w := artifactDo(t, srv, http.MethodPost, "/admin/artifacts/catalog/install",
		"bundle_url="+url.QueryEscape(catalog.URL+"/bundle.zip")+"&sha256="+strings.Repeat("f", 64), cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("install redirect: want 302, got %d", w.Code)
	}
	count, _ := srv.DB.Theme.Query().Where(theme.NameEQ("catalog-source")).Count(srv.Ctx)
	if count != 1 {
		t.Fatalf("checksum mismatch must not import (count=%d)", count)
	}

	// Correct integrity installs a renamed copy.
	form := "bundle_url=" + url.QueryEscape(catalog.URL+"/bundle.zip") +
		"&sha256=" + hex.EncodeToString(digest[:]) +
		"&name=" + url.QueryEscape("catalog-installed") +
		"&on_conflict=rename"
	w = artifactDo(t, srv, http.MethodPost, "/admin/artifacts/catalog/install", form, cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("install: want 302, got %d (%s)", w.Code, w.Body.String())
	}
	if !bundleServed {
		t.Fatalf("the catalog bundle was never fetched")
	}
	count, _ = srv.DB.Theme.Query().Where(theme.NameEQ("catalog-installed")).Count(srv.Ctx)
	if count != 1 {
		t.Fatalf("catalog install did not create the renamed theme (count=%d)", count)
	}
}

func TestArtifactCatalogPageListsEntries(t *testing.T) {
	srv := artifactTestServer(t)
	cookie := loginAsAdmin(t, srv)

	index := `{"artifacts":[{"kind":"theme","name":"Neon","version":"1.0.0","author":"martyn","description":"A neon theme","bundle_url":"https://example.com/neon.zip","sha256":"deadbeef","size":123}]}`
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(index))
	}))
	defer catalog.Close()

	hostPort := strings.TrimPrefix(catalog.URL, "http://")
	host := hostPort[:strings.LastIndex(hostPort, ":")]
	t.Setenv("ARTIFACT_CATALOG_HOSTS", host)
	t.Setenv("ARTIFACT_CATALOG_URL", catalog.URL+"/index.json")

	w := artifactDo(t, srv, http.MethodGet, "/admin/artifacts/catalog", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("catalog page: want 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Neon", "martyn", "deadbeef"} {
		if !strings.Contains(body, want) {
			t.Fatalf("catalog page missing %q", want)
		}
	}
}
