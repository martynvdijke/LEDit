package effects

import (
	"fmt"
	"image"
	"sort"
	"sync"
	"time"
)

// ParamsBounds declares the accepted range for each parameter field.
type ParamsBounds struct {
	Speed      [2]float64
	Intensity  [2]float64
	Density    [2]float64
	Directions []string
}

// DefaultBounds is the bounds shared by every built-in effect: percentages in
// 0..100 and the full closed direction set.
func DefaultBounds() ParamsBounds {
	return ParamsBounds{
		Speed:      [2]float64{0, 100},
		Intensity:  [2]float64{0, 100},
		Density:    [2]float64{0, 100},
		Directions: ValidDirections(),
	}
}

// Descriptor describes one registered effect: its stable name, the parameter
// defaults used when a preset does not specify a field, and the declared
// bounds used for validation and defensive clamping.
type Descriptor struct {
	Name     string
	Defaults Params
	Bounds   ParamsBounds
}

// Effect renders one frame for the given parameters at the elapsed animation
// time. Draw must be a pure function of its arguments and must stay inside
// dst's bounds.
type Effect interface {
	Name() string
	Draw(dst *image.RGBA, p Params, elapsed time.Duration)
}

type registryEntry struct {
	desc Descriptor
	eff  Effect
}

var (
	registryMu sync.RWMutex
	registry   = map[string]registryEntry{}
)

// Register adds an effect to the global registry. Registration is intended to
// happen from package init and panics on programmer errors (empty or duplicate
// names, or a mismatch between the descriptor and the effect name).
func Register(d Descriptor, e Effect) {
	if e == nil {
		panic("effects: Register called with a nil effect")
	}
	if d.Name == "" {
		panic("effects: Register called with an empty name")
	}
	if e.Name() != d.Name {
		panic(fmt.Sprintf("effects: effect name %q does not match descriptor %q", e.Name(), d.Name))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[d.Name]; ok {
		panic(fmt.Sprintf("effects: effect %q registered twice", d.Name))
	}
	registry[d.Name] = registryEntry{desc: d, eff: e}
}

// Lookup returns the descriptor and effect registered under name.
func Lookup(name string) (Descriptor, Effect, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	entry, ok := registry[name]
	if !ok {
		return Descriptor{}, nil, false
	}
	return entry.desc, entry.eff, true
}

// Has reports whether name is registered.
func Has(name string) bool {
	_, _, ok := Lookup(name)
	return ok
}

// Names returns the registered effect names in stable sorted order.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Descriptors returns every descriptor in stable name order.
func Descriptors() []Descriptor {
	names := Names()
	out := make([]Descriptor, 0, len(names))
	for _, name := range names {
		d, _, ok := Lookup(name)
		if ok {
			out = append(out, d)
		}
	}
	return out
}

// FieldError is a validation failure tied to one parameter field.
type FieldError struct {
	Field   string
	Message string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// FieldErrors is a collection of field-level validation failures.
type FieldErrors []FieldError

func (e FieldErrors) Error() string {
	if len(e) == 1 {
		return e[0].Error()
	}
	return fmt.Sprintf("%d effect parameter errors: %s", len(e), e[0].Error())
}

// FieldErrorsOf extracts field-level errors from any error returned by
// Validate. The boolean is false when err does not carry field information.
func FieldErrorsOf(err error) ([]FieldError, bool) {
	switch e := err.(type) {
	case nil:
		return nil, false
	case FieldError:
		return []FieldError{e}, true
	case *FieldError:
		return []FieldError{*e}, true
	case FieldErrors:
		return e, len(e) > 0
	case *FieldErrors:
		if e == nil || len(*e) == 0 {
			return nil, false
		}
		return *e, true
	default:
		return nil, false
	}
}

// FieldMap converts an error from Validate into a field -> message map.
func FieldMap(err error) map[string]string {
	fields, ok := FieldErrorsOf(err)
	if !ok {
		if err == nil {
			return nil
		}
		if fielded, fok := err.(FieldError); fok {
			return map[string]string{fielded.Field: fielded.Message}
		}
		return map[string]string{"effect": err.Error()}
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f.Field] = f.Message
	}
	return out
}

// Validate checks a parameter document against the registered effect and its
// declared bounds, returning nil, a single FieldError, or FieldErrors.
func Validate(name string, p Params) error {
	desc, _, ok := Lookup(name)
	if !ok {
		return FieldError{Field: "effect", Message: fmt.Sprintf("unknown effect %q", name)}
	}
	var errs FieldErrors
	if err := checkBound("speed", p.Speed, desc.Bounds.Speed); err != nil {
		errs = append(errs, *err)
	}
	if err := checkBound("intensity", p.Intensity, desc.Bounds.Intensity); err != nil {
		errs = append(errs, *err)
	}
	if err := checkBound("density", p.Density, desc.Bounds.Density); err != nil {
		errs = append(errs, *err)
	}
	if !directionAllowed(p.Direction, desc.Bounds.Directions) {
		errs = append(errs, FieldError{Field: "direction", Message: fmt.Sprintf("direction must be one of %s", joinWords(desc.Bounds.Directions))})
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errs
	}
}

// Clamp defensively folds a parameter document into the declared bounds for
// name. Unknown effects are returned unchanged because they cannot render.
func Clamp(name string, p Params) Params {
	desc, _, ok := Lookup(name)
	if !ok {
		return p
	}
	p.Speed = clampRange(p.Speed, desc.Bounds.Speed)
	p.Intensity = clampRange(p.Intensity, desc.Bounds.Intensity)
	p.Density = clampRange(p.Density, desc.Bounds.Density)
	if !directionAllowed(p.Direction, desc.Bounds.Directions) {
		p.Direction = desc.Defaults.Direction
	}
	return p
}

// WithDefaults fills unspecified fields from the descriptor defaults.
func WithDefaults(name string, p Params) Params {
	desc, _, ok := Lookup(name)
	if !ok {
		return p
	}
	if p.Direction == "" {
		p.Direction = desc.Defaults.Direction
	}
	return p
}

func checkBound(field string, value float64, bounds [2]float64) *FieldError {
	if value < bounds[0] || value > bounds[1] {
		return &FieldError{Field: field, Message: fmt.Sprintf("%s must be between %s and %s", field, trimFloat(bounds[0]), trimFloat(bounds[1]))}
	}
	return nil
}

func clampRange(v float64, bounds [2]float64) float64 {
	if v < bounds[0] {
		return bounds[0]
	}
	if v > bounds[1] {
		return bounds[1]
	}
	return v
}

func directionAllowed(dir string, allowed []string) bool {
	if dir == "" {
		return true
	}
	if len(allowed) == 0 {
		return DirectionValid(dir)
	}
	for _, d := range allowed {
		if d == dir {
			return true
		}
	}
	return false
}

func joinWords(values []string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func trimFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}
