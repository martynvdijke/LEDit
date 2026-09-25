package handlers

import (
	"context"
	"testing"

	"ledit/ent"
)

func TestGroupPolicy_BrightnessDeviceWins(t *testing.T) {
	_, client, _ := newPrecHub(t)
	ctx := context.Background()
	grp := client.DeviceGroup.Create().SetName("GB").SetBrightnessEnabled(true).SetBrightnessSchedules(`[{"days":[1],"start":"07:00","end":"09:00","level":20}]`).SaveX(ctx)
	ov := 50
	dev := client.DeviceSettings.Create().SetName("D1").SetToken("t1").SetEnabled(true).SetBrightnessEnabled(true).SetBrightnessSchedules(`[{"days":[1],"start":"07:00","end":"09:00","level":90}]`).SetBrightnessOverride(ov).SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var want *ent.DeviceSettings
	for _, x := range devWithGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	enabled, sched, override, _ := effectiveBrightnessConfig(want)
	if !enabled || sched != `[{"days":[1],"start":"07:00","end":"09:00","level":90}]` || override == nil || *override != 50 {
		t.Fatalf("device wins: enabled=%v sched=%q override=%v", enabled, sched, override)
	}
}

func TestGroupPolicy_BrightnessInheritsGroup(t *testing.T) {
	_, client, _ := newPrecHub(t)
	ctx := context.Background()
	sensor := `{"entity_id":"sensor.lux","lux_levels":[{"maxLux":100,"level":30}]}`
	grp := client.DeviceGroup.Create().SetName("GB2").SetBrightnessEnabled(true).SetBrightnessSchedules(`[{"days":[1],"start":"07:00","end":"09:00","level":25}]`).SetBrightnessSensorConfig(sensor).SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D2").SetToken("t2").SetEnabled(true).SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var want *ent.DeviceSettings
	for _, x := range devWithGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	enabled, sched, _, sensorCfg := effectiveBrightnessConfig(want)
	if !enabled || sched != `[{"days":[1],"start":"07:00","end":"09:00","level":25}]` || sensorCfg == nil || *sensorCfg != sensor {
		t.Fatalf("inherit group brightness failed: enabled=%v sched=%q sensor=%v", enabled, sched, sensorCfg)
	}
}

func TestGroupPolicy_BrightnessEmptyGroupFallback(t *testing.T) {
	_, client, _ := newPrecHub(t)
	ctx := context.Background()
	grp := client.DeviceGroup.Create().SetName("GemptyB").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D3").SetToken("t3").SetEnabled(true).SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var want *ent.DeviceSettings
	for _, x := range devWithGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	enabled, _, override, sensorCfg := effectiveBrightnessConfig(want)
	if enabled || override != nil || sensorCfg != nil {
		t.Fatalf("empty group fallback should be disabled, got enabled=%v override=%v sensor=%v", enabled, override, sensorCfg)
	}
}

func TestGroupPolicy_BrightnessDanglingGroupNoPanic(t *testing.T) {
	_, client, _ := newPrecHub(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	dev := client.DeviceSettings.Create().SetName("D4").SetToken("t4").SetEnabled(true).SaveX(context.Background())
	devNoGroup, _ := client.DeviceSettings.Query().WithGroup().All(context.Background())
	var want *ent.DeviceSettings
	for _, x := range devNoGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	_, _, _, _ = effectiveBrightnessConfig(want)
}

func TestGroupPolicy_OverlayDeviceWins(t *testing.T) {
	_, client, _ := newPrecHub(t)
	ctx := context.Background()
	grp := client.DeviceGroup.Create().SetName("GO1").SetOverlayEnabled(true).SetOverlayText("group text").SetOverlayBg("#000000").SetOverlayFg("#ffffff").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D5").SetToken("t5").SetEnabled(true).SetOverlayEnabled(true).SetOverlayText("device text").SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var want *ent.DeviceSettings
	for _, x := range devWithGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	spec := overlaySpecForDeviceWithGroup(want)
	if spec.Text != "device text" {
		t.Fatalf("device overlay wins: got %q", spec.Text)
	}
}

func TestGroupPolicy_OverlayInheritsGroup(t *testing.T) {
	_, client, _ := newPrecHub(t)
	ctx := context.Background()
	grp := client.DeviceGroup.Create().SetName("GO2").SetOverlayEnabled(true).SetOverlayText("hello group").SetOverlayBg("#000000").SetOverlayFg("#ffffff").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D6").SetToken("t6").SetEnabled(true).SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var want *ent.DeviceSettings
	for _, x := range devWithGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	spec := overlaySpecForDeviceWithGroup(want)
	if spec.Text != "hello group" || !spec.Enabled {
		t.Fatalf("inherit group overlay: %+v", spec)
	}
}

func TestGroupPolicy_OverlayEmptyGroupFallback(t *testing.T) {
	_, client, _ := newPrecHub(t)
	ctx := context.Background()
	grp := client.DeviceGroup.Create().SetName("GOempty").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D7").SetToken("t7").SetEnabled(true).SetOverlayText("").SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var want *ent.DeviceSettings
	for _, x := range devWithGroup {
		if x.ID == dev.ID {
			want = x
			break
		}
	}
	spec := overlaySpecForDeviceWithGroup(want)
	if spec.Enabled || spec.Text != "" {
		t.Fatalf("empty group fallback should be disabled empty, got %+v", spec)
	}
	_ = grp
}

func TestGroupPolicy_NilAndWhitespaceSafe(t *testing.T) {
	enabled, sched, override, sensor := effectiveBrightnessConfig(nil)
	if enabled || sched != "[]" || override != nil || sensor != nil {
		t.Fatalf("nil device should be inert: %v %q %v %v", enabled, sched, override, sensor)
	}
	if isDeviceBrightnessExplicit(nil) || isGroupBrightnessSet(nil) {
		t.Fatal("nil explicit checks should be false")
	}
	if spec := overlaySpecForDeviceWithGroup(nil); spec.Enabled || spec.Text != "" {
		t.Fatalf("nil overlay spec should be empty, got %+v", spec)
	}
	// Whitespace-padded empty JSON must not count as explicit config.
	d := &ent.DeviceSettings{BrightnessSchedules: "[ ]"}
	if isDeviceBrightnessExplicit(d) {
		t.Fatal("[ ] must be treated as empty schedules")
	}
}
