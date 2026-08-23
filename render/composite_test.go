package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func solidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = c.A
	}
	return img
}

func equalPix(a, b *image.NRGBA) bool {
	if !a.Bounds().Eq(b.Bounds()) {
		return false
	}
	return bytes.Equal(a.Pix, b.Pix)
}

func TestBlendEndpoints(t *testing.T) {
	prev := solidNRGBA(4, 4, color.NRGBA{10, 20, 30, 255})
	next := solidNRGBA(4, 4, color.NRGBA{200, 210, 220, 255})
	// Also test with distinct pattern to catch off-by-one.
	prev2 := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	next2 := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for i := 0; i < len(prev2.Pix); i += 4 {
		prev2.Pix[i+0] = uint8(i)
		prev2.Pix[i+1] = uint8(i + 1)
		prev2.Pix[i+2] = uint8(i + 2)
		prev2.Pix[i+3] = 255
		next2.Pix[i+0] = uint8(255 - i)
		next2.Pix[i+1] = uint8(255 - i - 1)
		next2.Pix[i+2] = uint8(255 - i - 2)
		next2.Pix[i+3] = 255
	}

	blends := []struct {
		name string
		fn   func(prev, next *image.NRGBA, t float64) *image.NRGBA
	}{
		{"Fade", BlendFade},
		{"Wipe", BlendWipe},
		{"Dissolve", BlendDissolve},
	}
	cases := []struct {
		name string
		prev *image.NRGBA
		next *image.NRGBA
	}{
		{"solid", prev, next},
		{"pattern", prev2, next2},
	}
	for _, b := range blends {
		for _, c := range cases {
			if got := b.fn(c.prev, c.next, 0); !equalPix(got, c.prev) {
				t.Errorf("%s %s t=0 not equal prev", b.name, c.name)
			}
			if got := b.fn(c.prev, c.next, 1); !equalPix(got, c.next) {
				t.Errorf("%s %s t=1 not equal next", b.name, c.name)
			}
			// Clamping.
			if got := b.fn(c.prev, c.next, -1); !equalPix(got, c.prev) {
				t.Errorf("%s %s t=-1 clamp failed", b.name, c.name)
			}
			if got := b.fn(c.prev, c.next, 2); !equalPix(got, c.next) {
				t.Errorf("%s %s t=2 clamp failed", b.name, c.name)
			}
		}
	}
}

func TestBlendDeterminism(t *testing.T) {
	prev := solidNRGBA(8, 8, color.NRGBA{10, 20, 30, 255})
	next := solidNRGBA(8, 8, color.NRGBA{200, 210, 220, 255})
	// Vary pattern to make dissolve more sensitive.
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := prev.PixOffset(x, y)
			prev.Pix[i+0] = uint8(x * 10)
			next.Pix[i+0] = uint8(255 - x*10)
		}
	}
	blends := []struct {
		name string
		fn   func(prev, next *image.NRGBA, t float64) *image.NRGBA
	}{
		{"Fade", BlendFade},
		{"Wipe", BlendWipe},
		{"Dissolve", BlendDissolve},
	}
	for _, b := range blends {
		for _, tVal := range []float64{0, 0.25, 0.5, 0.75, 1} {
			a := b.fn(prev, next, tVal)
			bb := b.fn(prev, next, tVal)
			if !bytes.Equal(a.Pix, bb.Pix) {
				t.Fatalf("%s determinism failed at t=%v", b.name, tVal)
			}
		}
	}
}

func TestBlendNoMutation(t *testing.T) {
	prev := solidNRGBA(2, 2, color.NRGBA{10, 20, 30, 255})
	next := solidNRGBA(2, 2, color.NRGBA{200, 210, 220, 255})
	prevCopy := make([]byte, len(prev.Pix))
	nextCopy := make([]byte, len(next.Pix))
	copy(prevCopy, prev.Pix)
	copy(nextCopy, next.Pix)
	_ = BlendFade(prev, next, 0.5)
	_ = BlendWipe(prev, next, 0.5)
	_ = BlendDissolve(prev, next, 0.5)
	if !bytes.Equal(prev.Pix, prevCopy) {
		t.Error("prev mutated")
	}
	if !bytes.Equal(next.Pix, nextCopy) {
		t.Error("next mutated")
	}
}

func TestBlendFadeMidpoint(t *testing.T) {
	red := solidNRGBA(3, 3, color.NRGBA{255, 0, 0, 255})
	blue := solidNRGBA(3, 3, color.NRGBA{0, 0, 255, 255})
	got := BlendFade(red, blue, 0.5)
	c := got.NRGBAAt(1, 1)
	// Average: (127 or 128, 0, 127 or 128)
	if c.R < 126 || c.R > 129 {
		t.Errorf("fade R %d want ~127", c.R)
	}
	if c.G != 0 {
		t.Errorf("fade G %d want 0", c.G)
	}
	if c.B < 126 || c.B > 129 {
		t.Errorf("fade B %d want ~127", c.B)
	}
	if c.A != 255 {
		t.Errorf("fade A %d want 255", c.A)
	}
}

func TestBlendWipeMonotonicity(t *testing.T) {
	w, h := 10, 4
	prev := solidNRGBA(w, h, color.NRGBA{0, 0, 0, 255})
	next := solidNRGBA(w, h, color.NRGBA{255, 255, 255, 255})
	countNextCols := func(img *image.NRGBA) int {
		cnt := 0
		for x := 0; x < w; x++ {
			isNext := true
			for y := 0; y < h; y++ {
				c := img.NRGBAAt(x, y)
				if c.R != 255 {
					isNext = false
					break
				}
			}
			if isNext {
				cnt++
			}
		}
		return cnt
	}
	prevCount := -1
	for _, tVal := range []float64{0, 0.1, 0.2, 0.33, 0.5, 0.7, 0.9, 1.0} {
		got := BlendWipe(prev, next, tVal)
		cnt := countNextCols(got)
		if cnt < prevCount {
			t.Fatalf("wipe monotonicity violated at t=%v: %d < %d", tVal, cnt, prevCount)
		}
		prevCount = cnt
		// Also check expected count equals int(t*w)
		want := int(tVal * float64(w))
		if cnt != want {
			t.Errorf("wipe t=%v got %d cols want %d", tVal, cnt, want)
		}
	}
}

