package handlers

import (
	"testing"
	"time"
)

func TestSensorConfigSourceValidation(t *testing.T) {
	ha := &SensorConfig{EntityID: "sensor.lux", LuxLevels: []LuxLevel{{MaxLux: 100, Level: 50}}}
	if err := ValidateSensorConfig(ha); err != nil {
		t.Fatalf("legacy HA config should validate: %v", err)
	}
	dev := &SensorConfig{Source: "device", LuxLevels: []LuxLevel{{MaxLux: 100, Level: 50}}}
	if err := ValidateSensorConfig(dev); err != nil {
		t.Fatalf("device source config should validate without entity_id: %v", err)
	}
	bad := &SensorConfig{Source: "wifi", EntityID: "x", LuxLevels: []LuxLevel{{MaxLux: 100, Level: 50}}}
	if err := ValidateSensorConfig(bad); err == nil {
		t.Fatalf("unknown source must be rejected")
	}
	raw := `{"source":"device","lux_levels":[{"maxLux":100,"level":40}]}`
	cfg, err := ParseSensorConfig(&raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Source != "device" || len(cfg.LuxLevels) != 1 || cfg.LuxLevels[0].Level != 40 {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func convergePushLevel(pb *pushBrightness) int {
	var v int
	for i := 0; i < 15; i++ {
		// The ramp advances at most once per 3s; clear the throttle so each
		// call steps the ramp and the test converges without sleeping.
		pb.lastAdvance = time.Time{}
		v = pb.level(time.Now())
	}
	return v
}

func TestFreshDeviceLuxDrivesPushBrightness(t *testing.T) {
	const devID = 990001
	deviceLuxCache.Delete(devID)
	t.Cleanup(func() { deviceLuxCache.Delete(devID) })

	cfg := &SensorConfig{Source: "device", LuxLevels: []LuxLevel{{MaxLux: 50, Level: 10}, {MaxLux: 1000, Level: 80}}}
	pb := &pushBrightness{deviceID: devID, enabled: true, sensorCfg: cfg, ramp: NewBrightnessRamp(100), lastAdvance: time.Time{}}

	if got := convergePushLevel(pb); got != 100 {
		t.Fatalf("no reading should fall back to schedule/100, got %d", got)
	}
	recordDeviceLux(devID, 5)
	if got := convergePushLevel(pb); got != 10 {
		t.Fatalf("fresh low lux should map to 10, got %d", got)
	}
	recordDeviceLux(devID, 900)
	if got := convergePushLevel(pb); got != 80 {
		t.Fatalf("fresh high lux should map to 80, got %d", got)
	}
	deviceLuxCache.Store(devID, deviceLuxReading{lux: 5, at: time.Now().Add(-2 * time.Minute)})
	if got := convergePushLevel(pb); got != 100 {
		t.Fatalf("stale reading must fall back, got %d", got)
	}
}

func TestDeviceLuxRespectsManualHint(t *testing.T) {
	const devID = 990002
	deviceLuxCache.Delete(devID)
	t.Cleanup(func() {
		deviceLuxCache.Delete(devID)
		unregisterDeviceFeed(devID)
	})

	fc := &FeedController{DeviceID: devID}
	registerDeviceFeed(devID, fc)
	fc.SetBrightnessHint(33)
	recordDeviceLux(devID, 5)

	cfg := &SensorConfig{Source: "device", LuxLevels: []LuxLevel{{MaxLux: 50, Level: 10}}}
	pb := &pushBrightness{deviceID: devID, enabled: true, sensorCfg: cfg, ramp: NewBrightnessRamp(100), lastAdvance: time.Now()}
	if got := pb.level(time.Now()); got != 33 {
		t.Fatalf("manual hint must win over device lux, got %d", got)
	}
}

func TestDeviceInputStatusRecordedAndCleared(t *testing.T) {
	const devID = 990003
	clearDeviceInputStatus(devID)
	t.Cleanup(func() { clearDeviceInputStatus(devID) })

	recordDeviceInput(devID, InputEvent{Source: InputSourceLux, Event: InputEventLux})
	recordDeviceInput(devID, InputEvent{Source: InputSourceNFC, Event: InputEventTap})

	sources, last, seen, ok := DeviceInputStatus(devID)
	if !ok || len(sources) != 2 || sources[0] != "lux" || sources[1] != "nfc" {
		t.Fatalf("unexpected sources %v ok=%v", sources, ok)
	}
	if last != "nfc:tap" || seen.IsZero() {
		t.Fatalf("unexpected last=%q seen=%v", last, seen)
	}

	registerInputSink(devID, func(InputEvent) {})
	unregisterInputSink(devID)
	if _, _, _, ok := DeviceInputStatus(devID); ok {
		t.Fatalf("unregisterInputSink should clear input status")
	}
}
