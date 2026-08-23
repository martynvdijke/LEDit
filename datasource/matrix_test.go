package datasource

import (
	"image/png"
	"strings"
	"testing"
	"time"

	"ledit/render"
)

// stubSource counts GetPNG invocations so tests can assert cache behaviour.
type stubSource struct {
	name  string
	calls int
}

func (s *stubSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	s.calls++
	return render.RenderPanel(map[string]string{"k": "v"}, width, height, render.Theme{
		Name:      "stub",
		Title:     s.name,
		FontSize:  16,
		TextColor: [3]uint8{200, 200, 200},
	}, "../../fonts/PixelifySans.ttf")
}

func stubResolver(src *stubSource) func(string, int) (Datasource, string, error) {
	return func(sourceType string, sourceID int) (Datasource, string, error) {
		if sourceType == "missing" {
			return nil, "", nil
		}
		return src, src.name, nil
	}
}

func mustDecodePNG(t *testing.T, img *render.RenderedImage) {
	t.Helper()
	if img == nil || img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatalf("expected PNG image, got %+v", img)
	}
	if _, err := png.Decode(strings.NewReader(string(img.Data))); err != nil {
		t.Fatalf("invalid PNG data: %v", err)
	}
}

func TestMatrixGetPNG(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	m := &MatrixDS{
		Name:       "grid",
		Rows:       2,
		Cols:       2,
		Gap:        2,
		Background: "#282a36",
		Bindings: []PanelBinding{
			{Row: 0, Col: 0, SourceType: "weather", SourceID: 1},
		},
		Resolve: stubResolver(src),
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG error: %v", err)
	}
	mustDecodePNG(t, img)
	if src.calls != 1 {
		t.Fatalf("expected 1 source render, got %d", src.calls)
	}
}

func TestMatrixCacheHit(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()
	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "weather", SourceID: 1}},
		Resolve:  stubResolver(src),
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if src.calls != 1 {
		t.Fatalf("cache hit expected: source rendered %d times", src.calls)
	}
}

func TestMatrixCacheKeyedBySize(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()
	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "weather", SourceID: 1}},
		Resolve:  stubResolver(src),
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetPNG(32, 32); err != nil {
		t.Fatal(err)
	}
	if src.calls != 2 {
		t.Fatalf("different size should re-fetch: %d calls", src.calls)
	}
}

func TestMatrixCacheTTLExpiry(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()
	old := panelCacheTTL
	panelCacheTTL = 50 * time.Millisecond
	defer func() { panelCacheTTL = old }()

	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "weather", SourceID: 1}},
		Resolve:  stubResolver(src),
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if src.calls != 1 {
		t.Fatalf("within TTL expected 1 call, got %d", src.calls)
	}
	time.Sleep(70 * time.Millisecond)
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if src.calls != 2 {
		t.Fatalf("after TTL expiry expected refetch, got %d calls", src.calls)
	}
}

func TestMatrixClearCache(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()
	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "weather", SourceID: 1}},
		Resolve:  stubResolver(src),
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	clearPanelCache()
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if src.calls != 2 {
		t.Fatalf("clearPanelCache should force refetch, got %d calls", src.calls)
	}
}

func TestMatrixUnboundCell(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()
	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{
			{Row: 0, Col: 0, SourceType: "weather", SourceID: 1},
			{Row: 1, Col: 1, SourceType: "missing", SourceID: 99}, // unresolved
		},
		Resolve: stubResolver(src),
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("unresolved binding must not fail: %v", err)
	}
	mustDecodePNG(t, img)
}

func TestMatrixOutOfRangeBindingSkipped(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()
	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{
			{Row: 5, Col: 5, SourceType: "weather", SourceID: 1}, // outside 2x2
		},
		Resolve: stubResolver(src),
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("out-of-range binding must be skipped: %v", err)
	}
	mustDecodePNG(t, img)
	if src.calls != 0 {
		t.Fatalf("out-of-range binding rendered source %d times", src.calls)
	}
}

func TestValidBindings(t *testing.T) {
	valid := `[{"row":0,"col":1,"source_type":"weather","source_id":2}]`
	if !ValidBindings(valid, 2, 2) {
		t.Fatal("expected valid bindings to pass")
	}
	outOfRange := `[{"row":3,"col":0,"source_type":"weather","source_id":2}]`
	if ValidBindings(outOfRange, 2, 2) {
		t.Fatal("expected out-of-range binding to fail")
	}
	malformed := `not json`
	if ValidBindings(malformed, 2, 2) {
		t.Fatal("expected malformed bindings to fail")
	}
}

func TestParseBindings(t *testing.T) {
	if got := ParseBindings(`garbage`); len(got) != 0 {
		t.Fatalf("malformed JSON should yield empty bindings, got %v", got)
	}
	b := ParseBindings(`[{"row":0,"col":1,"source_type":"weather","source_id":7}]`)
	if len(b) != 1 || b[0].Row != 0 || b[0].Col != 1 || b[0].SourceType != "weather" || b[0].SourceID != 7 {
		t.Fatalf("unexpected parse result: %+v", b)
	}
	roundTrip := ParseBindings(BindingsJSON(b))
	if len(roundTrip) != 1 || roundTrip[0].SourceID != 7 {
		t.Fatalf("BindingsJSON round-trip failed: %+v", roundTrip)
	}
}

func TestPanelCacheHook(t *testing.T) {
	src := &stubSource{name: "WEATHER"}
	clearPanelCache()

	var calls []bool
	PanelCacheHook = func(hit bool) { calls = append(calls, hit) }
	t.Cleanup(func() { PanelCacheHook = nil })

	m := &MatrixDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "weather", SourceID: 1}},
		Resolve:  stubResolver(src),
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] {
		t.Fatalf("first render should be a cache miss, got %v", calls)
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !calls[1] {
		t.Fatalf("second render within TTL should be a cache hit, got %v", calls)
	}
}

