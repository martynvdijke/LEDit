package handlers

import (
	"strings"

	"ledit/ent"
	"ledit/render"
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
	return d != nil && (d.OverlayEnabled || d.OverlayText != "")
}

func isGroupOverlaySet(g *ent.DeviceGroup) bool {
	return g != nil && (g.OverlayEnabled || g.OverlayText != "")
}

func overlaySpecForGroup(g *ent.DeviceGroup) render.OverlaySpec {
	return render.OverlaySpec{
		Enabled:    g.OverlayEnabled,
		Position:   g.OverlayPosition,
		Height:     g.OverlayHeight,
		Text:       g.OverlayText,
		SpeedPx:    g.OverlaySpeedPx,
		Background: g.OverlayBg,
		Foreground: g.OverlayFg,
	}
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
