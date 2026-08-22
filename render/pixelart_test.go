package render

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"testing"
)

func validDoc() PixelFrameDoc {
	return PixelFrameDoc{
		Palette: []string{"#000000", "#ff0000", "#00ff00"},
		Frames: []PixelFrame{
			{Duration: 500, Pixels: []int{0, 1, 1, 0}},
			{Duration: 250, Pixels: []int{2, 2, 2, 2}},
		},
	}
}

func TestValidatePixelDoc(t *testing.T) {
	if err := ValidatePixelDoc(validDoc(), 2, 2); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}

	cases := map[string]func(*PixelFrameDoc){
		"no frames":       func(d *PixelFrameDoc) { d.Frames = nil },
		"zero duration":   func(d *PixelFrameDoc) { d.Frames[0].Duration = 0 },
		"pixel count":     func(d *PixelFrameDoc) { d.Frames[0].Pixels = []int{0, 1} },
		"index too large": func(d *PixelFrameDoc) { d.Frames[0].Pixels = []int{0, 3, 1, 0} },
		"index too small": func(d *PixelFrameDoc) { d.Frames[0].Pixels = []int{-5, 1, 1, 0} },
		"short palette":   func(d *PixelFrameDoc) { d.Palette = []string{"#000000"} },
		"too many frames": func(d *PixelFrameDoc) { d.Frames = make([]PixelFrame, MaxPixelFrames+1) },
	}
	for name, mutate := range cases {
		doc := validDoc()
		mutate(&doc)
		if err := ValidatePixelDoc(doc, 2, 2); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestValidatePixelDocTransparentIndexAllowed(t *testing.T) {
	doc := validDoc()
	doc.Transparent = -1
	doc.Frames[0].Pixels = []int{-1, 1, 1, -1}
	if err := ValidatePixelDoc(doc, 2, 2); err != nil {
		t.Fatalf("transparent indices rejected: %v", err)
	}
}

func TestParsePixelFramesInvalidJSON(t *testing.T) {
	if _, err := ParsePixelFrames("not json", 2, 2); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePixelFramesSkipsCountCheckWithoutDims(t *testing.T) {
	doc := validDoc()
	doc.Frames[0].Pixels = []int{0, 1} // wrong count
	raw, _ := json.Marshal(doc)
	if _, err := ParsePixelFrames(string(raw), 0, 0); err != nil {
		t.Fatalf("expected count check skipped with zero dims: %v", err)
	}
	if _, err := ParsePixelFrames(string(raw), 2, 2); err == nil {
		t.Fatal("expected count check to fail with real dims")
	}
}

func decodePixelPNG(t *testing.T, img *RenderedImage) *image.RGBA {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("png.Decode error: %v", err)
	}
	return decoded.(*image.RGBA)
}

func pixelAt(img *image.RGBA, x, y int) [4]uint8 {
	r, g, b, a := img.At(x, y).RGBA()
	return [4]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func TestRenderPixelFramesGridScaling(t *testing.T) {
	doc := validDoc() // frame 0: [[black, red], [red, black]]
	img, err := RenderPixelFramesGrid(doc, 0, 2, 2, 4, 4, nil)
	if err != nil {
		t.Fatalf("RenderPixelFramesGrid error: %v", err)
	}
	decoded := decodePixelPNG(t, img)

	// scale=2, centered: cell (0,0)=black covers (0..1, 0..1); cell (1,0)=red covers (2..3, 0..1)
	if got := pixelAt(decoded, 0, 0); got[0] != 0 || got[1] != 0 {
		t.Errorf("cell(0,0) top-left: want black, got %v", got)
	}
	if got := pixelAt(decoded, 3, 1); got[0] != 255 || got[1] != 0 {
		t.Errorf("cell(1,0) area: want red, got %v", got)
	}
	if got := pixelAt(decoded, 0, 3); got[0] != 255 || got[1] != 0 {
		t.Errorf("cell(0,1) area: want red, got %v", got)
	}
}

func TestRenderPixelFramesGridLetterbox(t *testing.T) {
	doc := validDoc()
	img, err := RenderPixelFramesGrid(doc, 0, 2, 2, 8, 4, nil)
	if err != nil {
		t.Fatalf("RenderPixelFramesGrid error: %v", err)
	}
	decoded := decodePixelPNG(t, img)

	// scale=min(8/2,4/2)=2 → drawW=4, offX=(8-4)/2=2. Columns 0-1 and 6-7 are letterbox.
	bg := pixelAt(decoded, 0, 0)
	if bg[0] != 0 || bg[1] != 0 || bg[2] != 0 {
		t.Errorf("letterbox column: want background black, got %v", bg)
	}
	if got := pixelAt(decoded, 2, 1); got[0] != 0 || got[1] != 0 {
		t.Errorf("artwork start column: want black cell, got %v", got)
	}
	if got := pixelAt(decoded, 5, 1); got[0] != 255 {
		t.Errorf("red cell after offset: want red, got %v", got)
	}
	if got := pixelAt(decoded, 6, 1); got[0] != 0 || got[1] != 0 {
		t.Errorf("letterbox after artwork: want background black, got %v", got)
	}
}

func TestRenderPixelFramesGridCropOversized(t *testing.T) {
	// 4x4 grid onto 2x2 canvas must not error (center-crop + warning).
	big := PixelFrameDoc{
		Palette: []string{"#123456"},
		Frames:  []PixelFrame{{Duration: 100, Pixels: make([]int, 16)}},
	}
	for i := range big.Frames[0].Pixels {
		big.Frames[0].Pixels[i] = 0
	}
	if _, err := RenderPixelFramesGrid(big, 0, 4, 4, 2, 2, nil); err != nil {
		t.Fatalf("oversized grid should crop, got error: %v", err)
	}
}

func TestRenderPixelFramesGridTransparentUsesBackground(t *testing.T) {
	doc := validDoc()
	doc.Background = "#112233"
	doc.Frames[0].Pixels = []int{-1, 1, 1, -1}
	img, err := RenderPixelFramesGrid(doc, 0, 2, 2, 4, 4, nil)
	if err != nil {
		t.Fatalf("RenderPixelFramesGrid error: %v", err)
	}
	decoded := decodePixelPNG(t, img)
	got := pixelAt(decoded, 0, 0)
	if got[0] != 0x11 || got[1] != 0x22 || got[2] != 0x33 {
		t.Errorf("transparent cell: want background #112233, got %v", got)
	}
}

func TestRenderPixelFramesGridFrameSelection(t *testing.T) {
	doc := validDoc()
	img, err := RenderPixelFramesGrid(doc, 1, 2, 2, 4, 4, nil)
	if err != nil {
		t.Fatalf("RenderPixelFramesGrid error: %v", err)
	}
	decoded := decodePixelPNG(t, img)
	// Frame 1 is all green (#00ff00).
	got := pixelAt(decoded, 1, 1)
	if got[1] != 255 || got[0] != 0 {
		t.Errorf("frame 1: want green, got %v", got)
	}
}

func TestRenderPixelFramesGridOutOfRangeIndexClamps(t *testing.T) {
	doc := validDoc()
	doc.Frames[0].Pixels = []int{99, 1, 1, 0} // invalid index renders as background, no panic
	if _, err := RenderPixelFramesGrid(doc, 0, 2, 2, 4, 4, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderPixelFramesGridErrors(t *testing.T) {
	if _, err := RenderPixelFramesGrid(PixelFrameDoc{}, 0, 2, 2, 4, 4, nil); err == nil {
		t.Error("empty doc: expected error")
	}
	doc := validDoc()
	if _, err := RenderPixelFramesGrid(doc, 0, 0, 2, 4, 4, nil); err == nil {
		t.Error("zero grid width: expected error")
	}
}
