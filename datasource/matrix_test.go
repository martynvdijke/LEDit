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
