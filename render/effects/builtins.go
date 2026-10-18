package effects

// Native built-in effects, registered at package init. The registry is the only
// way callers refer to an effect, and Names() therefore always returns a stable
// sorted list.
func init() {
	Register(Descriptor{
		Name:     "plasma",
		Defaults: defaultParams(DirectionNone, 50),
		Bounds:   DefaultBounds(),
	}, plasma{})
	Register(Descriptor{
		Name:     "fire",
		Defaults: defaultParams(DirectionUp, 50),
		Bounds:   DefaultBounds(),
	}, fire{})
	Register(Descriptor{
		Name:     "sparkle",
		Defaults: defaultParams(DirectionNone, 12),
		Bounds:   DefaultBounds(),
	}, sparkle{})
	Register(Descriptor{
		Name:     "scan",
		Defaults: defaultParams(DirectionLeft, 50),
		Bounds:   DefaultBounds(),
	}, scan{})
	Register(Descriptor{
		Name:     "noise",
		Defaults: defaultParams(DirectionNone, 50),
		Bounds:   DefaultBounds(),
	}, noise{})
	Register(Descriptor{
		Name:     "gradient",
		Defaults: defaultParams(DirectionNone, 50),
		Bounds:   DefaultBounds(),
	}, gradient{})
}

func defaultParams(direction string, density float64) Params {
	return Params{
		Speed:     50,
		Intensity: 50,
		Density:   density,
		Direction: direction,
	}
}
