package handlers

import (
	"strings"

	"ledit/ent"
	"ledit/render"
	"ledit/render/effects"
)

func isDeviceBrightnessExplicit(d *ent.DeviceSettings) bool {
	if d == nil {
		return false
	}
	if d.BrightnessEnabled {
		return true
	}
	if d.BrightnessOverride != nil {
		return true
	}
	if !emptyJSONList(d.BrightnessSchedules) {
		return true
	}
	if d.BrightnessSensorConfig != nil {
		return true
	}
	return false
}

// emptyJSONList reports whether a JSON list field is effectively empty ("" / "[]" / "[ ]").
func emptyJSONList(s string) bool {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "") == "[]"
}

func isGroupBrightnessSet(g *ent.DeviceGroup) bool {
	if g == nil {
		return false
	}
	if g.BrightnessEnabled {
		return true
	}
	if g.BrightnessOverride != nil {
		return true
	}
	if !emptyJSONList(g.BrightnessSchedules) {
		return true
	}
	if g.BrightnessSensorConfig != nil {
		return true
	}
	return false
}

func effectiveBrightnessConfig(d *ent.DeviceSettings) (bool, string, *int, *string) {
	if d == nil {
		return false, "[]", nil, nil
	}
	if isDeviceBrightnessExplicit(d) {
		return d.BrightnessEnabled, d.BrightnessSchedules, d.BrightnessOverride, d.BrightnessSensorConfig
	}
	if grp, err := d.Edges.GroupOrErr(); err == nil && grp != nil && isGroupBrightnessSet(grp) {
		return grp.BrightnessEnabled, grp.BrightnessSchedules, grp.BrightnessOverride, grp.BrightnessSensorConfig
	}
	return d.BrightnessEnabled, d.BrightnessSchedules, d.BrightnessOverride, d.BrightnessSensorConfig
}

func isDeviceOverlayExplicit(d *ent.DeviceSettings) bool {
	return d != nil && (d.OverlayEnabled || d.OverlayText != "" || d.OverlayEffectPresetID != nil)
}

func isGroupOverlaySet(g *ent.DeviceGroup) bool {
	return g != nil && (g.OverlayEnabled || g.OverlayText != "" || g.OverlayEffectPresetID != nil)
}

// overlayEffectFields resolves an effect preset reference into the render spec.
// A missing, deleted or malformed preset leaves the spec unchanged so the strip
// falls back to the solid background instead of breaking the frame.
func overlayEffectFields(spec render.OverlaySpec, presetID *int) render.OverlaySpec {
	if presetID == nil {
		return spec
	}
	p, ok := cachedEffectPresetByID(*presetID)
	if !ok || p.Effect == "" {
		return spec
	}
	if _, err := effects.UnmarshalParams([]byte(p.Params)); err != nil {
		return spec
	}
	spec.EffectName = p.Effect
	spec.EffectParams = p.Params
	return spec
}

// overlayEffectPresetID loads the preset id for a device (nil-safe).
func overlayEffectPresetID(d *ent.DeviceSettings) *int {
	if d == nil {
		return nil
	}
	return d.OverlayEffectPresetID
}

func overlaySpecForGroup(g *ent.DeviceGroup) render.OverlaySpec {
	spec := render.OverlaySpec{
		Enabled:    g.OverlayEnabled,
		Position:   g.OverlayPosition,
		Height:     g.OverlayHeight,
		Text:       g.OverlayText,
		SpeedPx:    g.OverlaySpeedPx,
		Background: g.OverlayBg,
		Foreground: g.OverlayFg,
	}
	return overlayEffectFields(spec, g.OverlayEffectPresetID)
}

func overlaySpecForDeviceWithGroup(d *ent.DeviceSettings) render.OverlaySpec {
	if d == nil {
		return render.DefaultOverlaySpec()
	}
	if isDeviceOverlayExplicit(d) {
		return overlaySpecForDevice(d)
	}
	if grp, err := d.Edges.GroupOrErr(); err == nil && grp != nil && isGroupOverlaySet(grp) {
		return overlaySpecForGroup(grp)
	}
	return overlaySpecForDevice(d)
}
