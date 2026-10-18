// Package legacy exposes the pre-existing screensaver and matrix-rain renderers
// as effect-engine adapters. Each adapter delegates to the original code path
// and is byte-identical to it for the same elapsed time and dimensions, so
// adoption of the effects library is opt-in and never changes existing output.
package legacy

import (
	"bytes"
	"image"
	"image/png"
	"time"

	"ledit/render"
	"ledit/render/effects"
	"ledit/render/screensaver"
)

func init() {
	base := effects.Params{Speed: 50, Intensity: 50, Density: 50, Direction: effects.DirectionNone}
	bounds := effects.DefaultBounds()
	register("legacy-matrix-rain", base, bounds, func(dst *image.RGBA, elapsed time.Duration) {
		img, err := render.RenderMatrixRain(legacyTime(elapsed), dst.Rect.Dx(), dst.Rect.Dy())
		if err != nil {
			return
		}
		decoded, derr := png.Decode(bytes.NewReader(img.Data))
		if derr != nil {
			return
		}
		blit(dst, decoded)
	})
	register("legacy-plasma", base, bounds, func(dst *image.RGBA, elapsed time.Duration) {
		blit(dst, screensaver.DrawPlasma(dst.Rect.Dx(), dst.Rect.Dy(), elapsed))
	})
	register("legacy-starfield", base, bounds, func(dst *image.RGBA, elapsed time.Duration) {
		blit(dst, screensaver.DrawStarfield(dst.Rect.Dx(), dst.Rect.Dy(), elapsed))
	})
	register("legacy-dvd", base, bounds, func(dst *image.RGBA, elapsed time.Duration) {
		blit(dst, screensaver.DrawDVD(dst.Rect.Dx(), dst.Rect.Dy(), elapsed))
	})
}

// legacyTime converts an elapsed duration into the wall-clock time base used by
// the legacy matrix-rain renderer, keeping the adapter a pure function of
// elapsed.
func legacyTime(elapsed time.Duration) time.Time {
	return time.UnixMilli(elapsed.Milliseconds())
}

type adapter struct {
	name string
	draw func(dst *image.RGBA, elapsed time.Duration)
}

func (a adapter) Name() string { return a.name }

func (a adapter) Draw(dst *image.RGBA, _ effects.Params, elapsed time.Duration) {
	if dst == nil || dst.Rect.Dx() <= 0 || dst.Rect.Dy() <= 0 {
		return
	}
	a.draw(dst, elapsed)
}

func register(name string, defaults effects.Params, bounds effects.ParamsBounds, draw func(dst *image.RGBA, elapsed time.Duration)) {
	effects.Register(effects.Descriptor{Name: name, Defaults: defaults, Bounds: bounds}, adapter{name: name, draw: draw})
}

// blit copies src into dst, clipping to the destination bounds.
func blit(dst *image.RGBA, src image.Image) {
	if src == nil {
		return
	}
	b := dst.Bounds().Intersect(src.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x, y, src.At(x, y))
		}
	}
}
