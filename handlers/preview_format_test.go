package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"ledit/render"
)

func previewPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.NRGBA{10, 20, 30, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func TestWriteImageResponse_FormatNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := &Server{}
	pngData := previewPNGBytes(t)
	ri := &render.RenderedImage{Format: "PNG", Data: pngData}

	tests := []struct {
		query     string
		wantCT    string
		wantCode  int
		checkBody func([]byte) bool
	}{
		{"", "image/png", 200, func(b []byte) bool { return bytes.Equal(b, pngData) }},
		{"format=png", "image/png", 200, func(b []byte) bool { return bytes.Equal(b, pngData) }},
		{"format=webp", "image/webp", 200, func(b []byte) bool { return len(b) > 0 && !bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'}) }},
		{"format=bogus", "", 400, nil},
		{"format=WEBP", "image/webp", 200, nil},
	}
	for _, tc := range tests {
		url := "/preview"
		if tc.query != "" {
			url += "?" + tc.query
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, url, nil)
		// preset headers to verify preservation
		c.Header("Cache-Control", "no-store")
		c.Header("X-LEDit-Stale", "1")
		srv.writeImageResponse(c, ri)
		if w.Code != tc.wantCode {
			t.Fatalf("query %q: code %d want %d body %s", tc.query, w.Code, tc.wantCode, w.Body.String())
		}
		if tc.wantCT != "" {
			ct := w.Header().Get("Content-Type")
			if ct != tc.wantCT {
				t.Fatalf("query %q: Content-Type %q want %q", tc.query, ct, tc.wantCT)
			}
		}
		if tc.checkBody != nil && !tc.checkBody(w.Body.Bytes()) {
			t.Fatalf("query %q: body check failed", tc.query)
		}
		// headers preserved?
		if tc.wantCode == 200 {
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control not preserved for %q", tc.query)
			}
			if w.Header().Get("X-LEDit-Stale") != "1" {
				t.Fatalf("X-LEDit-Stale not preserved for %q", tc.query)
			}
		}
	}
}
