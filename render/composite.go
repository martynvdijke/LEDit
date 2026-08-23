package render

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"math/rand"
)

// DecodeNRGBA decodes PNG bytes to an opaque *image.NRGBA.
// If the decoded image is already *image.NRGBA it is returned directly.
// Otherwise it is composited over an opaque black canvas of the same bounds
// (LED panels have no translucency).
func DecodeNRGBA(pngBytes []byte) (*image.NRGBA, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n, nil
	}
	bounds := img.Bounds()
	dst := image.NewNRGBA(bounds)
	// Fill with opaque black.
	draw.Draw(dst, bounds, image.NewUniform(image.Black), image.Point{}, draw.Src)
	// Composite source over black.
	draw.Draw(dst, bounds, img, bounds.Min, draw.Over)
	// Ensure alpha is 255 everywhere (black canvas already opaque, but Over
	// should produce 255 alpha; force it for safety).
	for i := 3; i < len(dst.Pix); i += 4 {
		dst.Pix[i] = 255
	}
	return dst, nil
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

// scaledNext returns next scaled to prev's dimensions via nearest-neighbor
// if dimensions differ; otherwise returns next unchanged.
func scaledNext(prev, next *image.NRGBA) *image.NRGBA {
	pb := prev.Bounds()
	nb := next.Bounds()
	if pb.Dx() == nb.Dx() && pb.Dy() == nb.Dy() && pb.Min == (image.Point{}) && nb.Min == (image.Point{}) {
		// Same dims; check also bounds equality.
		return next
	}
	if pb.Dx() == nb.Dx() && pb.Dy() == nb.Dy() {
		return next
	}
	return scaleNearestNeighbor(next, pb.Dx(), pb.Dy())
}

func scaleNearestNeighbor(src *image.NRGBA, w, h int) *image.NRGBA {
	sb := src.Bounds()
	sw := sb.Dx()
	sh := sb.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if sw == 0 || sh == 0 || w == 0 || h == 0 {
		return dst
	}
	for y := 0; y < h; y++ {
		sy := y * sh / h
		for x := 0; x < w; x++ {
			sx := x * sw / w
			si := src.PixOffset(sb.Min.X+sx, sb.Min.Y+sy)
			di := dst.PixOffset(x, y)
			dst.Pix[di+0] = src.Pix[si+0]
			dst.Pix[di+1] = src.Pix[si+1]
			dst.Pix[di+2] = src.Pix[si+2]
			dst.Pix[di+3] = src.Pix[si+3]
		}
	}
	return dst
}

// BlendFade performs per-pixel linear interpolation of RGB channels.
// Alpha is always 255.
func BlendFade(prev, next *image.NRGBA, t float64) *image.NRGBA {
	t = clamp01(t)
	nxt := scaledNext(prev, next)
	b := prev.Bounds()
	dst := image.NewNRGBA(b)
	// Copy structure; Pix already allocated with correct size by NewNRGBA.
	for i := 0; i < len(prev.Pix); i += 4 {
		r0 := float64(prev.Pix[i+0])
		g0 := float64(prev.Pix[i+1])
		b0 := float64(prev.Pix[i+2])
		r1 := float64(nxt.Pix[i+0])
		g1 := float64(nxt.Pix[i+1])
		b1 := float64(nxt.Pix[i+2])
		dst.Pix[i+0] = uint8(r0*(1-t) + r1*t + 0.5)
		dst.Pix[i+1] = uint8(g0*(1-t) + g1*t + 0.5)
		dst.Pix[i+2] = uint8(b0*(1-t) + b1*t + 0.5)
		dst.Pix[i+3] = 255
	}
	return dst
}

// BlendWipe performs a column sweep hard edge: x < t*width → next pixel, else prev.
func BlendWipe(prev, next *image.NRGBA, t float64) *image.NRGBA {
	t = clamp01(t)
	nxt := scaledNext(prev, next)
	b := prev.Bounds()
	w := b.Dx()
	h := b.Dy()
	cutoff := int(t * float64(w))
	dst := image.NewNRGBA(b)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			di := dst.PixOffset(x, y)
			var src *image.NRGBA
			if x < cutoff {
				src = nxt
			} else {
				src = prev
			}
			si := src.PixOffset(x, y)
			dst.Pix[di+0] = src.Pix[si+0]
			dst.Pix[di+1] = src.Pix[si+1]
			dst.Pix[di+2] = src.Pix[si+2]
			dst.Pix[di+3] = 255
		}
	}
	return dst
}

// permutation returns a deterministic Fisher-Yates permutation of 0..n-1
// seeded from (w,h).
func permutation(w, h int) []int {
	n := w * h
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	seed := int64(w)<<32 | int64(h)
	r := rand.New(rand.NewSource(seed))
	for i := n - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		perm[i], perm[j] = perm[j], perm[i]
	}
	return perm
}

// BlendDissolve reveals floor(t*N) pixels from next in permutation order, rest from prev.
func BlendDissolve(prev, next *image.NRGBA, t float64) *image.NRGBA {
	t = clamp01(t)
	nxt := scaledNext(prev, next)
	b := prev.Bounds()
	w := b.Dx()
	h := b.Dy()
	n := w * h
	k := int(t * float64(n)) // floor

	dst := image.NewNRGBA(b)
	// Start with prev pixels.
	copy(dst.Pix, prev.Pix)
	for i := 3; i < len(dst.Pix); i += 4 {
		dst.Pix[i] = 255
	}
	if k == 0 {
		return dst
	}
	perm := permutation(w, h)
	// Reveal first k indices.
	for i := 0; i < k; i++ {
		idx := perm[i]
		// idx corresponds to pixel position (y*w+x) assuming bounds origin 0,0.
		// Use offset calculation directly.
		// But use coordinate-based offset for correctness with bounds.
		x := idx % w
		y := idx / w
		di := dst.PixOffset(x, y)
		si := nxt.PixOffset(x, y)
		dst.Pix[di+0] = nxt.Pix[si+0]
		dst.Pix[di+1] = nxt.Pix[si+1]
		dst.Pix[di+2] = nxt.Pix[si+2]
		dst.Pix[di+3] = 255
	}
	if k == n {
		// Ensure fully next (already handled but guarantee byte equality).
		copy(dst.Pix, nxt.Pix)
		for i := 3; i < len(dst.Pix); i += 4 {
			dst.Pix[i] = 255
		}
	}
	return dst
}
