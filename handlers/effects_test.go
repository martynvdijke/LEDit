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

	"github.com/gin-gonic/gin"

	"ledit/datasource"
	"ledit/ent/effectpreset"
	"ledit/ent/generalsettings"
	"ledit/render/effects"
)

// effectPresetForm is a minimal valid preset submission.
func effectPresetForm(name, effect string) url.Values {
	return url.Values{
		"name":      {name},
		"effect":    {effect},
		"speed":     {"50"},
		"intensity": {"50"},
		"density":   {"50"},
		"direction": {"none"},
		"seed":      {"0"},
	}
}

func effectPresetCount(t *testing.T, srv *Server) int {
	t.Helper()
	n, err := srv.DB.EffectPreset.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count presets: %v", err)
	}
	return n
}

// presetIDByName returns the id of a preset row.
func presetIDByName(t *testing.T, srv *Server, name string) int {
	t.Helper()
	row, err := srv.DB.EffectPreset.Query().Where(effectpreset.NameEQ(name)).Only(context.Background())
	if err != nil {
		t.Fatalf("preset %q not found: %v", name, err)
	}
	return row.ID
}

func TestEffectPresetsSeededOnStartup(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	want := []string{"Ember", "Plasma", "Static", "Sweep"}
	for _, name := range want {
		row, err := srv.DB.EffectPreset.Query().Where(effectpreset.NameEQ(name)).Only(context.Background())
		if err != nil {
			t.Fatalf("built-in preset %q missing: %v", name, err)
		}
		if !row.BuiltIn {
			t.Fatalf("preset %q should be built-in", name)
		}
		if _, _, ok := effects.Lookup(row.Effect); !ok {
			t.Fatalf("preset %q names unknown effect %q", name, row.Effect)
		}
		if !strings.Contains(row.Params, `"v":1`) {
			t.Fatalf("preset %q params missing schema version: %s", name, row.Params)
		}
		if strings.Contains(row.Params, "palette") {
			t.Fatalf("preset %q must not persist palette colors: %s", name, row.Params)
		}
	}
	// Seeding must be idempotent.
	before := effectPresetCount(t, srv)
	seedEffectPresets(srv.DB, context.Background())
	if after := effectPresetCount(t, srv); after != before {
		t.Fatalf("re-seeding changed preset count: %d -> %d", before, after)
	}
}

