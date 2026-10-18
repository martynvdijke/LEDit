package render

import (
	"bytes"
	"image/color"
	"testing"
	"time"

	"ledit/render/effects"
)

func effectParamsJSON(t *testing.T, name string) string {
	t.Helper()
	desc, _, ok := effects.Lookup(name)
	if !ok {
		t.Fatalf("unknown effect %q", name)
	}
	raw, err := effects.MarshalParams(desc.Defaults.StripPalette())
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return string(raw)
}

func TestValidateOverlaySpecEffect(t *testing.T) {
	base := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "hi", Background: "#000000", Foreground: "#ffffff"}

	withEffect := base
	withEffect.EffectName = "plasma"
	withEffect.EffectParams = effectParamsJSON(t, "plasma")
	if err := ValidateOverlaySpec(withEffect, 64); err != nil {
		t.Fatalf("valid effect spec rejected: %v", err)
	}

	unknown := base
	unknown.EffectName = "not-an-effect"
	if err := ValidateOverlaySpec(unknown, 64); err == nil {
		t.Fatal("unknown overlay effect must be rejected")
	}

	badParams := base
	badParams.EffectName = "plasma"
	badParams.EffectParams = "{not json"
	if err := ValidateOverlaySpec(badParams, 64); err == nil {
		t.Fatal("invalid overlay effect params must be rejected")
	}
}

// TestCompositeOverlayPNGWithoutEffectIgnoresEffectFields proves the effect path
// is opt-in: a spec with no effect name produces the same bytes whether or not a
// palette was supplied, and an identical effect-free spec is stable.
func TestCompositeOverlayPNGWithoutEffectIgnoresEffectFields(t *testing.T) {
	data := testFrame(t, 64, 64)
	now := time.Unix(1700000000, 0)

	spec := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "HI", Background: "#000000", Foreground: "#ffffff"}
	plain, err := CompositeOverlayPNG(data, spec, now)
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	withPalette := spec
	withPalette.EffectPalette = effects.PaletteFromRGB([3]uint8{255, 0, 0}, [3]uint8{0, 255, 0}, [3]uint8{0, 0, 255})
	other, err := CompositeOverlayPNG(data, withPalette, now)
	if err != nil {
		t.Fatalf("composite with palette: %v", err)
	}
	if !bytes.Equal(plain, other) {
		t.Fatal("a palette without an effect name must not change the frame")
	}

	// A disabled spec still returns the input bytes untouched.
	out, err := CompositeOverlayPNG(data, DefaultOverlaySpec(), now)
	if err != nil {
		t.Fatalf("composite disabled: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatal("disabled overlay must return the input bytes")
	}
}

func TestCompositeOverlayPNGEffectRendersBehindText(t *testing.T) {
	data := testFrame(t, 64, 64)
	now := time.Unix(1700000000, 0)

	plain := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "HI", Background: "#000000", Foreground: "#ffffff"}
	plainOut, err := CompositeOverlayPNG(data, plain, now)
	if err != nil {
		t.Fatalf("composite plain: %v", err)
	}

	eff := plain
	eff.EffectName = "plasma"
	eff.EffectParams = effectParamsJSON(t, "plasma")
	eff.EffectPalette = effects.PaletteFromRGB([3]uint8{20, 30, 200}, [3]uint8{200, 40, 30}, [3]uint8{255, 240, 120})
	effOut, err := CompositeOverlayPNG(data, eff, now)
	if err != nil {
		t.Fatalf("composite effect: %v", err)
	}
	if bytes.Equal(plainOut, effOut) {
		t.Fatal("an effect-backed strip must differ from the solid strip")
	}

	img := decodeRGBA(t, effOut)
	b := img.Bounds()
	// The area above the strip is untouched (white).
	if c := img.RGBAAt(0, 0); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Fatalf("top pixel changed: %+v", c)
	}
	// The strip is no longer a flat black fill: it holds several distinct colors.
	seen := map[color.RGBA]struct{}{}
	textPixels := 0
	for y := b.Max.Y - 8; y < b.Max.Y; y++ {
		for x := 0; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			seen[c] = struct{}{}
			if c.R == 255 && c.G == 255 && c.B == 255 {
				textPixels++
			}
		}
	}
	if len(seen) < 3 {
		t.Fatalf("effect strip looks flat: %d distinct colors", len(seen))
	}
	if textPixels == 0 {
		t.Fatal("overlay text vanished behind the effect")
	}
}

func TestCompositeOverlayPNGEffectIsDeterministic(t *testing.T) {
	data := testFrame(t, 64, 64)
	now := time.Unix(1700000000, 0)
	spec := OverlaySpec{
		Enabled: true, Position: "bottom", Height: 8, Text: "HI",
		Background: "#000000", Foreground: "#ffffff",
		EffectName: "plasma", EffectParams: effectParamsJSON(t, "plasma"),
	}
	first, err := CompositeOverlayPNG(data, spec, now)
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	second, err := CompositeOverlayPNG(data, spec, now)
	if err != nil {
		t.Fatalf("composite: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same send time must reproduce the same frame")
	}
	// A later tick advances the animation.
	later, err := CompositeOverlayPNG(data, spec, now.Add(2*effects.TickInterval))
	if err != nil {
		t.Fatalf("composite later: %v", err)
	}
	if bytes.Equal(first, later) {
		t.Fatal("the strip effect must animate across ticks")
	}
}

func TestCompositeOverlayPNGEffectUnknownFallsBackToSolid(t *testing.T) {
	data := testFrame(t, 64, 64)
	now := time.Unix(1700000000, 0)
	plain := OverlaySpec{Enabled: true, Position: "bottom", Height: 8, Text: "HI", Background: "#000000", Foreground: "#ffffff"}
	want, err := CompositeOverlayPNG(data, plain, now)
	if err != nil {
		t.Fatalf("composite plain: %v", err)
	}
	broken := plain
	broken.EffectName = "does-not-exist"
	got, err := CompositeOverlayPNG(data, broken, now)
	if err != nil {
		t.Fatalf("composite with unknown effect must not error: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatal("an unknown effect must degrade to the solid strip")
	}
}
