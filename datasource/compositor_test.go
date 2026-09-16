package datasource

import (
	"context"
	"strings"
	"testing"

	"ledit/render"
)

// compThemedStub tracks plain vs themed render calls.
type compThemedStub struct{ plain, themed int }

func (c *compThemedStub) GetPNG(w, h int) (*render.RenderedImage, error) {
	c.plain++
	return render.RenderPanel(nil, w, h, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func (c *compThemedStub) GetPNGThemed(w, h int, theme render.Theme) (*render.RenderedImage, error) {
	c.themed++
	return render.RenderPanel(map[string]string{"t": "1"}, w, h, theme, "fonts/PixelifySans.ttf")
}

// compStateStub is a StateProvider over a fixed map.
type compStateStub struct {
	state map[string]any
	err   error
}

func (c *compStateStub) GetPNG(w, h int) (*render.RenderedImage, error) {
	return render.RenderPanel(nil, w, h, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func (c *compStateStub) CurrentState(context.Context) (map[string]any, error) {
	return c.state, c.err
}

func TestValidRegions(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"empty", "[]", true},
		{"grid cell", `[{"row":0,"col":1,"source_type":"clock","source_id":0}]`, true},
		{"grid span", `[{"row":0,"col":0,"row_span":2,"col_span":2}]`, true},
		{"out of bounds row", `[{"row":2,"col":0}]`, false},
		{"span overflows", `[{"row":1,"col":0,"row_span":2}]`, false},
		{"absolute", `[{"x":4,"y":4,"w":10,"h":10}]`, true},
		{"absolute missing h", `[{"x":4,"y":4,"w":10}]`, false},
		{"absolute negative x", `[{"x":-1,"y":0,"w":10,"h":10}]`, false},
		{"negative inset", `[{"row":0,"col":0,"inset":-2}]`, false},
		{"bad theme", `[{"row":0,"col":0,"theme":{"accent":"nope"}}]`, false},
		{"good theme", `[{"row":0,"col":0,"theme":{"accent":"#ff0000"}}]`, true},
		{"malformed", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidRegions(tc.raw, 2, 2); got != tc.want {
				t.Fatalf("ValidRegions(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestCompositorParseRegionsRoundTrip(t *testing.T) {
	raw := `[{"id":"a","row":0,"col":1,"source_type":"clock","source_id":0}]`
	regions := ParseRegions(raw)
	if len(regions) != 1 || regions[0].ID != "a" || regions[0].Col != 1 {
		t.Fatalf("unexpected parse: %+v", regions)
	}
	back := ParseRegions(RegionsJSON(regions))
	if len(back) != 1 || back[0].SourceType != "clock" {
		t.Fatalf("round trip failed: %+v", back)
	}
	if got := ParseRegions("{not json"); len(got) != 0 {
		t.Fatalf("malformed should yield empty, got %+v", got)
	}
}

func TestCompositorGetPNG(t *testing.T) {
	clearPanelCache()
	a := &stubSource{name: "A"}
	b := &stubSource{name: "B"}
	m := &CompositorDS{
		Name: "grid", Rows: 2, Cols: 2, Gap: 2, Background: "#282a36",
		Regions: []Region{
			{Row: 0, Col: 0, SourceType: "a", SourceID: 1},
			{Row: 1, Col: 1, SourceType: "b", SourceID: 2},
		},
		Resolve: func(sourceType string, _ int) (Datasource, string, error) {
			if sourceType == "a" {
				return a, a.name, nil
			}
			return b, b.name, nil
		},
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG error: %v", err)
	}
	mustDecodePNG(t, img)
	if a.calls != 1 || b.calls != 1 {
		t.Fatalf("expected one render each, got a=%d b=%d", a.calls, b.calls)
	}
}

func TestCompositorRegionErrorIsolation(t *testing.T) {
	clearPanelCache()
	ok := &stubSource{name: "OK"}
	m := &CompositorDS{
		Name: "grid", Rows: 1, Cols: 2, Background: "#000000",
		Regions: []Region{
			{Row: 0, Col: 0, SourceType: "ok", SourceID: 1},
			{Row: 0, Col: 1, SourceType: "missing", SourceID: 9},
		},
		Resolve: func(sourceType string, _ int) (Datasource, string, error) {
			if sourceType == "ok" {
				return ok, ok.name, nil
			}
			return nil, "", errorString("boom")
		},
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("a failing region must not fail the frame: %v", err)
	}
	mustDecodePNG(t, img)
	if ok.calls != 1 {
		t.Fatalf("expected the healthy region to render, got %d", ok.calls)
	}
}

func TestCompositorAbsoluteRegionAndBorder(t *testing.T) {
	clearPanelCache()
	m := &CompositorDS{
		Name: "abs", Background: "#000000",
		Regions: []Region{
			{X: 10, Y: 10, W: 20, H: 20, Border: true, Theme: &CellTheme{Accent: "#ff0000"}},
		},
	}
	img, err := m.GetPNG(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	rgba := decodeRGBA(t, img)
	r, g, b, _ := rgba.At(10, 10).RGBA()
	if uint8(r>>8) != 255 || uint8(g>>8) != 0 || uint8(b>>8) != 0 {
		t.Fatalf("border pixel = %d,%d,%d, want 255,0,0", r>>8, g>>8, b>>8)
	}
	r, g, b, _ = rgba.At(0, 0).RGBA()
	if uint8(r>>8) != 0 || uint8(g>>8) != 0 || uint8(b>>8) != 0 {
		t.Fatalf("background pixel = %d,%d,%d, want 0,0,0", r>>8, g>>8, b>>8)
	}
}

func TestCompositorCachingAndAmbientBypass(t *testing.T) {
	clearPanelCache()
	cached := &stubSource{name: "CACHED"}
	m := &CompositorDS{
		Name: "c", Rows: 1, Cols: 1,
		Regions: []Region{{Row: 0, Col: 0, SourceType: "cached", SourceID: 1}},
		Resolve: func(string, int) (Datasource, string, error) { return cached, "cached", nil },
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if cached.calls != 1 {
		t.Fatalf("non-ambient child should be panel-cached, got %d calls", cached.calls)
	}

	clearPanelCache()
	amb := &ambientDS{ambient: true}
	m2 := &CompositorDS{
		Name: "a", Rows: 1, Cols: 1,
		Regions: []Region{{Row: 0, Col: 0, SourceType: "amb", SourceID: 1}},
		Resolve: func(string, int) (Datasource, string, error) { return amb, "amb", nil },
	}
	if _, err := m2.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if _, err := m2.GetPNG(64, 64); err != nil {
		t.Fatal(err)
	}
	if amb.calls != 2 {
		t.Fatalf("ambient child should bypass cache, got %d calls", amb.calls)
	}
}

func TestCompositorThemedPath(t *testing.T) {
	clearPanelCache()
	ds := &compThemedStub{}
	m := &CompositorDS{
		Name: "t", Rows: 1, Cols: 1,
		Regions: []Region{{Row: 0, Col: 0, SourceType: "themed", SourceID: 1, Theme: &CellTheme{Accent: "#00ff00"}}},
		Resolve: func(string, int) (Datasource, string, error) { return ds, "themed", nil },
	}
	if _, err := m.GetPNGThemed(64, 64, DefaultTheme()); err != nil {
		t.Fatal(err)
	}
	if ds.themed != 1 || ds.plain != 0 {
		t.Fatalf("expected themed render path, got themed=%d plain=%d", ds.themed, ds.plain)
	}
}

func TestCompositorAmbient(t *testing.T) {
	plain := &stubSource{name: "P"}
	amb := &ambientDS{ambient: true}
	m := &CompositorDS{
		Rows: 1, Cols: 2,
		Regions: []Region{
			{Row: 0, Col: 0, SourceType: "plain", SourceID: 1},
			{Row: 0, Col: 1, SourceType: "amb", SourceID: 2},
		},
		Resolve: func(sourceType string, _ int) (Datasource, string, error) {
			if sourceType == "amb" {
				return amb, "amb", nil
			}
			return plain, "plain", nil
		},
	}
	if !m.Ambient() {
		t.Fatal("composition with an ambient child should be ambient")
	}
	m.Regions = m.Regions[:1]
	if m.Ambient() {
		t.Fatal("composition without ambient children should not be ambient")
	}
}

func TestCompositorCurrentState(t *testing.T) {
	withID := &compStateStub{state: map[string]any{"temp": 21}}
	noID := &compStateStub{state: map[string]any{"temp": 30}}
	failing := &compStateStub{err: errorString("nope")}
	m := &CompositorDS{
		Rows: 1, Cols: 3,
		Regions: []Region{
			{ID: "left", Row: 0, Col: 0, SourceType: "a", SourceID: 1},
			{Row: 0, Col: 1, SourceType: "b", SourceID: 2},
			{Row: 0, Col: 2, SourceType: "c", SourceID: 3},
		},
		Resolve: func(sourceType string, _ int) (Datasource, string, error) {
			switch sourceType {
			case "a":
				return withID, "a", nil
			case "b":
				return noID, "b", nil
			}
			return failing, "c", nil
		},
	}
	st, err := m.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("aggregate should not fail: %v", err)
	}
	if st["left.temp"] != 21 {
		t.Fatalf("expected region-ID namespaced key, got %+v", st)
	}
	if st["b:2.temp"] != 30 {
		t.Fatalf("expected type:id namespaced key, got %+v", st)
	}
	if _, ok := st["c:3."]; ok {
		t.Fatalf("failing child should be skipped: %+v", st)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

func TestCompositorEmptyFrameStillEncodes(t *testing.T) {
	m := &CompositorDS{Name: "empty", Rows: 1, Cols: 1, Background: "#101010",
		Regions: []Region{{Row: 0, Col: 0}}}
	img, err := m.GetPNG(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	mustDecodePNG(t, img)
	if !strings.HasPrefix(img.Format, "PNG") {
		t.Fatalf("unexpected format %q", img.Format)
	}
}
