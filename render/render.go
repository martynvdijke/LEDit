package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type RenderedImage struct {
	Format  string
	Data    []byte
	Scrolls bool
}

func RenderDict(dataDict map[string]string, width, height int, theme Theme, fontPath string) (*RenderedImage, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	fillBG := color.RGBA{theme.BackgroundColor[0], theme.BackgroundColor[1], theme.BackgroundColor[2], 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{fillBG}, image.Point{}, draw.Src)

	accent := color.RGBA{theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2], 255}
	textCol := color.RGBA{theme.TextColor[0], theme.TextColor[1], theme.TextColor[2], 255}

	pixelSize := 8
	for x := 0; x < width; x += pixelSize {
		var c, c2 color.Color
		if (x/pixelSize)%2 == 0 {
			c = textCol
			c2 = accent
		} else {
			c = accent
			c2 = textCol
		}
		fillRect(img, x, 0, x+pixelSize-1, pixelSize-1, c)
		fillRect(img, x, height-pixelSize, x+pixelSize-1, height-1, c2)
	}
	for y := 0; y < height; y += pixelSize {
		var c, c2 color.Color
		if (y/pixelSize)%2 == 0 {
			c = textCol
			c2 = accent
		} else {
			c = accent
			c2 = textCol
		}
		fillRect(img, 0, y, pixelSize-1, y+pixelSize-1, c)
		fillRect(img, width-pixelSize, y, width-1, y+pixelSize-1, c2)
	}

	margin := 40
	yPos := 50

	face, fontErr := loadFont(fontPath, theme.FontSize)
	if fontErr == nil {
		drawString(img, theme.Title, margin, 20, face, accent)
	} else {
		drawStringSimple(img, theme.Title, margin, 20, accent)
	}

	for y := yPos; y < yPos+3; y++ {
		for x := margin; x < width-margin; x++ {
			img.Set(x, y, textCol)
		}
	}
	yPos += 20

	scrolls := false
	availW := width - 2*margin
	if availW < 1 {
		availW = 1
	}
	for key, value := range dataDict {
		markerX := margin - 15
		markerY := yPos + 8
		for dy := range 8 {
			for dx := range 8 {
				img.Set(markerX+dx, markerY+dy, accent)
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
		yPos += 35
	}

	for y := 0; y < height; y += 4 {
		for x := range width {
			existing := img.RGBAAt(x, y)
			existing.R = uint8(float64(existing.R) * 0.8)
			existing.G = uint8(float64(existing.G) * 0.8)
			existing.B = uint8(float64(existing.B) * 0.8)
			img.Set(x, y, existing)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes(), Scrolls: scrolls}, nil
}

func RenderText(text string, width, height int, bgColor, textColor string, fontSize float64, fontPath string) (*RenderedImage, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	bg := parseHexColor(bgColor, color.RGBA{0, 0, 0, 255})
	tc := parseHexColor(textColor, color.RGBA{255, 255, 255, 255})

	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	face, fontErr := loadFont(fontPath, fontSize)
	if fontErr != nil {
		// Fallback: simple rendering without font
		drawStringSimple(img, text, 20, height/2, tc)
	} else {
		defer face.Close()

		// Word wrap: split into lines that fit within width
		lines := wordWrap(text, face, width-40)

		// Calculate total text height
		totalHeight := len(lines) * int(fontSize+8)
		startY := (height / 2) - (totalHeight / 2) + int(fontSize)

		for i, line := range lines {
			// Measure line width for centering
			lineWidth := font.MeasureString(face, line)
			xOff := (fixed.I(width) - lineWidth) / 2
			if xOff.Ceil() < 0 {
				xOff = fixed.I(10)
			}
			d := &font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(tc),
				Face: face,
				Dot:  fixed.Point26_6{X: xOff, Y: fixed.I(startY + i*(int(fontSize)+8))},
			}
			d.DrawString(line)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

func parseHexColor(hex string, fallback color.RGBA) color.RGBA {
	if len(hex) < 6 {
		return fallback
	}
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return fallback
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

func wordWrap(text string, face font.Face, maxWidth int) []string {
	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	current := ""
	for _, word := range words {
		testLine := current
		if testLine != "" {
			testLine += " "
		}
		testLine += word
		w := font.MeasureString(face, testLine).Ceil()
		if w > maxWidth && current != "" {
			lines = append(lines, current)
			current = word
		} else {
			current = testLine
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func fillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			img.Set(x, y, c)
		}
	}
}

func loadFont(fontPath string, fontSize float64) (font.Face, error) {
	f, err := os.Open(fontPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	fnt, err := sfnt.Parse(b)
	if err != nil {
		return nil, err
	}

	return opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func drawString(img *image.RGBA, text string, x, y int, face font.Face, col color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(text)
}

func drawStringSimple(img *image.RGBA, text string, x, y int, col color.Color) {
	baseX := x
	baseY := y
	charW := 6
	charH := 7
	for _, r := range text {
		if r == '\n' {
			baseX = x
			baseY += charH + 2
			continue
		}
		idx := int(r - 32)
		if idx < 0 || idx >= len(simpleFont) {
			baseX += charW
			continue
		}
		glyph := simpleFont[idx]
		for row := range charH {
			for colBit := range 5 {
				if glyph[row][colBit] == 1 {
					img.Set(baseX+colBit, baseY+row, col)
				}
			}
		}
		baseX += charW
	}
}

const simpleCharW = 6
const simpleCharH = 7

func shouldScroll(text string, availW int) bool {
	return len(text)*simpleCharW > availW
}

func drawStringSimpleScrolling(img *image.RGBA, text string, x, y int, col color.Color, availW int) {
	charW := simpleCharW
	charH := simpleCharH
	gap := charW * 3
	textW := len(text) * charW
	offset := ScrollOffset(time.Now(), 100, 30, textW, gap)
	total := textW + gap
	if total <= 0 {
		return
	}
	// Build strip image of width total, height charH.
	strip := image.NewRGBA(image.Rect(0, 0, total, charH))
	drawStringSimple(strip, text, 0, 0, col)
	// Copy visible window into target, clipped to availW.
	for dx := 0; dx < availW; dx++ {
		srcX := (offset + dx) % total
		for row := 0; row < charH; row++ {
			c := strip.RGBAAt(srcX, row)
			if c.A != 0 {
				tx := x + dx
				ty := y + row
				if tx >= 0 && tx < img.Bounds().Dx() && ty >= 0 && ty < img.Bounds().Dy() {
					img.Set(tx, ty, col)
				}
			}
		}
	}
}

// ApplyBrightness scales each RGB channel by level/100. Level 100 is a no-op,
// level 0 yields black; alpha is preserved. Returns same pointer for 100.
func ApplyBrightness(img *image.RGBA, level int) *image.RGBA {
	if img == nil {
		return nil
	}
	if level >= 100 {
		return img
	}
	if level <= 0 {
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				i := img.PixOffset(x, y)
				img.Pix[i+0] = 0
				img.Pix[i+1] = 0
				img.Pix[i+2] = 0
			}
		}
		return img
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = uint8(int(img.Pix[i+0]) * level / 100)
		img.Pix[i+1] = uint8(int(img.Pix[i+1]) * level / 100)
		img.Pix[i+2] = uint8(int(img.Pix[i+2]) * level / 100)
	}
	return img
}

// ApplyBrightnessNRGBA is the NRGBA variant applied to transition blended frames.
func ApplyBrightnessNRGBA(img *image.NRGBA, level int) *image.NRGBA {
	if img == nil {
		return nil
	}
	if level >= 100 {
		return img
	}
	if level <= 0 {
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i+0] = 0
			img.Pix[i+1] = 0
			img.Pix[i+2] = 0
		}
		return img
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = uint8(int(img.Pix[i+0]) * level / 100)
		img.Pix[i+1] = uint8(int(img.Pix[i+1]) * level / 100)
		img.Pix[i+2] = uint8(int(img.Pix[i+2]) * level / 100)
	}
	return img
}

func ReadFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func GetExtension(path string) string {
	ext := filepath.Ext(path)
	if len(ext) > 0 {
		return ext[1:]
	}
	return ""
}

func ChartRect(width, height int) image.Rectangle {
	h := 16
	if height < 32 {
		h = height / 3
		if h < 8 {
			h = 8
		}
	}
	return image.Rect(0, height-h, width, height)
}
