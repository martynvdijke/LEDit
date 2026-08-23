package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// CellSize computes the panel dimensions for a rows×cols grid with the given
// gap on a width×height canvas. The remainder pixels after dividing by the
// grid (plus gaps) are left as background.
func CellSize(rows, cols, gap, width, height int) (int, int) {
	cellW := (width - (cols-1)*gap) / cols
	cellH := (height - (rows-1)*gap) / rows
	if cellW < 1 {
		cellW = 1
	}
	if cellH < 1 {
		cellH = 1
	}
	return cellW, cellH
}

// PanelOrigin returns the top-left origin of the panel at (row, col) given
// the computed cell size and gap.
func PanelOrigin(row, col, gap, cellW, cellH int) (int, int) {
	return col * (cellW + gap), row * (cellH + gap)
}

// PanelFontSize scales the theme font to fit a panel: roughly panelH/4,
// clamped to a minimum of 8 so titles stay readable.
func PanelFontSize(panelH int) float64 {
	size := panelH / 4
	if size < 8 {
		size = 8
	}
	return float64(size)
}

// RenderPanel renders a data dict into a small panel: pixel border, title and
// up to as many rows as fit, with the font scaled to the panel height. Used by
// matrix grid cells where RenderDict's fixed layout would overflow.
func RenderPanel(data map[string]string, width, height int, theme Theme, fontPath string) (*RenderedImage, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	bg := color.RGBA{theme.BackgroundColor[0], theme.BackgroundColor[1], theme.BackgroundColor[2], 255}
	accent := color.RGBA{theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2], 255}
	textCol := color.RGBA{theme.TextColor[0], theme.TextColor[1], theme.TextColor[2], 255}

	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// Pixel border sized to the panel.
	pixelSize := 4
	if width >= 64 && height >= 64 {
		pixelSize = 8
	}

	// Font scaled to panel height.
	size := PanelFontSize(height)
	theme.FontSize = size
	face, fontErr := loadFont(fontPath, size)
	margin := width / 10
	if margin < 4 {
		margin = 4
	}
	yPos := pixelSize + int(size)
	if fontErr == nil {
		drawString(img, theme.Title, margin, yPos, face, accent)
	} else {
		drawStringSimple(img, theme.Title, margin, yPos, accent)
	}
	yPos += int(size) + 4

	// Rows, clamped to what fits above the bottom border.
	rowH := int(size) + 6
	maxRows := (height - yPos - pixelSize) / rowH
	if maxRows < 0 {
		maxRows = 0
	}
	scrolls := false
	availW := width - 2*margin
	if availW < 1 {
		availW = 1
	}
	i := 0
	for key, value := range data {
		if i >= maxRows {
			break
		}
		markerX := margin - 6
		markerY := yPos + 6
		for dy := range 5 {
			for dx := range 5 {
				if markerX+dx >= 0 && markerY+dy >= 0 && markerX+dx < width && markerY+dy < height {
					img.Set(markerX+dx, markerY+dy, accent)
				}
			}
		}
		if fontErr == nil {
			drawString(img, key+": "+value, margin, yPos, face, textCol)
		} else {
			text := key + ": " + value
			if shouldScroll(text, availW) {
				scrolls = true
				drawStringSimpleScrolling(img, text, margin, yPos, textCol, availW)
			} else {
				drawStringSimple(img, text, margin, yPos, textCol)
			}
		}
		yPos += rowH
		i++
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes(), Scrolls: scrolls}, nil
}

// TemplateGrid renders the PNG template for a matrix layout: background, cell
// grid lines, per-cell coordinates (R1C1), bound source short names and EMPTY
// marks for unbound cells. names[row][col] is the short name of the bound
// source, or "" for unbound cells.
func TemplateGrid(rows, cols, gap int, bg color.RGBA, names [][]string, width, height int) (*RenderedImage, error) {
	if rows < 1 || cols < 1 {
		return nil, fmt.Errorf("invalid grid dimensions %dx%d", rows, cols)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	accent := color.RGBA{80, 250, 123, 255}
	line := color.RGBA{139, 233, 253, 255}

	cellW, cellH := CellSize(rows, cols, gap, width, height)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			x, y := PanelOrigin(r, c, gap, cellW, cellH)
			// Cell border.
			for px := x; px < x+cellW; px++ {
				img.Set(px, y, line)
				img.Set(px, y+cellH-1, line)
			}
			for py := y; py < y+cellH; py++ {
				img.Set(x, py, line)
				img.Set(x+cellW-1, py, line)
			}
			// Coordinates.
			coord := fmt.Sprintf("R%dC%d", r+1, c+1)
			drawStringSimple(img, coord, x+3, y+8, accent)
			// Bound source name or EMPTY mark.
			name := ""
			if r < len(names) && c < len(names[r]) {
				name = names[r][c]
			}
			if name == "" {
				name = "EMPTY"
			}
			if len(name) > 12 {
				name = name[:12]
			}
			drawStringSimple(img, name, x+3, y+int(cellH/2), line)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}