// --- CellTheme / PanelBinding theme tests (task 3.1) ---

func TestCellThemeFullRoundTrip(t *testing.T) {
	bindings := []PanelBinding{
		{Row: 0, Col: 0, SourceType: "weather", SourceID: 1, Theme: &CellTheme{Accent: "#ff0000", Text: "00ff00", Background: "#0000ff", FontSize: 12.5}},
	}
	raw := BindingsJSON(bindings)
	parsed := ParseBindings(raw)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(parsed))
	}
	th := parsed[0].Theme
	if th == nil {
		t.Fatal("expected non-nil theme after round-trip")
	}
	if th.Accent != "#ff0000" || th.Text != "00ff00" || th.Background != "#0000ff" || th.FontSize != 12.5 {
		t.Fatalf("round-trip theme mismatch: %+v", th)
	}
	if !ValidBindings(raw, 2, 2) {
		t.Fatal("full theme valid hex should pass ValidBindings")
	}
}

func TestCellThemePartialAndNil(t *testing.T) {
	// Partial: only accent
	raw := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"accent":"#aabbcc"}}]`
	parsed := ParseBindings(raw)
	if len(parsed) != 1 || parsed[0].Theme == nil {
		t.Fatalf("expected partial theme parsed, got %+v", parsed)
	}
	if parsed[0].Theme.Accent != "#aabbcc" || parsed[0].Theme.Text != "" || parsed[0].Theme.Background != "" || parsed[0].Theme.FontSize != 0 {
		t.Fatalf("partial theme fields wrong: %+v", parsed[0].Theme)
	}
	if !ValidBindings(raw, 2, 2) {
		t.Fatal("partial theme should be valid")
	}
	// Nil theme stays nil
	raw2 := `[{"row":0,"col":0,"source_type":"weather","source_id":1}]`
	parsed2 := ParseBindings(raw2)
	if len(parsed2) != 1 || parsed2[0].Theme != nil {
		t.Fatalf("expected nil theme, got %+v", parsed2[0].Theme)
	}
	// Also via BindingsJSON nil omits key
	bNoTheme := []PanelBinding{{Row: 0, Col: 0, SourceType: "weather", SourceID: 1}}
	rawNoTheme := BindingsJSON(bNoTheme)
	if strings.Contains(rawNoTheme, "theme") {
		t.Fatalf("nil theme should be omitted from JSON, got %s", rawNoTheme)
	}
}

func TestCellThemeBadHexValidBindings(t *testing.T) {
	bad := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"accent":"zzz"}}]`
	if ValidBindings(bad, 2, 2) {
		t.Fatal("expected ValidBindings false for bad hex zzz")
	}
	// Must still parse (validation is ValidBindings' job)
	parsed := ParseBindings(bad)
	if len(parsed) != 1 || parsed[0].Theme == nil || parsed[0].Theme.Accent != "zzz" {
		t.Fatalf("bad theme should still parse, got %+v", parsed)
	}
	// Well-formed
	good := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"accent":"#aabbcc","text":"112233","background":"#ffffff"}}]`
	if !ValidBindings(good, 2, 2) {
		t.Fatal("well-formed hex should pass ValidBindings")
	}
	// Validate directly
	if (&CellTheme{Accent: "zzz"}).Validate() {
		t.Fatal("zzz should fail Validate")
	}
	if !(&CellTheme{Accent: "#aabbcc"}).Validate() {
		t.Fatal("#aabbcc should pass Validate")
	}
}

func TestCellThemeOldFormatBackwardCompatible(t *testing.T) {
	raw := `[{"row":0,"col":1,"source_type":"weather","source_id":2}]`
	if !ValidBindings(raw, 2, 2) {
		t.Fatal("old-format without theme key should be valid")
	}
	parsed := ParseBindings(raw)
	if len(parsed) != 1 || parsed[0].Theme != nil {
		t.Fatalf("old-format should yield Theme==nil, got %+v", parsed[0])
	}
}

func TestCellThemeInvalidFontSize(t *testing.T) {
	neg := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"font_size":-1}}]`
	if ValidBindings(neg, 2, 2) {
		t.Fatal("negative font_size should fail ValidBindings")
	}
	large := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"font_size":101}}]`
	if ValidBindings(large, 2, 2) {
		t.Fatal("font_size >100 should fail ValidBindings")
	}
	zero := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"font_size":0}}]`
	if !ValidBindings(zero, 2, 2) {
		t.Fatal("font_size 0 (unset) should pass ValidBindings")
	}
	valid := `[{"row":0,"col":0,"source_type":"weather","source_id":1,"theme":{"font_size":100}}]`
	if !ValidBindings(valid, 2, 2) {
		t.Fatal("font_size 100 should pass ValidBindings")
	}
	// Still parses
	for _, raw := range []string{neg, large} {
		p := ParseBindings(raw)
		if len(p) != 1 || p[0].Theme == nil {
			t.Fatalf("invalid font_size should still parse: %s got %+v", raw, p)
		}
	}
}

func TestCellThemeValidate(t *testing.T) {
	if !(&CellTheme{}).Validate() {
		t.Fatal("empty theme should validate")
	}
	if (&CellTheme{Accent: "gggggg"}).Validate() {
		t.Fatal("gggggg should fail")
	}
	if !(&CellTheme{Accent: "#AABBCC"}).Validate() {
		t.Fatal("uppercase hex should pass")
	}
	if (&CellTheme{FontSize: -0.1}).Validate() {
		t.Fatal("negative font size should fail")
	}
}
