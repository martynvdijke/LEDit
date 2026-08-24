package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestApplyBrightness_Identity(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{100, 150, 200, 255})
		}
	}
	orig := make([]byte, len(img.Pix))
	copy(orig, img.Pix)
	ret := ApplyBrightness(img, 100)
	if ret != img {
		t.Fatalf("level 100 should return same pointer")
	}
	if !bytes.Equal(img.Pix, orig) {
		t.Fatalf("100%% identity should not modify pixels")
	}
}

func TestApplyBrightness_Black(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 128, 64, 200})
	img.Set(1, 0, color.RGBA{10, 20, 30, 255})
	ApplyBrightness(img, 0)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			c := img.RGBAAt(x, y)
			if c.R != 0 || c.G != 0 || c.B != 0 {
				t.Fatalf("0%%: pixel %d,%d = %v, want black", x, y, c)
			}
		}
	}
	// alpha preserved
	if img.RGBAAt(0, 0).A != 200 {
		t.Fatalf("alpha should be preserved, got %d", img.RGBAAt(0, 0).A)
	}
	if img.RGBAAt(1, 0).A != 255 {
		t.Fatalf("alpha 255 preserved")
	}
}

func TestApplyBrightness_30Approx(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{100, 200, 255, 255})
	ApplyBrightness(img, 30)
	c := img.RGBAAt(0, 0)
	if c.R != 30 || c.G != 60 || c.B != 76 {
		t.Fatalf("30%% scaling got %v want {30 60 76 255}", c)
	}
	if c.A != 255 {
		t.Fatalf("alpha preserved")
	}
}

func TestApplyBrightness_AlphaPreserved(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{200, 100, 50, 123})
	img.Set(1, 1, color.RGBA{200, 100, 50, 10})
	ApplyBrightness(img, 50)
	if img.RGBAAt(0, 0).A != 123 || img.RGBAAt(1, 1).A != 10 {
		t.Fatalf("alpha not preserved")
	}
}

func TestApplyBrightnessNRGBA_And_AfterBlend(t *testing.T) {
	// Ensure brightness is applied after blending, not per-source.
	// Simulate fade blend then apply brightness.
	red := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	blue := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			red.Set(x, y, color.NRGBA{255, 0, 0, 255})
			blue.Set(x, y, color.NRGBA{0, 0, 255, 255})
		}
	}
	blended := BlendFade(red, blue, 0.5)
	// blended should be ~127,0,127
	blendedCopy := *blended
	_ = blendedCopy
	ApplyBrightnessNRGBA(blended, 50)
	c := blended.NRGBAAt(0, 0)
	// blended 127 *0.5 ~63
	if c.R < 62 || c.R > 64 || c.B < 62 || c.B > 64 {
		t.Fatalf("after blend brightness 50 got %v want ~63", c)
	}
	// Also verify ApplyBrightness returns same pointer for 100
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	if ApplyBrightness(img, 100) != img {
		t.Fatalf("100 returns same pointer")
	}
	nimg := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	nimg.Set(0, 0, color.NRGBA{10, 20, 30, 255})
	if ApplyBrightnessNRGBA(nimg, 100) != nimg {
		t.Fatalf("NRGBA 100 same pointer")
	}
	_ = png.Encode // ensure import used indirectly
	_ = bytes.NewReader
}

func BenchmarkApplyBrightness64x64(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 255)
	}
	// preserve alpha 255
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// operate on copy to avoid blackening
		cp := image.NewRGBA(img.Bounds())
		copy(cp.Pix, img.Pix)
		ApplyBrightness(cp, 30)
	}
}
