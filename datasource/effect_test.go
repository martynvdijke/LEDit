package datasource

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"ledit/render"
	"ledit/render/effects"
)

// Compile-time guarantees: an effect source satisfies every contract the feed,
// preview and picker surfaces rely on.
var (
	_ Datasource     = (*EffectDS)(nil)
	_ ThemedRenderer = (*EffectDS)(nil)
	_ Animator       = (*EffectDS)(nil)
	_ Ambienter      = (*EffectDS)(nil)
	_ StateProvider  = nil
	_                = effects.Params{}
)

func TestEffectDSImplementsContracts(t *testing.T) {
	ds := &EffectDS{Effect: "plasma"}
	if !ds.Ambient() {
		t.Fatal("effects must report ambient so panel cells bypass the TTL cache")
	}
	if ds.FrameCount() <= 1 {
		t.Fatalf("effects must animate, FrameCount = %d", ds.FrameCount())
	}
	if !SupportsTheme(ds) {
		t.Fatal("effect source must support themed rendering")
	}
	if dst, ok := EffectKind(Datasource(ds)); !ok || dst != ds {
		t.Fatal("EffectKind did not recognise the effect source")
	}
}

func TestEffectDSDeterministicBeforeFirstFrame(t *testing.T) {
	// With no start time the elapsed clock reads zero, so two renders at the
	// same size, params and theme must be byte-identical.
	ds := &EffectDS{Effect: "scan", Params: effects.Params{Speed: 50, Intensity: 50, Density: 50, Direction: "left"}}
	first, err := ds.GetPNGThemed(64, 32, DefaultTheme())
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := ds.GetPNGThemed(64, 32, DefaultTheme())
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if !bytes.Equal(first.Data, second.Data) {
		t.Fatal("effect render is not deterministic at a fixed tick")
	}
}

func TestEffectDSAnimatesAcrossTicks(t *testing.T) {
	ds := &EffectDS{Effect: "scan", Params: effects.Params{Speed: 50, Intensity: 60, Density: 50, Direction: "left"}}
	base := time.Unix(1700000000, 0)
	if idx := ds.NextFrame(base); idx != 0 {
		t.Fatalf("first NextFrame = %d, want 0", idx)
	}
	if idx := ds.NextFrame(base.Add(200 * time.Millisecond)); idx != 3 {
		t.Fatalf("NextFrame after 200ms = %d, want 3", idx)
	}

	ds2 := &EffectDS{Effect: "scan", Params: effects.Params{Speed: 50, Intensity: 60, Density: 50, Direction: "left"}}
	ds2.startTime = base
	ds3 := &EffectDS{Effect: "scan", Params: effects.Params{Speed: 50, Intensity: 60, Density: 50, Direction: "left"}}
	ds3.startTime = base.Add(-2 * time.Second) // 30 ticks earlier

	a, err := ds2.GetPNGThemed(64, 32, DefaultTheme())
	if err != nil {
		t.Fatalf("tick a: %v", err)
	}
	b, err := ds3.GetPNGThemed(64, 32, DefaultTheme())
	if err != nil {
		t.Fatalf("tick b: %v", err)
	}
	if bytes.Equal(a.Data, b.Data) {
		t.Fatal("animated effect produced identical frames on different ticks")
	}
}

func TestEffectDSUsesThemePalette(t *testing.T) {
	params := effects.Params{Speed: 50, Intensity: 60, Density: 60, Direction: "left"}
	plasma := &EffectDS{Effect: "plasma", Params: params}
	other := &EffectDS{Effect: "plasma", Params: params}

	base := DefaultTheme()
	alt := base
	alt.BackgroundColor = [3]uint8{4, 4, 40}
	alt.AccentColor = [3]uint8{255, 0, 128}
	alt.TextColor = [3]uint8{255, 255, 255}

	a, err := plasma.GetPNGThemed(64, 32, base)
	if err != nil {
		t.Fatalf("default theme render: %v", err)
	}
	b, err := other.GetPNGThemed(64, 32, alt)
	if err != nil {
		t.Fatalf("alt theme render: %v", err)
	}
	if bytes.Equal(a.Data, b.Data) {
		t.Fatal("effect frame did not change with the theme palette")
	}
}

func TestEffectDSUnknownEffect(t *testing.T) {
	ds := &EffectDS{Effect: "not-registered"}
	if _, err := ds.GetPNG(32, 32); err != ErrEffectUnknown {
		t.Fatalf("expected ErrEffectUnknown, got %v", err)
	}
	if _, err := ds.GetPNGThemed(32, 32, DefaultTheme()); err != ErrEffectUnknown {
		t.Fatalf("expected ErrEffectUnknown from themed render, got %v", err)
	}
}

func TestEffectDSResolutionAndEncoding(t *testing.T) {
	ds := &EffectDS{Effect: "gradient", Params: effects.Params{Speed: 50, Intensity: 50, Density: 50}}
	cases := []struct{ w, h, wantW, wantH int }{
		{64, 32, 64, 32},
		{0, 0, 64, 64},
		{-5, 20, 64, 20},
		{1, 1, 1, 1},
	}
	for _, tc := range cases {
		img, err := ds.GetPNG(tc.w, tc.h)
		if err != nil {
			t.Fatalf("GetPNG(%d,%d): %v", tc.w, tc.h, err)
		}
		if img == nil || img.Format != "PNG" || len(img.Data) == 0 {
			t.Fatalf("GetPNG(%d,%d) returned an empty frame", tc.w, tc.h)
		}
		decoded, err := png.Decode(bytes.NewReader(img.Data))
		if err != nil {
			t.Fatalf("decode PNG: %v", err)
		}
		if b := decoded.Bounds(); b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Fatalf("GetPNG(%d,%d) bounds = %dx%d, want %dx%d", tc.w, tc.h, b.Dx(), b.Dy(), tc.wantW, tc.wantH)
		}
	}
}

func TestEffectSourceName(t *testing.T) {
	cases := []struct{ preset, effect, want string }{
		{"Aurora", "gradient", "Aurora"},
		{"", "gradient", "Effect: gradient"},
		{"", "", "Effect"},
	}
	for _, tc := range cases {
		if got := EffectSourceName(tc.preset, tc.effect); got != tc.want {
			t.Fatalf("EffectSourceName(%q,%q) = %q, want %q", tc.preset, tc.effect, got, tc.want)
		}
	}
}

func TestEffectDSLogicalWidthRender(t *testing.T) {
	// Multi-panel devices render at the logical canvas width; the effect must
	// fill exactly that width so the existing gap slicer stays unchanged.
	ds := &EffectDS{Effect: "noise", Params: effects.Params{Speed: 40, Intensity: 50, Density: 40}}
	logical := render.PanelLogicalWidth(64, 2, 2)
	img, err := ds.GetPNGThemed(logical, 32, DefaultTheme())
	if err != nil {
		t.Fatalf("logical width render: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Bounds().Dx() != logical {
		t.Fatalf("logical render width = %d, want %d", decoded.Bounds().Dx(), logical)
	}
	if _, err := render.SlicePanelGapsPNG(img.Data, 2, 2); err != nil {
		t.Fatalf("panel slicing must still work on an effect frame: %v", err)
	}
}
