package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"ledit/render"
)

const (
	maxUploadBytes    = 5 * 1024 * 1024
	maxDecodedDim     = 1024
	maxDecodedBytes   = 8 * 1024 * 1024
	maxGIFFrames      = 64
	maxPaletteSize    = 64
	maxTargetGrid     = 128
	defaultTarget     = 32
	defaultAutoColors = 16
)

// ---- color helpers ----

func parseHexToRGBA(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	var r, g, b uint8
	_, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}, true
}

func colorDistance(a, b color.RGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

func nearestPaletteColor(c color.RGBA, palette []color.RGBA) int {
	best := 0
	bestD := colorDistance(c, palette[0])
	for i := 1; i < len(palette); i++ {
		d := colorDistance(c, palette[i])
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}

// floydSteinbergDither returns grid of palette indices for scaled (W*H).
func floydSteinbergDither(scaled image.Image, palette []color.RGBA) []int {
	b := scaled.Bounds()
	w, h := b.Dx(), b.Dy()
	// copy to float errors per pixel per channel
	type errPix struct{ r, g, b float64 }
	errs := make([]errPix, w*h)
	// initialize with actual colors
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rr, gg, bb, _ := scaled.At(b.Min.X+x, b.Min.Y+y).RGBA()
			errs[y*w+x] = errPix{r: float64(rr >> 8), g: float64(gg >> 8), b: float64(bb >> 8)}
		}
	}
	out := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			e := errs[idx]
			cr := clampByte(e.r)
			cg := clampByte(e.g)
			cb := clampByte(e.b)
			cur := color.RGBA{R: cr, G: cg, B: cb, A: 255}
			pi := nearestPaletteColor(cur, palette)
			out[idx] = pi
			chosen := palette[pi]
			er := e.r - float64(chosen.R)
			eg := e.g - float64(chosen.G)
			eb := e.b - float64(chosen.B)
			// distribute
			if x+1 < w {
				n := idx + 1
				errs[n].r += er * 7.0 / 16
				errs[n].g += eg * 7.0 / 16
				errs[n].b += eb * 7.0 / 16
			}
			if y+1 < h {
				if x > 0 {
					n := (y+1)*w + x - 1
					errs[n].r += er * 3.0 / 16
					errs[n].g += eg * 3.0 / 16
					errs[n].b += eb * 3.0 / 16
				}
				n := (y+1)*w + x
				errs[n].r += er * 5.0 / 16
				errs[n].g += eg * 5.0 / 16
				errs[n].b += eb * 5.0 / 16
				if x+1 < w {
					n2 := (y+1)*w + x + 1
					errs[n2].r += er * 1.0 / 16
					errs[n2].g += eg * 1.0 / 16
					errs[n2].b += eb * 1.0 / 16
				}
			}
		}
	}
	return out
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func clampByte(f float64) uint8 {
	if f < 0 {
		return 0
	}
	if f > 255 {
		return 255
	}
	return uint8(f + 0.5)
}

