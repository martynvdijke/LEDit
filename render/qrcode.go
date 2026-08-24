package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/skip2/go-qrcode"
)

// QRCodeParams holds render inputs.
type QRCodeParams struct {
	Payload         string
	Caption         string
	ErrorCorrection string
	QuietZone       int
	Width           int
	Height          int
}

func eccLevel(s string) qrcode.RecoveryLevel {
	switch s {
	case "L":
		return qrcode.Low
	case "M":
		return qrcode.Medium
	case "Q":
		return qrcode.High
	case "H":
		return qrcode.Highest
	default:
		return qrcode.Medium
	}
}

// RenderQRCode renders a QR code scaled to width x height.
func RenderQRCode(p QRCodeParams) (*RenderedImage, error) {
	if p.Width <= 0 || p.Height <= 0 {
		p.Width = 64
		p.Height = 64
	}
	qz := p.QuietZone
	if qz < 0 {
		qz = 0
	}
	if qz > 8 {
		qz = 8
	}
	level := eccLevel(p.ErrorCorrection)
	qr, err := qrcode.New(p.Payload, level)
	if err != nil {
		return renderDegraded(p.Width, p.Height, "QR error")
	}
	// Disable border - we handle quiet zone ourselves.
	qr.DisableBorder = true
	bitmap := qr.Bitmap() // [][]bool
	m := len(bitmap)
	if m == 0 {
		return renderDegraded(p.Width, p.Height, "QR empty")
	}
	totalModules := m + 2*qz
	scale := min(p.Width, p.Height) / totalModules
	if scale < 1 {
		// Degraded placeholder: small checker + text
		return renderDegraded(p.Width, p.Height, "too small")
	}
	codeSize := totalModules * scale
	offX := (p.Width - codeSize) / 2
	offY := (p.Height - codeSize) / 2

	// Caption handling: reserve space if caption present.
	captionHeight := 0
	captionGap := 2
	if p.Caption != "" {
		// Need at least charH + gap below code.
		need := simpleCharH + captionGap
		remaining := p.Height - (offY + codeSize) - captionGap
		if remaining >= need {
			captionHeight = simpleCharH
			// Shift code up slightly to make room? Keep centered but ensure caption fits.
			// Adjust offY up by half need if needed to keep caption visible.
			// Currently offY is centered; check if caption fits without overlap.
			// If not enough bottom margin, move QR up.
			if offY+codeSize+captionGap+captionHeight > p.Height {
				offY = p.Height - codeSize - captionGap - captionHeight
				if offY < 0 {
					offY = 0
				}
			}
		} else {
			// Check if we can shrink vertical position slightly to fit caption.
			// Try moving QR up: available space from top
			if offY >= need {
				// we could shift up, but our offY is centered; compute alternative.
				newOffY := (p.Height - captionHeight - captionGap - codeSize) / 2
				if newOffY >= 0 && newOffY+codeSize+captionGap+captionHeight <= p.Height {
					offY = newOffY
					captionHeight = simpleCharH
				} else {
					captionHeight = 0
				}
			} else {
				captionHeight = 0
			}
		}
		if captionHeight == 0 {
			// omit caption
		}
		_ = captionHeight
	}
	// If captionHeight determined, recompute caption rendering flag.
	renderCaption := false
	if p.Caption != "" {
		need := simpleCharH
		if offY+codeSize+captionGap+need <= p.Height {
			renderCaption = true
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	// White background
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	black := color.RGBA{0, 0, 0, 255}

	// Draw modules
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			if !bitmap[y][x] {
				continue
			}
			px := offX + (x+qz)*scale
			py := offY + (y+qz)*scale
			fillRect(img, px, py, px+scale-1, py+scale-1, black)
		}
	}

	if renderCaption {
		cy := offY + codeSize + captionGap
		// Center caption horizontally
		tw := len(p.Caption) * simpleCharW
		cx := (p.Width - tw) / 2
		if cx < 0 {
			cx = 0
		}
		drawStringSimple(img, p.Caption, cx, cy, black)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

func renderDegraded(w, h int, msg string) (*RenderedImage, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	// simple border
	black := color.RGBA{0, 0, 0, 255}
	for x := 0; x < w; x++ {
		img.Set(x, 0, black)
		img.Set(x, h-1, black)
	}
	for y := 0; y < h; y++ {
		img.Set(0, y, black)
		img.Set(w-1, y, black)
	}
	// message truncated
	if len(msg)*simpleCharW < w-4 {
		cx := (w - len(msg)*simpleCharW) / 2
		cy := (h - simpleCharH) / 2
		drawStringSimple(img, msg, cx, cy, black)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
