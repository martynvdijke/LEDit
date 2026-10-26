package legacy

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"ledit/render"
	"ledit/render/effects"
	"ledit/render/screensaver"
)

var legacyNames = []string{"legacy-matrix-rain", "legacy-plasma", "legacy-starfield", "legacy-dvd"}

func TestLegacyAdaptersAreRegistered(t *testing.T) {
	names := effects.Names()
	index := map[string]bool{}
	for _, n := range names {
		index[n] = true
	}
	for _, want := range legacyNames {
		if !index[want] {
			t.Fatalf("adapter %q is not registered (have %v)", want, names)
		}
		_, eff, ok := effects.Lookup(want)
		if !ok || eff.Name() != want {
			t.Fatalf("Lookup(%q) failed", want)
		}
	}
}

func TestLegacyPlasmaMatchesOriginalBytes(t *testing.T) {
	assertMatchesScreensaver(t, "legacy-plasma", screensaver.DrawPlasma)
}

func TestLegacyStarfieldMatchesOriginalBytes(t *testing.T) {
	assertMatchesScreensaver(t, "legacy-starfield", screensaver.DrawStarfield)
}

func TestLegacyDVDMatchesOriginalBytes(t *testing.T) {
	assertMatchesScreensaver(t, "legacy-dvd", screensaver.DrawDVD)
}

func assertMatchesScreensaver(t *testing.T, name string, draw func(width, height int, elapsed time.Duration) *image.RGBA) {
	t.Helper()
	_, eff, ok := effects.Lookup(name)
	if !ok {
		t.Fatalf("adapter %q is not registered", name)
	}
	for _, size := range [][2]int{{64, 32}, {16, 16}, {1, 1}, {7, 3}} {
		for _, elapsed := range []time.Duration{0, 500 * time.Millisecond, 2 * time.Second} {
			dst := image.NewRGBA(image.Rect(0, 0, size[0], size[1]))
			eff.Draw(dst, effects.Params{}, elapsed)
			want := draw(size[0], size[1], elapsed)
			if !equalPixels(dst, want) {
				t.Fatalf("%s at %dx%d elapsed=%v is not byte-identical to the original", name, size[0], size[1], elapsed)
			}
		}
	}
}

func TestLegacyMatrixRainMatchesOriginalBytes(t *testing.T) {
	_, eff, ok := effects.Lookup("legacy-matrix-rain")
	if !ok {
		t.Fatal("legacy-matrix-rain is not registered")
	}
	for _, size := range [][2]int{{64, 32}, {32, 64}, {1, 1}} {
		for _, elapsed := range []time.Duration{0, 900 * time.Millisecond, 3 * time.Second} {
			dst := image.NewRGBA(image.Rect(0, 0, size[0], size[1]))
			eff.Draw(dst, effects.Params{}, elapsed)
			img, err := render.RenderMatrixRain(time.UnixMilli(elapsed.Milliseconds()), size[0], size[1])
			if err != nil {
				t.Fatalf("RenderMatrixRain: %v", err)
			}
			decoded, err := pngDecode(img.Data)
			if err != nil {
				t.Fatalf("decoding legacy frame: %v", err)
			}
			if !equalPixels(dst, decoded) {
				t.Fatalf("legacy-matrix-rain at %dx%d elapsed=%v is not byte-identical to the original", size[0], size[1], elapsed)
			}
		}
	}
}

func pngDecode(data []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(data))
}

func equalPixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}