// medianCut derived from design
func medianCut(colors []color.RGBA, n int) []color.RGBA {
	if n <= 0 {
		return nil
	}
	if len(colors) == 0 {
		return []color.RGBA{{}}
	}
	// deduplicate
	seen := map[uint32]bool{}
	uniq := []color.RGBA{}
	for _, c := range colors {
		k := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		if !seen[k] {
			seen[k] = true
			uniq = append(uniq, c)
		}
	}
	if len(uniq) <= n {
		// pad? just return uniq
		return uniq[:min(n, len(uniq))]
	}
	type box struct {
		cols []color.RGBA
	}
	boxes := []box{{cols: uniq}}
	for len(boxes) < n {
		// find box with largest range to split
		bestIdx := -1
		bestRange := -1
		for i, b := range boxes {
			if len(b.cols) < 2 {
				continue
			}
			var minR, maxR, minG, maxG, minB, maxB uint8
			minR, minG, minB = 255, 255, 255
			for _, c := range b.cols {
				if c.R < minR {
					minR = c.R
				}
				if c.R > maxR {
					maxR = c.R
				}
				if c.G < minG {
					minG = c.G
				}
				if c.G > maxG {
					maxG = c.G
				}
				if c.B < minB {
					minB = c.B
				}
				if c.B > maxB {
					maxB = c.B
				}
			}
			rng := max(int(maxR)-int(minR), max(int(maxG)-int(minG), int(maxB)-int(minB)))
			if rng > bestRange {
				bestRange = rng
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			break
		}
		b := boxes[bestIdx]
		// determine widest channel
		var minR, maxR, minG, maxG, minB, maxB uint8
		minR, minG, minB = 255, 255, 255
		for _, c := range b.cols {
			if c.R < minR {
				minR = c.R
			}
			if c.R > maxR {
				maxR = c.R
			}
			if c.G < minG {
				minG = c.G
			}
			if c.G > maxG {
				maxG = c.G
			}
			if c.B < minB {
				minB = c.B
			}
			if c.B > maxB {
				maxB = c.B
			}
		}
		dr := int(maxR) - int(minR)
		dg := int(maxG) - int(minG)
		db := int(maxB) - int(minB)
		channel := 0
		if dg > dr && dg >= db {
			channel = 1
		} else if db > dr && db > dg {
			channel = 2
		}
		// sort by channel
		cols := append([]color.RGBA(nil), b.cols...)
		// simple sort
		for i := 1; i < len(cols); i++ {
			j := i
			for j > 0 {
				a, b2 := cols[j-1], cols[j]
				var av, bv uint8
				switch channel {
				case 0:
					av, bv = a.R, b2.R
				case 1:
					av, bv = a.G, b2.G
				default:
					av, bv = a.B, b2.B
				}
				if av <= bv {
					break
				}
				cols[j-1], cols[j] = cols[j], cols[j-1]
				j--
			}
		}
		mid := len(cols) / 2
		b1 := box{cols: cols[:mid]}
		b2 := box{cols: cols[mid:]}
		boxes[bestIdx] = b1
		boxes = append(boxes, b2)
	}
	// average each box
	out := make([]color.RGBA, 0, len(boxes))
	for _, b := range boxes {
		var sr, sg, sb int
		for _, c := range b.cols {
			sr += int(c.R)
			sg += int(c.G)
			sb += int(c.B)
		}
		cnt := len(b.cols)
		if cnt == 0 {
			continue
		}
		out = append(out, color.RGBA{R: uint8(sr / cnt), G: uint8(sg / cnt), B: uint8(sb / cnt), A: 255})
	}
	return out
}

// scaleToTarget
func scaleToTarget(src image.Image, w, h int, mode string) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if mode == "stretch" {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)
		return dst
	}
	if mode == "crop" {
		// center-crop source to target aspect
		targetAspect := float64(w) / float64(h)
		srcAspect := float64(sw) / float64(sh)
		var crop image.Rectangle
		if srcAspect > targetAspect {
			// wider: crop width
			newW := int(float64(sh)*targetAspect + 0.5)
			x0 := (sw - newW) / 2
			crop = image.Rect(sb.Min.X+x0, sb.Min.Y, sb.Min.X+x0+newW, sb.Min.Y+sh)
		} else {
			newH := int(float64(sw)/targetAspect + 0.5)
			y0 := (sh - newH) / 2
			crop = image.Rect(sb.Min.X, sb.Min.Y+y0, sb.Min.X+sw, sb.Min.Y+y0+newH)
		}
		// crop source
		cropped, ok := src.(interface {
			SubImage(r image.Rectangle) image.Image
		})
		var sub image.Image
		if ok {
			sub = cropped.SubImage(crop)
		} else {
			// fallback: manual copy
			tmp := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
			draw.Draw(tmp, tmp.Bounds(), src, crop.Min, draw.Src)
			sub = tmp
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), sub, sub.Bounds(), draw.Over, nil)
		return dst
	}
	// fit: letterbox
	scale := minF(float64(w)/float64(sw), float64(h)/float64(sh))
	nw := int(float64(sw)*scale + 0.5)
	nh := int(float64(sh)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	scaled := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, draw.Over, nil)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// fill with black / transparent border
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
	offX := (w - nw) / 2
	offY := (h - nh) / 2
	draw.Draw(dst, image.Rect(offX, offY, offX+nw, offY+nh), scaled, image.Point{}, draw.Src)
	return dst
}

