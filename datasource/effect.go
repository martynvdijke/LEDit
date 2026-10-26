package datasource

import (
	"bytes"
	"image"
	"image/png"
	"time"

	"ledit/render"
	"ledit/render/effects"
)

// EffectDS renders a procedural effect preset from the effects registry. The
// preset contributes behaviour (effect name + params); the palette is mapped in
// from the effective theme at render time, so one preset looks right on every
// surface and follows theme changes without touching stored data.
type EffectDS struct {
	// PresetID is the effect_preset row id (0 for unsaved/adhoc presets such as
	// the admin live preview).
	PresetID int
	// PresetName is the human label used for source names.
	PresetName string
	// Effect is the registry effect name ("plasma", "legacy-dvd", ...).
	Effect string
	// Params are the preset's behaviour parameters; Palette is ignored and
	// replaced by the theme palette.
	Params effects.Params

	startTime time.Time
	renderer  *effects.Renderer
}

// EffectSourceName is the label shown in pickers and frames for a preset.
func EffectSourceName(presetName, effectName string) string {
	if presetName != "" {
		return presetName
	}
	if effectName != "" {
		return "Effect: " + effectName
	}
	return "Effect"
}

// paletteForTheme maps the effective theme onto the effect palette roles.
func paletteForTheme(theme render.Theme) effects.Palette {
	return effects.PaletteFromRGB(theme.BackgroundColor, theme.AccentColor, theme.TextColor)
}

// paramsFor returns the params with the theme palette applied.
func (e *EffectDS) paramsFor(theme render.Theme) effects.Params {
	p := e.Params.Clone()
	p.Palette = paletteForTheme(theme)
	return p
}

func (e *EffectDS) rendererFor() (*effects.Renderer, bool) {
	if e.renderer != nil {
		return e.renderer, true
	}
	r, ok := effects.NewRendererFor(e.Effect)
	if !ok {
		return nil, false
	}
	e.renderer = r
	return r, true
}

func (e *EffectDS) elapsed() time.Duration {
	if e.startTime.IsZero() {
		return 0
	}
	return time.Since(e.startTime)
}

// render encodes the effect frame at the requested size using the given theme.
func (e *EffectDS) render(width, height int, theme render.Theme) (*render.RenderedImage, error) {
	r, ok := e.rendererFor()
	if !ok {
		return nil, ErrEffectUnknown
	}
	if width <= 0 {
		width = 64
	}
	if height <= 0 {
		height = 64
	}
	img := r.Frame(width, height, e.paramsFor(theme), e.elapsed())
	if img == nil {
		return nil, ErrEffectUnknown
	}
	return encodeEffectPNG(img)
}

// ErrEffectUnknown is returned when a preset names an effect that is not
// registered (for example a legacy adapter whose package is not linked in).
var ErrEffectUnknown = errEffectUnknown{}

type errEffectUnknown struct{}

func (errEffectUnknown) Error() string { return "unknown effect" }

func encodeEffectPNG(img image.Image) (*render.RenderedImage, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

// GetPNG renders with the default theme. The animated feed branch calls GetPNG
// directly, so this must return a fully themed frame on its own.
func (e *EffectDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	return e.render(width, height, DefaultTheme())
}

// GetPNGThemed renders with a caller-supplied theme.
func (e *EffectDS) GetPNGThemed(width, height int, theme render.Theme) (*render.RenderedImage, error) {
	return e.render(width, height, theme)
}

// Animator: effects animate on the shared 66ms (~15fps) tick.
func (e *EffectDS) FrameCount() int { return 1000000 }

func (e *EffectDS) NextFrame(now time.Time) int {
	if e.startTime.IsZero() {
		e.startTime = now
		return 0
	}
	return int(now.Sub(e.startTime).Milliseconds() / 66)
}

// Ambienter: effects are ambient content and bypass the per-source TTL cache.
func (e *EffectDS) Ambient() bool { return true }

// EffectKind reports whether a datasource is an effect preset source.
func EffectKind(ds Datasource) (*EffectDS, bool) {
	e, ok := ds.(*EffectDS)
	return e, ok
}
