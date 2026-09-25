package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func mkPNG(w, h int, c color.RGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestFrameToPixelsBilinearViaDevice(t *testing.T) {
	srv := newBackupTestServer(t)
	pngBytes := mkPNG(2, 2, color.RGBA{100, 100, 100, 255})
	// Enable bilinear
	d := srv.DB.DeviceSettings.Create().SetName("b").SetWidth(4).SetHeight(4).SetOutputBilinear(true).SaveX(srv.Ctx)
	pixels, err := frameToPixels(pngBytes, 4, 4, d)
	if err != nil {
		t.Fatalf("frameToPixels %v", err)
	}
	if len(pixels) != 48 {
		t.Fatalf("len %d", len(pixels))
	}
}

func TestFrameToPixelsPerPanel(t *testing.T) {
	srv := newBackupTestServer(t)
	img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		i := img.PixOffset(x, 0)
		img.Pix[i+0] = 10
		img.Pix[i+1] = 20
		img.Pix[i+2] = 30
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	pngBytes := buf.Bytes()
	d := srv.DB.DeviceSettings.Create().SetName("p").SetWidth(4).SetHeight(1).SetPanelCols(2).SetOutputPanelColorOrders(`["RGB","GRB"]`).SaveX(srv.Ctx)
	pixels, err := frameToPixels(pngBytes, 4, 1, d)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	// panel0 RGB, panel1 GRB
	if pixels[0] != 10 || pixels[1] != 20 {
		t.Fatalf("panel0 %v", pixels[0:3])
	}
	if pixels[6] != 20 || pixels[7] != 10 {
		t.Fatalf("panel1 GRB %v", pixels[6:9])
	}
}