// decode helpers
func decodeImage(data []byte) (image.Image, string, error) {
	// try png, jpeg, webp via image.Decode
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("unsupported image format")
	}
	return img, format, nil
}

func decodeGIF(data []byte) (*gif.GIF, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid GIF")
	}
	return g, nil
}

// composite GIF frames simply (Background/Previous simplified)
func compositeGIFFrames(g *gif.GIF) []image.Image {
	frames := make([]image.Image, len(g.Image))
	// canvas for compositing
	rect := g.Config
	_ = rect
	// Use first frame bounds; gif images are paletted with Rect matching bounds
	var prev *image.RGBA
	bgColor := color.RGBA{0, 0, 0, 0}
	if int(g.BackgroundIndex) < len(g.Image[0].Palette) {
		c := g.Image[0].Palette[g.BackgroundIndex]
		rr, gg, bb, aa := c.RGBA()
		bgColor = color.RGBA{uint8(rr >> 8), uint8(gg >> 8), uint8(bb >> 8), uint8(aa >> 8)}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	// fill with bg
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)
	for i, pal := range g.Image {
		disposal := byte(g.Disposal[i])
		if disposal == 2 { // Background: clear to bg before drawing?
			// clear frame rect to bg
			draw.Draw(canvas, pal.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)
		} else if disposal == 3 && prev != nil {
			// Previous: restore prev
			draw.Draw(canvas, canvas.Bounds(), prev, image.Point{}, draw.Src)
		}
		// save previous for disposal 3 next frame
		if disposal == 3 {
			prev = cloneRGBA(canvas)
		}
		draw.Draw(canvas, pal.Bounds(), pal, pal.Bounds().Min, draw.Over)
		// snapshot
		frames[i] = cloneRGBA(canvas)
		if disposal == 2 {
			// after drawing, will be cleared at next iteration; keep as drawn snapshot already
		}
	}
	return frames
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// import pipeline result
type importResult struct {
	palette      []string
	frames       []render.PixelFrame
	gridW, gridH int
}

func runImportPipeline(data []byte, targetW, targetH int, aspect string, paletteHex []string, autoPalette bool, autoColors int) (*importResult, error) {
	if targetW < 1 || targetW > maxTargetGrid || targetH < 1 || targetH > maxTargetGrid {
		return nil, fmt.Errorf("target dimensions must be 1-%d", maxTargetGrid)
	}
	if len(paletteHex) > maxPaletteSize {
		return nil, fmt.Errorf("palette too large (max %d)", maxPaletteSize)
	}
	// try GIF first if gif header
	isGIF := len(data) >= 3 && string(data[:3]) == "GIF"
	var images []image.Image
	var delays []int
	if isGIF {
		g, err := decodeGIF(data)
		if err == nil && len(g.Image) > 0 {
			if len(g.Image) > maxGIFFrames {
				return nil, fmt.Errorf("too many GIF frames (max %d)", maxGIFFrames)
			}
			// cap check decoded bytes
			if g.Config.Width > maxDecodedDim || g.Config.Height > maxDecodedDim {
				return nil, fmt.Errorf("decoded image too large (max %dx%d)", maxDecodedDim, maxDecodedDim)
			}
			if g.Config.Width*g.Config.Height*4*len(g.Image) > maxDecodedBytes {
				return nil, fmt.Errorf("decoded GIF too large (max 8MB)")
			}
			images = compositeGIFFrames(g)
			delays = g.Delay
		} else if err != nil {
			// fallback to static decode
			isGIF = false
		}
	}
	if !isGIF {
		img, _, err := decodeImage(data)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		if b.Dx() > maxDecodedDim || b.Dy() > maxDecodedDim {
			return nil, fmt.Errorf("decoded image too large (max %dx%d)", maxDecodedDim, maxDecodedDim)
		}
		if b.Dx()*b.Dy()*4 > maxDecodedBytes {
			return nil, fmt.Errorf("decoded image too large (max 8MB)")
		}
		images = []image.Image{img}
	}
	// palette preparation
	var palRGBA []color.RGBA
	var palHexOut []string
	if autoPalette {
		if autoColors < 4 {
			autoColors = 4
		}
		if autoColors > 64 {
			autoColors = 64
		}
		// collect colors from scaled images after scaling? spec says median-cut on scaled image
		// we need to scale first then collect; so we do two-pass: scale then medianCut
		// but to derive palette we need scaled images; we will scale first using temporary then derive
		scaledTmp := make([]image.Image, len(images))
		for i, im := range images {
			scaledTmp[i] = scaleToTarget(im, targetW, targetH, aspect)
		}
		allColors := []color.RGBA{}
		for _, si := range scaledTmp {
			b := si.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					r, g, bb, _ := si.At(x, y).RGBA()
					allColors = append(allColors, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bb >> 8), 255})
				}
			}
		}
		derived := medianCut(allColors, autoColors)
		for _, c := range derived {
			palHexOut = append(palHexOut, fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B))
			palRGBA = append(palRGBA, c)
		}
		// proceed to dither with derived
		frames := make([]render.PixelFrame, len(scaledTmp))
		for i, si := range scaledTmp {
			pixels := floydSteinbergDither(si, palRGBA)
			dur := 500
			if i < len(delays) {
				d := delays[i] * 10
				if d == 0 {
					d = 100
				}
				dur = d
			}
			frames[i] = render.PixelFrame{Duration: dur, Pixels: pixels}
		}
		return &importResult{palette: palHexOut, frames: frames, gridW: targetW, gridH: targetH}, nil
	}
	// existing palette path
	if len(paletteHex) == 0 {
		// default: auto 16
		return runImportPipeline(data, targetW, targetH, aspect, nil, true, defaultAutoColors)
	}
	for _, h := range paletteHex {
		c, ok := parseHexToRGBA(h)
		if !ok {
			return nil, fmt.Errorf("invalid palette color %q", h)
		}
		palRGBA = append(palRGBA, c)
		palHexOut = paletteHex
	}
	frames := make([]render.PixelFrame, len(images))
	for i, im := range images {
		scaled := scaleToTarget(im, targetW, targetH, aspect)
		pixels := floydSteinbergDither(scaled, palRGBA)
		dur := 500
		if i < len(delays) {
			d := delays[i] * 10
			if d == 0 {
				d = 100
			}
			dur = d
		}
		frames[i] = render.PixelFrame{Duration: dur, Pixels: pixels}
	}
	return &importResult{palette: palHexOut, frames: frames, gridW: targetW, gridH: targetH}, nil
}