func TestEffectPresetCreateAndCache(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)

	w := postRuleForm(t, srv, session, "/admin/effects/new/edit", effectPresetForm("Night Sky", "sparkle"))
	if w.Code != http.StatusFound {
		t.Fatalf("create preset: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	row, err := srv.DB.EffectPreset.Query().Where(effectpreset.NameEQ("Night Sky")).Only(context.Background())
	if err != nil {
		t.Fatalf("created preset not persisted: %v", err)
	}
	if row.BuiltIn {
		t.Fatal("user preset must not be built-in")
	}
	params, err := effects.UnmarshalParams([]byte(row.Params))
	if err != nil {
		t.Fatalf("stored params unreadable: %v", err)
	}
	if params.Density != 50 || params.Direction != "none" {
		t.Fatalf("params not persisted: %+v", params)
	}
	if !params.Palette.IsZero() {
		t.Fatalf("stored params must not carry a palette: %+v", params.Palette)
	}

	// The cache is refreshed by the handler so pickers see it immediately.
	found := false
	for _, p := range cachedEffectPresets() {
		if p.ID == row.ID && p.Effect == "sparkle" {
			found = true
		}
	}
	if !found {
		t.Fatal("created preset missing from cache")
	}

	// Edit round-trip.
	form := effectPresetForm("Night Sky", "gradient")
	form.Set("speed", "80")
	w = postRuleForm(t, srv, session, fmt.Sprintf("/admin/effects/%d/edit", row.ID), form)
	if w.Code != http.StatusFound {
		t.Fatalf("edit preset: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	updated, err := srv.DB.EffectPreset.Query().Where(effectpreset.NameEQ("Night Sky")).Only(context.Background())
	if err != nil {
		t.Fatalf("reload preset: %v", err)
	}
	if updated.Effect != "gradient" {
		t.Fatalf("effect not updated: %s", updated.Effect)
	}
	up, _ := effects.UnmarshalParams([]byte(updated.Params))
	if up.Speed != 80 {
		t.Fatalf("speed not updated: %v", up.Speed)
	}
}

func TestEffectPresetValidationRejects(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)

	cases := []struct {
		name  string
		form  url.Values
		match string
	}{
		{"unknown effect", effectPresetForm("Bad Effect", "does-not-exist"), "Unknown effect"},
		{"speed out of range", func() url.Values {
			f := effectPresetForm("Too Fast", "plasma")
			f.Set("speed", "101")
			return f
		}(), "must be between"},
		{"density out of range", func() url.Values {
			f := effectPresetForm("Too Dense", "plasma")
			f.Set("density", "500")
			return f
		}(), "must be between"},
		{"bad direction", func() url.Values {
			f := effectPresetForm("Diagonal", "scan")
			f.Set("direction", "diagonal")
			return f
		}(), "must be one of"},
		{"empty name", func() url.Values {
			f := effectPresetForm("", "plasma")
			return f
		}(), "name is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := effectPresetCount(t, srv)
			w := postRuleForm(t, srv, session, "/admin/effects/new/edit", tc.form)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 re-render, got %d", w.Code)
			}
			if loc := w.Header().Get("Location"); loc != "" {
				t.Fatalf("rejected submission must not redirect, got %q", loc)
			}
			if !strings.Contains(w.Body.String(), tc.match) {
				t.Fatalf("expected message %q in body, got %s", tc.match, w.Body.String())
			}
			if after := effectPresetCount(t, srv); after != before {
				t.Fatalf("rejected submission persisted a row: %d -> %d", before, after)
			}
		})
	}

	// Duplicate name is rejected.
	first := effectPresetForm("Duplicate Me", "plasma")
	if w := postRuleForm(t, srv, session, "/admin/effects/new/edit", first); w.Code != http.StatusFound {
		t.Fatalf("create first preset: %d %s", w.Code, w.Body.String())
	}
	before := effectPresetCount(t, srv)
	w := postRuleForm(t, srv, session, "/admin/effects/new/edit", effectPresetForm("Duplicate Me", "fire"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "already exists") {
		t.Fatalf("duplicate name not rejected: %d %s", w.Code, w.Body.String())
	}
	if after := effectPresetCount(t, srv); after != before {
		t.Fatalf("duplicate name persisted a row: %d -> %d", before, after)
	}
}

func TestEffectBuiltInsImmutable(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	id := presetIDByName(t, srv, "Plasma")

	form := effectPresetForm("Renamed Plasma", "fire")
	w := postRuleForm(t, srv, session, fmt.Sprintf("/admin/effects/%d/edit", id), form)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect for built-in edit, got %d", w.Code)
	}
	row, err := srv.DB.EffectPreset.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("built-in disappeared: %v", err)
	}
	if row.Name != "Plasma" || row.Effect != "plasma" {
		t.Fatalf("built-in was modified: %s/%s", row.Name, row.Effect)
	}

	// Delete is refused too.
	w = postRuleForm(t, srv, session, fmt.Sprintf("/admin/effects/%d/delete", id), nil)
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect for built-in delete, got %d", w.Code)
	}
	if _, err := srv.DB.EffectPreset.Get(context.Background(), id); err != nil {
		t.Fatalf("built-in was deleted: %v", err)
	}

	// Duplication into an editable copy is allowed and preserves parameters.
	w = postRuleForm(t, srv, session, fmt.Sprintf("/admin/effects/%d/duplicate", id), nil)
	if w.Code != http.StatusFound {
		t.Fatalf("duplicate built-in: got %d", w.Code)
	}
	copyRow, err := srv.DB.EffectPreset.Query().Where(effectpreset.NameEQ("Plasma copy")).Only(context.Background())
	if err != nil {
		t.Fatalf("duplicate not created: %v", err)
	}
	if copyRow.BuiltIn || copyRow.Effect != "plasma" || copyRow.Params == "" {
		t.Fatalf("duplicate incorrect: %+v", copyRow)
	}
}

func TestEffectPresetSourcesRegistered(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	if w := postRuleForm(t, srv, session, "/admin/effects/new/edit", effectPresetForm("Aurora", "gradient")); w.Code != http.StatusFound {
		t.Fatalf("create preset: %d %s", w.Code, w.Body.String())
	}
	id := presetIDByName(t, srv, "Aurora")

	// bindingOptions exposes it under the effect group.
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	gc.Request = httptest.NewRequest(http.MethodGet, "/admin/effects", nil)
	opts := srv.bindingOptions(gc)
	foundOpt := false
	for _, o := range opts["effect"] {
		if o.ID == id && o.Label == "Aurora" {
			foundOpt = true
		}
	}
	if !foundOpt {
		t.Fatalf("effect preset missing from bindingOptions: %+v", opts["effect"])
	}

	// buildSourceIndex resolves it and renders a frame.
	settings, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(context.Background())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	idx := buildSourceIndex(settings, srv.aiConfig(context.Background()))
	src, name, err := idx.Resolve("effect", id)
	if err != nil {
		t.Fatalf("effect source not resolvable: %v", err)
	}
	if name != "Aurora" {
		t.Fatalf("unexpected label %q", name)
	}
	img, err := src.GetPNG(64, 32)
	if err != nil || img == nil || len(img.Data) == 0 {
		t.Fatalf("effect render failed: %v", err)
	}

	// loadSources includes it for the websocket feed.
	feedSrcs := srv.WSHub.loadSources(settings)
	found := false
	for _, s := range feedSrcs {
		if s.cacheKey == fmt.Sprintf("effect:%d", id) {
			found = true
		}
	}
	if !found {
		t.Fatal("effect preset missing from feed source list")
	}
}

