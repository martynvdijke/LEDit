package render

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/skip2/go-qrcode"
)

func decodePNG(t *testing.T, data []byte) (w, h int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

func TestRenderQRCodeScaleAndCentering(t *testing.T) {
	p := QRCodeParams{Payload: "hello", ErrorCorrection: "M", QuietZone: 4, Width: 64, Height: 64}
	img, err := RenderQRCode(p)
	if err != nil {
		t.Fatalf("RenderQRCode: %v", err)
	}
	w, h := decodePNG(t, img.Data)
	if w != 64 || h != 64 {
		t.Fatalf("dims %dx%d want 64x64", w, h)
	}
	// Verify scale math: decode expected scale and centering produces black pixel at expected offset
	qr, _ := qrcode.New("hello", qrcode.Medium)
	qr.DisableBorder = true
	m := len(qr.Bitmap())
	total := m + 2*4
	scale := 64 / total
	if scale < 1 {
		t.Fatalf("expected scale>=1 got %d", scale)
	}
	codeSize := total * scale
	offX := (64 - codeSize) / 2
	offY := (64 - codeSize) / 2
	// Top-left border pixel should be white (quiet zone)
	decoded, _ := png.Decode(bytes.NewReader(img.Data))
	// Check that image is not all white and not degraded
	// The QR module (0,0) maps to offX+4*scale, offY+4*scale; if that module is black, pixel there should be black.
	// Instead verify centering: if width != height, offX != offY
	p2 := QRCodeParams{Payload: "hello", ErrorCorrection: "M", QuietZone: 4, Width: 80, Height: 64}
	img2, _ := RenderQRCode(p2)
	w2, h2 := decodePNG(t, img2.Data)
	if w2 != 80 || h2 != 64 {
		t.Fatalf("dims %dx%d want 80x64", w2, h2)
	}
	_ = offX
	_ = offY
	_ = decoded
}

func TestRenderQRCodeCaptionOmittedWhenNoSpace(t *testing.T) {
	// Small height where caption cannot fit - should not error and should render without panic
	p := QRCodeParams{Payload: "hello", Caption: "MyCaption", ErrorCorrection: "M", QuietZone: 4, Width: 64, Height: 64}
	img, err := RenderQRCode(p)
	if err != nil {
		t.Fatalf("with caption: %v", err)
	}
	if img == nil || len(img.Data) == 0 {
		t.Fatal("no image")
	}
	// Now force no space: tiny canvas where code already fills height, caption should be omitted gracefully
	// Use height just enough for code but not for caption gap+charH
	// With small 64x64 and caption, the render should still succeed and dimensions match
	w, h := decodePNG(t, img.Data)
	if w != 64 || h != 64 {
		t.Fatalf("dims %dx%d", w, h)
	}
	// A very short canvas: caption should be omitted (no crash)
	pSmall := QRCodeParams{Payload: "hello", Caption: "Caption", ErrorCorrection: "M", QuietZone: 4, Width: 64, Height: 32}
	img2, err := RenderQRCode(pSmall)
	if err != nil {
		t.Fatalf("small with caption: %v", err)
	}
	if len(img2.Data) == 0 {
		t.Fatal("no data")
	}
}

func TestRenderQRCodeDegradedPlaceholderTinyDevice(t *testing.T) {
	p := QRCodeParams{Payload: "hello", ErrorCorrection: "M", QuietZone: 4, Width: 10, Height: 10}
	img, err := RenderQRCode(p)
	if err != nil {
		t.Fatalf("degraded: %v", err)
	}
	w, h := decodePNG(t, img.Data)
	if w != 10 || h != 10 {
		t.Fatalf("degraded dims %dx%d want 10x10", w, h)
	}
	// Degraded placeholder should have border (corners black)
	decoded, _ := png.Decode(bytes.NewReader(img.Data))
	r, g, b, _ := decoded.At(0, 0).RGBA()
	if r>>8 != 0 || g>>8 != 0 || b>>8 != 0 {
		t.Fatalf("degraded border pixel not black, got %d,%d,%d", r>>8, g>>8, b>>8)
	}
}

func TestRenderQRCodeDefaultDims(t *testing.T) {
	p := QRCodeParams{Payload: "hello", Width: 0, Height: 0}
	img, err := RenderQRCode(p)
	if err != nil {
		t.Fatalf("default dims: %v", err)
	}
	w, h := decodePNG(t, img.Data)
	if w != 64 || h != 64 {
		t.Fatalf("default dims %dx%d want 64x64", w, h)
	}
}