func pixelsToPNGBase64(pixels []int, palette []string, w, h int) (string, error) {
	pal := make([]color.RGBA, len(palette))
	for i, s := range palette {
		c, _ := parseHexToRGBA(s)
		pal[i] = c
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := pixels[y*w+x]
			if idx < 0 || idx >= len(pal) {
				img.Set(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				img.Set(x, y, pal[idx])
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// HTTP handlers

func (s *Server) PixelArtImport(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	// file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()
	if header.Size > maxUploadBytes {
		slog.Warn("import cap violation", "reason", "upload too large", "size", header.Size)
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 5MB)"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file"})
		return
	}
	if len(data) > maxUploadBytes {
		slog.Warn("import cap violation", "reason", "upload too large")
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 5MB)"})
		return
	}
	// also use jpeg/png magic check via decode
	targetW, _ := strconv.Atoi(c.PostForm("target_width"))
	targetH, _ := strconv.Atoi(c.PostForm("target_height"))
	if targetW == 0 {
		targetW = defaultTarget
	}
	if targetH == 0 {
		targetH = defaultTarget
	}
	aspect := c.PostForm("aspect")
	if aspect == "" {
		aspect = "fit"
	}
	if aspect != "fit" && aspect != "stretch" && aspect != "crop" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "aspect must be fit/stretch/crop"})
		return
	}
	// palette
	var paletteHex []string
	paletteJSON := c.PostForm("palette_json")
	if paletteJSON != "" {
		var arr []string
		if err := json.Unmarshal([]byte(paletteJSON), &arr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "palette_json must be JSON array of hex colors"})
			return
		}
		paletteHex = arr
	}
	autoPalette := c.PostForm("auto_palette") == "true" || c.PostForm("auto_palette") == "1"
	autoColors, _ := strconv.Atoi(c.PostForm("auto_palette_colors"))
	if autoColors == 0 {
		autoColors = defaultAutoColors
	}
	if !autoPalette && len(paletteHex) == 0 {
		// try palette_id lookup? treat as auto
		autoPalette = true
	}
	res, err := runImportPipeline(data, targetW, targetH, aspect, paletteHex, autoPalette, autoColors)
	if err != nil {
		slog.Warn("import pipeline failed", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// create draft pixelart
	doc := render.PixelFrameDoc{Palette: res.palette, Background: "#000000", Transparent: -1, Frames: res.frames}
	raw, _ := json.Marshal(doc)
	name := header.Filename
	if name == "" {
		name = "Imported Art"
	}
	// strip extension
	if dot := strings.LastIndex(name, "."); dot > 0 {
		name = name[:dot]
	}
	if name == "" {
		name = "Imported Art"
	}
	f := map[string]string{
		"name":        name + " (imported)",
		"grid_width":  strconv.Itoa(res.gridW),
		"grid_height": strconv.Itoa(res.gridH),
		"frames":      string(raw),
		"bindings":    "{}",
		"api_url":     "",
		"api_token":   "",
		"enabled":     "on",
	}
	entry := dsRegistry["pixelart"]
	obj, err := entry.CreateFields(s.DB, s.Ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create pixelart: " + err.Error()})
		return
	}
	// attach to general settings edge if exists (best effort)
	if gs, err := s.DB.GeneralSettings.Query().First(s.Ctx); err == nil && gs != nil {
		_ = entry.AddEdge(s.DB.GeneralSettings.UpdateOne(gs), obj).Exec(s.Ctx)
	}
	// return id
	var id int
	switch v := obj.(type) {
	case *struct{ ID int }:
		id = v.ID
	default:
		// use reflection for ent PixelArt
		// try type assert to ent.PixelArt
		if pa, ok := obj.(interface{ GetID() int }); ok {
			id = pa.GetID()
		} else {
			// fallback via json? ent.PixelArt has ID field
			b, _ := json.Marshal(obj)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			if v, ok := m["id"].(float64); ok {
				id = int(v)
			}
		}
	}
	// try direct ent.PixelArt
	if id == 0 {
		// attempt to read via ent.PixelArt struct
		// use generic via interface with field ID
		type hasID struct{ ID int }
		b, _ := json.Marshal(obj)
		var h hasID
		_ = json.Unmarshal(b, &h)
		id = h.ID
	}
	// if still 0, query latest by name
	if id == 0 {
		if pa, err := s.DB.PixelArt.Query().Order().All(s.Ctx); err == nil && len(pa) > 0 {
			id = pa[len(pa)-1].ID
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) PixelArtImportPreview(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()
	if header.Size > maxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 5MB)"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file"})
		return
	}
	if len(data) > maxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 5MB)"})
		return
	}
	targetW, _ := strconv.Atoi(c.PostForm("target_width"))
	targetH, _ := strconv.Atoi(c.PostForm("target_height"))
	if targetW == 0 {
		targetW = defaultTarget
	}
	if targetH == 0 {
		targetH = defaultTarget
	}
	aspect := c.PostForm("aspect")
	if aspect == "" {
		aspect = "fit"
	}
	if aspect != "fit" && aspect != "stretch" && aspect != "crop" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "aspect must be fit/stretch/crop"})
		return
	}
	var paletteHex []string
	if pj := c.PostForm("palette_json"); pj != "" {
		var arr []string
		if err := json.Unmarshal([]byte(pj), &arr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "palette_json must be JSON array of hex colors"})
			return
		}
		paletteHex = arr
	}
	autoPalette := c.PostForm("auto_palette") == "true" || c.PostForm("auto_palette") == "1"
	autoColors, _ := strconv.Atoi(c.PostForm("auto_palette_colors"))
	if autoColors == 0 {
		autoColors = defaultAutoColors
	}
	if !autoPalette && len(paletteHex) == 0 {
		autoPalette = true
	}
	res, err := runImportPipeline(data, targetW, targetH, aspect, paletteHex, autoPalette, autoColors)
	if err != nil {
		slog.Warn("preview pipeline failed", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(res.frames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no frames produced"})
		return
	}
	b64, err := pixelsToPNGBase64(res.frames[0].Pixels, res.palette, res.gridW, res.gridH)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode preview"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"png_b64": b64})
}

// ensure jpeg import used
var _ = jpeg.Decode
var _ = png.Decode