func TestEffectPresetResolvesInMatrixAndScene(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	if w := postRuleForm(t, srv, session, "/admin/effects/new/edit", effectPresetForm("Comet", "sparkle")); w.Code != http.StatusFound {
		t.Fatalf("create preset: %d %s", w.Code, w.Body.String())
	}
	id := presetIDByName(t, srv, "Comet")

	// A matrix cell binding can reference the effect preset.
	ml, err := srv.DB.MatrixLayout.Create().
		SetName("Effect Grid").SetRows(1).SetCols(1).
		SetBindings(fmt.Sprintf(`{"0,0":{"source_type":"effect","source_id":%d}}`, id)).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create matrix layout: %v", err)
	}
	settings, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithMatrixLayouts().Only(context.Background())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	mds := srv.WSHub.buildMatrixDS(settings, ml, 0)
	if mds == nil {
		t.Fatal("matrix with effect binding did not build")
	}
	img, err := mds.GetPNG(32, 32)
	if err != nil || img == nil || len(img.Data) == 0 {
		t.Fatalf("matrix effect render failed: %v", err)
	}

	// A scene action can reference the effect preset too.
	scene := &Scene{
		Name:    "Effect Scene",
		Enabled: true,
		Actions: SceneActions{SourceType: "effect", SourceID: id},
	}
	sw, ok := resolveSceneSource(srv.DB, scene)
	if !ok || sw == nil {
		t.Fatal("scene could not resolve the effect preset")
	}
	if sw.cacheKey != fmt.Sprintf("effect:%d", id) {
		t.Fatalf("unexpected scene cache key %q", sw.cacheKey)
	}
	if _, err := sw.Source.GetPNG(16, 16); err != nil {
		t.Fatalf("scene effect render failed: %v", err)
	}
}

func TestDeletedEffectPresetDegrades(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	if w := postRuleForm(t, srv, session, "/admin/effects/new/edit", effectPresetForm("Temporary", "noise")); w.Code != http.StatusFound {
		t.Fatalf("create preset: %d %s", w.Code, w.Body.String())
	}
	id := presetIDByName(t, srv, "Temporary")
	if w := postRuleForm(t, srv, session, fmt.Sprintf("/admin/effects/%d/delete", id), nil); w.Code != http.StatusFound {
		t.Fatalf("delete preset: %d", w.Code)
	}
	if n := effectPresetCount(t, srv); n == 0 {
		t.Fatal("delete wiped every preset")
	}

	settings, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(context.Background())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	idx := buildSourceIndex(settings, srv.aiConfig(context.Background()))
	if _, _, err := idx.Resolve("effect", id); err == nil {
		t.Fatal("deleted preset still resolves")
	}
	for _, s := range srv.WSHub.loadSources(settings) {
		if s.cacheKey == fmt.Sprintf("effect:%d", id) {
			t.Fatal("deleted preset still in feed source list")
		}
	}
}

func TestAdminEffectPagesRequireAuth(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	for _, path := range []string{"/admin/effects", "/admin/effects/new", "/admin/effects/1/edit"} {
		w := getRulePage(t, srv, nil, path)
		if w.Code == http.StatusOK {
			t.Fatalf("anonymous GET %s returned 200", path)
		}
	}
	w := doRequest(t, srv, http.MethodPost, "/admin/preview/effect", "effect=plasma")
	if w.Code == http.StatusOK {
		t.Fatal("anonymous effect preview returned 200")
	}
}

