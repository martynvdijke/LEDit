package render

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// PanelLogicalWidth returns the width of the logical canvas that includes the
// bezel gaps between chained panels. For a single panel (or no gap) it is the
// physical width.
func PanelLogicalWidth(width, cols, gap int) int {
	if cols < 2 || gap <= 0 {
		return width
	}
	return width + (cols-1)*gap
}

// ValidatePanelSpec checks a device's horizontal panel topology. width is the
// physical framebuffer width the device receives; each panel must have an
// integer pixel width.
func ValidatePanelSpec(cols, gap, width, height int) error {
	if cols < 1 || cols > 16 {
		return fmt.Errorf("panel_cols must be between 1 and 16")
	}
	if gap < 0 || gap > 64 {
		return fmt.Errorf("panel_gap must be between 0 and 64")
	}
	if height <= 0 {
		return fmt.Errorf("height must be positive")
	}
	if cols > width {
		return fmt.Errorf("panel_cols must not exceed width")
	}
	if cols > 1 && width%cols != 0 {
		return fmt.Errorf("width must divide evenly by panel_cols")
	}
	return nil
}

// SlicePanelGapsPNG removes the hidden bezel gap columns from a logical-canvas
// frame, producing the physical frame the device receives. Every visible panel
// column is preserved in order; only the gap spans are dropped. The input is
// returned unchanged when there are no gaps to remove.
func SlicePanelGapsPNG(data []byte, cols, gap int) ([]byte, error) {
	if cols < 2 || gap <= 0 {
		return data, nil
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	outW := b.Dx() - (cols-1)*gap
	panelW := outW / cols
	if panelW <= 0 || outW <= 0 {
		return data, nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, outW, b.Dy()))
	for c := 0; c < cols; c++ {
		srcX := b.Min.X + c*panelW + c*gap
		draw.Draw(dst,
			image.Rect(c*panelW, 0, c*panelW+panelW, b.Dy()),
			src,
			image.Point{X: srcX, Y: b.Min.Y},
			draw.Src)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
