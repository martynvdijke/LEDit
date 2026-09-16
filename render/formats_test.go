package render

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"golang.org/x/image/webp"
)

func TestEncodeWebP(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 60), uint8(y * 60), 100, 255})
		}
	}
	data, err := EncodeWebP(img)
	if err != nil {
		t.Fatalf("EncodeWebP: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty webp")
	}
	// Must not be PNG
	pngSig := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if bytes.HasPrefix(data, pngSig) {
		t.Fatal("webp data starts with PNG signature")
	}
	dec, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("webp decode: %v", err)
	}
	if dec.Bounds().Dx() != 4 || dec.Bounds().Dy() != 4 {
		t.Fatalf("dimensions %v want 4x4", dec.Bounds())
	}
	r, g, b, _ := dec.At(0, 0).RGBA()
	// just check not all zero / alpha opaque
	if r == 0 && g == 0 && b == 0 {
		t.Logf("warning: pixel (0,0) is black, check: %d %d %d", r, g, b)
	}
}

func TestEncodeGIF_TwoFrames(t *testing.T) {
	f1 := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	f2 := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			f1.Set(x, y, color.RGBA{255, 0, 0, 255})
			f2.Set(x, y, color.RGBA{0, 255, 0, 255})
		}
	}
	data, err := EncodeGIF([]image.Image{f1, f2}, 25)
	if err != nil {
		t.Fatalf("EncodeGIF: %v", err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gif decode: %v", err)
	}
	if len(g.Image) != 2 {
		t.Fatalf("frames %d want 2", len(g.Image))
	}
	if g.Delay[0] != 25 {
		t.Fatalf("delay %d want 25", g.Delay[0])
	}
}

func TestEncodeGIF_DefaultDelay(t *testing.T) {
	f1 := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	data, err := EncodeGIF([]image.Image{f1}, 0)
	if err != nil {
		t.Fatalf("EncodeGIF: %v", err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gif decode: %v", err)
	}
	if g.Delay[0] != 10 {
		t.Fatalf("default delay %d want 10", g.Delay[0])
	}
}

func TestEncodeGIF_ZeroFrames(t *testing.T) {
	_, err := EncodeGIF(nil, 10)
	if err == nil {
		t.Fatal("expected error for zero frames")
	}
}

func TestEncodeGIF_PalettedPassthrough(t *testing.T) {
	pm := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.RGBA{255, 0, 0, 255}})
	pm.Pix[0] = 0
	data, err := EncodeGIF([]image.Image{pm}, 10)
	if err != nil {
		t.Fatalf("EncodeGIF paletted: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty")
	}
}