func TestAdminPreviewEffectRendersUnsavedValuesWithoutPersisting(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	before := effectPresetCount(t, srv)

	form := url.Values{
		"effect":    {"plasma"},
		"speed":     {"100"},
		"intensity": {"80"},
		"density":   {"20"},
		"direction": {"none"},
		"seed":      {"3"},
		"w":         {"64"},
		"h":         {"32"},
	}
	w := postRuleForm(t, srv, session, "/admin/preview/effect", form)
	if w.Code != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("preview content-type = %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Fatal("preview returned an empty body")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("preview must be no-store, got %q", w.Header().Get("Cache-Control"))
	}
	if after := effectPresetCount(t, srv); after != before {
		t.Fatalf("preview persisted a preset: %d -> %d", before, after)
	}

	// Unknown effect is rejected, not rendered.
	bad := url.Values{"effect": {"nope"}}
	if w := postRuleForm(t, srv, session, "/admin/preview/effect", bad); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown effect preview expected 400, got %d", w.Code)
	}
}

// TestEffectPresetCompositionAndBackground covers the "background/base layer"
// pattern from the change: an effect used as a full-canvas source and as a
// full-span composition region, with no compositor geometry change.
func TestEffectPresetCompositionAndBackground(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	if w := postRuleForm(t, srv, session, "/admin/effects/new/edit", effectPresetForm("Backdrop", "plasma")); w.Code != http.StatusFound {
		t.Fatalf("create preset: %d %s", w.Code, w.Body.String())
	}
	id := presetIDByName(t, srv, "Backdrop")

	// Full-canvas background: the effect source itself renders at wall size.
	gs, err := srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithCompositions().Only(context.Background())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	idx := buildSourceIndex(gs, srv.aiConfig(context.Background()))
	bgSrc, _, err := idx.Resolve("effect", id)
	if err != nil {
		t.Fatalf("background effect unresolvable: %v", err)
	}
	bg, err := bgSrc.GetPNG(64, 32)
	if err != nil || bg == nil {
		t.Fatalf("background render failed: %v", err)
	}
	if w, h := decodedSize(t, bg.Data); w != 64 || h != 32 {
		t.Fatalf("background render size = %dx%d", w, h)
	}

	// First full-span composition region: the effect fills a cell on the canvas.
	comp, err := srv.DB.Composition.Create().
		SetName("Effect Backdrop").SetMode("grid").SetRows(1).SetCols(1).
		SetRegions(datasource.RegionsJSON([]datasource.Region{
			{ID: "base", Row: 0, Col: 0, SourceType: "effect", SourceID: id},
		})).
		SetEnabled(true).Save(context.Background())
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := srv.DB.GeneralSettings.UpdateOneID(1).AddCompositions(comp).Save(context.Background()); err != nil {
		t.Fatalf("attach composition: %v", err)
	}
	gs, err = srv.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).WithCompositions().Only(context.Background())
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	idx = buildSourceIndex(gs, srv.aiConfig(context.Background()))
	key := fmt.Sprintf("composition:%d", comp.ID)
	if idx.byKey[key] == nil {
		t.Fatalf("composition with effect region missing from source index")
	}
	compSrc, _, err := idx.Resolve("composition", comp.ID)
	if err != nil {
		t.Fatalf("composition unresolvable: %v", err)
	}
	img, err := compSrc.GetPNG(64, 32)
	if err != nil || img == nil {
		t.Fatalf("composition render failed: %v", err)
	}
	if w, h := decodedSize(t, img.Data); w != 64 || h != 32 {
		t.Fatalf("composition render size = %dx%d (geometry changed)", w, h)
	}
	found := false
	for _, s := range srv.WSHub.loadSources(gs) {
		if s.cacheKey == key {
			found = true
		}
	}
	if !found {
		t.Fatal("composition with effect region missing from feed sources")
	}
}

// TestAdminPreviewEffectDoesNotMutateFeed checks the preview endpoint family
// stays read-only: no feed pin/state change and no display analytics from a
// preview request.
func TestAdminPreviewEffectDoesNotMutateFeed(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)

	before := GlobalFeed.Status()
	form := url.Values{
		"effect": {"sparkle"}, "speed": {"70"}, "intensity": {"60"},
		"density": {"30"}, "direction": {"none"}, "seed": {"9"},
		"w": {"32"}, "h": {"16"},
	}
	if w := postRuleForm(t, srv, session, "/admin/preview/effect", form); w.Code != http.StatusOK {
		t.Fatalf("preview: %d", w.Code)
	}
	after := GlobalFeed.Status()
	if len(before) != len(after) {
		t.Fatalf("feed status changed by preview: %v -> %v", before, after)
	}
	for k, v := range before {
		if fmt.Sprint(after[k]) != fmt.Sprint(v) {
			t.Fatalf("feed status field %q changed by preview: %v -> %v", k, v, after[k])
		}
	}
	GlobalFeed.Resume()
}

// decodedSize decodes a PNG and returns its pixel dimensions.
func decodedSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}
