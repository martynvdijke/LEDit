package datasource

import (
	"testing"

	"ledit/render"
)

type scrollStub struct {
	calls   int
	scrolls bool
}

func (s *scrollStub) GetPNG(w, h int) (*render.RenderedImage, error) {
	s.calls++
	return &render.RenderedImage{Format: "PNG", Data: []byte{1, 2, 3}, Scrolls: s.scrolls}, nil
}

func TestMatrixScrollBypass(t *testing.T) {
	clearPanelCache()
	stub := &scrollStub{scrolls: true}
	m := &MatrixDS{
		Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "stub", SourceID: 99}},
		Resolve:  func(string, int) (Datasource, string, error) { return stub, "", nil },
	}
	if _, err := m.GetPNG(32, 32); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetPNG(32, 32); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 2 {
		t.Fatalf("scrolling should bypass cache: calls %d want 2", stub.calls)
	}
}

func TestMatrixNonScrollCached(t *testing.T) {
	clearPanelCache()
	stub := &scrollStub{scrolls: false}
	m := &MatrixDS{
		Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "stub", SourceID: 100}},
		Resolve:  func(string, int) (Datasource, string, error) { return stub, "", nil },
	}
	if _, err := m.GetPNG(32, 32); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetPNG(32, 32); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("non-scrolling should cache: calls %d want 1", stub.calls)
	}
}

func TestMatrixScrollCacheHook(t *testing.T) {
	clearPanelCache()
	var hooks []bool
	PanelCacheHook = func(hit bool) { hooks = append(hooks, hit) }
	t.Cleanup(func() { PanelCacheHook = nil })
	stub := &scrollStub{scrolls: true}
	m := &MatrixDS{
		Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "stub", SourceID: 101}},
		Resolve:  func(string, int) (Datasource, string, error) { return stub, "", nil },
	}
	m.GetPNG(32, 32)
	m.GetPNG(32, 32)
	if len(hooks) != 2 || hooks[0] != false || hooks[1] != false {
		t.Fatalf("scrolling should be miss both times, got %v", hooks)
	}
}
