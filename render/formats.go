package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"

	webp "github.com/HugoSmits86/nativewebp"
)

// EncodeWebP encodes img to WebP (lossless VP8L) and returns the bytes.
func EncodeWebP(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EncodeGIF encodes frames to an animated GIF.
// delayCS is the per-frame delay in centiseconds (1/100s); if <=0 it defaults to 10.
// Each frame is preserved if already *image.Paletted, otherwise it is quantized
// via draw.FloydSteinberg with palette.Plan9.
func EncodeGIF(frames []image.Image, delayCS int) ([]byte, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames")
	}
	if delayCS <= 0 {
		delayCS = 10
	}
	var g gif.GIF
	for _, fr := range frames {
		var pm *image.Paletted
		if p, ok := fr.(*image.Paletted); ok {
			pm = p
		} else {
			b := fr.Bounds()
			pm = image.NewPaletted(b, palette.Plan9)
			draw.FloydSteinberg.Draw(pm, b, fr, b.Min)
		}
		g.Image = append(g.Image, pm)
		g.Delay = append(g.Delay, delayCS)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &g); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