func TestBlendDissolveRevealCount(t *testing.T) {
	w, h := 8, 8
	prev := solidNRGBA(w, h, color.NRGBA{0, 0, 0, 255})
	next := solidNRGBA(w, h, color.NRGBA{255, 255, 255, 255})
	n := w * h
	for _, tVal := range []float64{0, 0.1, 0.25, 0.5, 0.75, 1.0} {
		got := BlendDissolve(prev, next, tVal)
		wantK := int(tVal * float64(n))
		gotK := 0
		for i := 0; i < len(got.Pix); i += 4 {
			if got.Pix[i+0] == 255 && got.Pix[i+1] == 255 && got.Pix[i+2] == 255 {
				gotK++
			}
		}
		if gotK != wantK {
			t.Errorf("dissolve t=%v gotK=%d want %d", tVal, gotK, wantK)
		}
	}
}

func TestBlendDimsMismatch(t *testing.T) {
	prev := solidNRGBA(4, 4, color.NRGBA{10, 20, 30, 255})
	next := solidNRGBA(2, 2, color.NRGBA{200, 210, 220, 255})
	for _, fn := range []func(prev, next *image.NRGBA, t float64) *image.NRGBA{BlendFade, BlendWipe, BlendDissolve} {
		got := fn(prev, next, 1)
		if !got.Bounds().Eq(prev.Bounds()) {
			t.Fatalf("dims mismatch bounds %v want %v", got.Bounds(), prev.Bounds())
		}
		if !equalPix(got, solidNRGBA(4, 4, color.NRGBA{200, 210, 220, 255})) {
			// After scaling 2x2 solid to 4x4 solid, t=1 should be solid next color.
			t.Error("dims mismatch t=1 not next color")
		}
	}
}

func TestDecodeNRGBA(t *testing.T) {
	// Encode from NRGBA.
	srcNRGBA := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	srcNRGBA.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	srcNRGBA.Set(1, 0, color.NRGBA{0, 255, 0, 255})
	srcNRGBA.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	srcNRGBA.Set(1, 1, color.NRGBA{255, 255, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, srcNRGBA); err != nil {
		t.Fatalf("encode NRGBA: %v", err)
	}
	decoded, err := DecodeNRGBA(buf.Bytes())
	if err != nil {
		t.Fatalf("decode NRGBA: %v", err)
	}
	if decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("dims %v", decoded.Bounds())
	}
	for i := 3; i < len(decoded.Pix); i += 4 {
		if decoded.Pix[i] != 255 {
			t.Fatalf("alpha not 255 at %d: %d", i, decoded.Pix[i])
		}
	}
	// Encode from image.RGBA and Gray (non-NRGBA) — should composite over black.
	srcRGBA := image.NewRGBA(image.Rect(0, 0, 2, 2))
	srcRGBA.Set(0, 0, color.RGBA{255, 0, 0, 255})
	srcRGBA.Set(1, 0, color.RGBA{0, 255, 0, 255})
	srcRGBA.Set(0, 1, color.RGBA{0, 0, 255, 255})
	srcRGBA.Set(1, 1, color.RGBA{255, 255, 0, 255})
	buf.Reset()
	if err := png.Encode(&buf, srcRGBA); err != nil {
		t.Fatalf("encode RGBA: %v", err)
	}
	decoded2, err := DecodeNRGBA(buf.Bytes())
	if err != nil {
		t.Fatalf("decode RGBA: %v", err)
	}
	if decoded2.Bounds().Dx() != 2 || decoded2.Bounds().Dy() != 2 {
		t.Fatalf("dims %v", decoded2.Bounds())
	}
	for i := 3; i < len(decoded2.Pix); i += 4 {
		if decoded2.Pix[i] != 255 {
			t.Fatalf("alpha not 255 after RGBA decode at %d: %d", i, decoded2.Pix[i])
		}
	}
	// Also test a Gray image which decodes as *image.Gray (non-NRGBA).
	grayImg := image.NewGray(image.Rect(0, 0, 2, 2))
	grayImg.SetGray(0, 0, color.Gray{0})
	grayImg.SetGray(1, 0, color.Gray{128})
	grayImg.SetGray(0, 1, color.Gray{200})
	grayImg.SetGray(1, 1, color.Gray{255})
	buf.Reset()
	if err := png.Encode(&buf, grayImg); err != nil {
		t.Fatalf("encode Gray: %v", err)
	}
	decoded3, err := DecodeNRGBA(buf.Bytes())
	if err != nil {
		t.Fatalf("decode Gray: %v", err)
	}
	if decoded3.Bounds().Dx() != 2 || decoded3.Bounds().Dy() != 2 {
		t.Fatalf("dims %v", decoded3.Bounds())
	}
	for i := 3; i < len(decoded3.Pix); i += 4 {
		if decoded3.Pix[i] != 255 {
			t.Fatalf("alpha not 255 after Gray decode at %d", i)
		}
	}
}

func TestDecodeNRGBAInvalid(t *testing.T) {
	if _, err := DecodeNRGBA([]byte("not png")); err == nil {
		t.Error("expected error for invalid png")
	}
}
