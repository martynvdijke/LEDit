package datasource

import (
	"testing"

	"ledit/render"
)

type ambientDS struct {
	calls   int
	ambient bool
}

func (a *ambientDS) GetPNG(w, h int) (*render.RenderedImage, error) {
	a.calls++
	return render.RenderPanel(map[string]string{"k": "v"}, w, h, DefaultTheme(), "fonts/PixelifySans.ttf")
}
func (a *ambientDS) Ambient() bool { return a.ambient }

type plainDS2 struct{}

func (p *plainDS2) GetPNG(w, h int) (*render.RenderedImage, error) {
	return render.RenderPanel(nil, w, h, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func TestIsAmbient(t *testing.T) {
	if IsAmbient(nil) {
		t.Fatal("nil false")
	}
	var p Datasource = &plainDS2{}
	if IsAmbient(p) {
		t.Fatal("plain should be false")
	}
	if IsAmbient(&ambientDS{ambient: false}) {
		t.Fatal("false ambient")
	}
	if !IsAmbient(&ambientDS{ambient: true}) {
		t.Fatal("true ambient")
	}
}

func TestMatrixAmbientBypass(t *testing.T) {
	clearPanelCache()
	stub := &ambientDS{ambient: true}
	m := &MatrixDS{
		Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "amb", SourceID: 5}},
		Resolve:  func(string, int) (Datasource, string, error) { return stub, "", nil },
	}
	m.GetPNG(32, 32)
	m.GetPNG(32, 32)
	if stub.calls != 2 {
		t.Fatalf("ambient should bypass cache calls %d want 2", stub.calls)
	}
	// non-ambient should cache
	clearPanelCache()
	stub2 := &ambientDS{ambient: false}
	m2 := &MatrixDS{
		Rows: 1, Cols: 1, Gap: 0,
		Bindings: []PanelBinding{{Row: 0, Col: 0, SourceType: "amb", SourceID: 5}},
		Resolve:  func(string, int) (Datasource, string, error) { return stub2, "", nil },
	}
	m2.GetPNG(32, 32)
	m2.GetPNG(32, 32)
	if stub2.calls != 1 {
		t.Fatalf("non-ambient should cache calls %d want 1", stub2.calls)
	}
}
