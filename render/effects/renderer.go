package effects

import (
	"image"
	"time"
)

// QuantizeTick folds an elapsed duration onto the shared animation quantum.
func QuantizeTick(elapsed time.Duration) time.Duration {
	if elapsed < 0 {
		return 0
	}
	return elapsed / TickInterval * TickInterval
}

// TickIndex returns the integer tick number for an elapsed duration.
func TickIndex(elapsed time.Duration) int64 {
	if elapsed < 0 {
		return 0
	}
	return int64(elapsed / TickInterval)
}

// Renderer renders one effect repeatedly, reusing its scratch buffer so
// steady-state rendering performs no per-frame scratch allocation.
//
// A renderer is not safe for concurrent use by multiple goroutines, and the
// frame returned by Frame stays valid only until the next Frame call on the
// same renderer; callers that need to keep a frame must copy it first. Each
// EffectDS and overlay composition owns its own renderer, so independent
// renderers never share mutable state.
type Renderer struct {
	eff Effect
	img *image.RGBA
}

// NewRenderer wraps an effect in a buffer-reusing renderer.
func NewRenderer(eff Effect) *Renderer { return &Renderer{eff: eff} }

// NewRendererFor looks up a registered effect by name.
func NewRendererFor(name string) (*Renderer, bool) {
	_, eff, ok := Lookup(name)
	if !ok {
		return nil, false
	}
	return &Renderer{eff: eff}, true
}

// Effect returns the wrapped effect.
func (r *Renderer) Effect() Effect {
	if r == nil {
		return nil
	}
	return r.eff
}

// Name returns the wrapped effect name, or "" for a nil renderer.
func (r *Renderer) Name() string {
	if r == nil || r.eff == nil {
		return ""
	}
	return r.eff.Name()
}

// Frame renders the effect at the requested size for the given parameters and
// elapsed animation time. Time is quantized to the tick quantum and the
// parameters are defensively clamped into the declared bounds, so an
// out-of-range document still produces a bounded frame instead of an error.
func (r *Renderer) Frame(width, height int, p Params, elapsed time.Duration) *image.RGBA {
	if r == nil || r.eff == nil {
		return nil
	}
	w, h := width, height
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	p = WithDefaults(r.eff.Name(), Clamp(r.eff.Name(), p))
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	r.eff.Draw(r.img, p, QuantizeTick(elapsed))
	return r.img
}

// Scratch reports the current scratch buffer bounds, if allocated. It is used
// by tests to prove the buffer is reused across frames.
func (r *Renderer) Scratch() *image.RGBA {
	if r == nil {
		return nil
	}
	return r.img
}
