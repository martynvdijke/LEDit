package handlers

import (
	"encoding/json"
	"fmt"

	"ledit/ent"
)

// ponytail: brightness state reports override-or-100 without schedule/sensor resolution; discovery republished only on connect/device-save, not on every state change beyond brightness.

func haDiscoveryEnabled(s *Server) bool {
	if s == nil || s.DB == nil {
		return false
	}
	st := EnsureOutboundSettings(s.DB)
	if st == nil {
		return false
	}
	return st.HaDiscoveryEnabled
}

func publishHADiscoveryForDevice(d *ent.DeviceSettings) {
	if d == nil {
		return
	}
	id := d.ID
	prefix := "homeassistant"
	deviceBlock := map[string]any{
		"identifiers":  []string{fmt.Sprintf("ledit_%d", id)},
		"name":         d.Name,
		"manufacturer": "LEDit",
		"model":        fmt.Sprintf("%dx%d %s", d.Width, d.Height, d.Transport),
		"sw_version":   d.FirmwareVersion,
	}
	availTopic := fmt.Sprintf("ledit/device/%d/online", id)

	// binary_sensor online
	publishDiscovery(prefix, "binary_sensor", id, "online", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d online", id),
		"unique_id":             fmt.Sprintf("ledit_%d_online", id),
		"state_topic":           availTopic,
		"payload_on":            "true",
		"payload_off":           "false",
		"device":                deviceBlock,
		"availability_topic":    availTopic,
		"payload_available":     "true",
		"payload_not_available": "false",
	})
	// number brightness
	publishDiscovery(prefix, "number", id, "brightness", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d brightness", id),
		"unique_id":             fmt.Sprintf("ledit_%d_brightness", id),
		"state_topic":           fmt.Sprintf("ledit/device/%d/brightness/state", id),
		"command_topic":         fmt.Sprintf("ledit/device/%d/brightness/set", id),
		"min":                   0,
		"max":                   100,
		"step":                  1,
		"unit_of_measurement":   "%",
		"device":                deviceBlock,
		"availability_topic":    availTopic,
		"payload_available":     "true",
		"payload_not_available": "false",
	})
	// switch paused
	publishDiscovery(prefix, "switch", id, "paused", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d paused", id),
		"unique_id":             fmt.Sprintf("ledit_%d_paused", id),
		"state_topic":           fmt.Sprintf("ledit/device/%d/paused", id),
		"command_topic":         fmt.Sprintf("ledit/device/%d/paused/set", id),
		"payload_on":            "true",
		"payload_off":           "false",
		"state_on":              "true",
		"state_off":             "false",
		"device":                deviceBlock,
		"availability_topic":    availTopic,
		"payload_available":     "true",
		"payload_not_available": "false",
	})
	// button next
	publishDiscovery(prefix, "button", id, "next", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d next", id),
		"unique_id":             fmt.Sprintf("ledit_%d_next", id),
		"command_topic":         fmt.Sprintf("ledit/device/%d/next/set", id),
		"payload_press":         "PRESS",
		"device":                deviceBlock,
		"availability_topic":    availTopic,
		"payload_available":     "true",
		"payload_not_available": "false",
	})
	// sensor firmware
	publishDiscovery(prefix, "sensor", id, "firmware", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d firmware", id),
		"unique_id":             fmt.Sprintf("ledit_%d_firmware", id),
		"state_topic":           fmt.Sprintf("ledit/device/%d/firmware_version", id),
		"device":                deviceBlock,
		"availability_topic":    availTopic,
		"payload_available":     "true",
		"payload_not_available": "false",
	})
	// sensor transport
	publishDiscovery(prefix, "sensor", id, "transport", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d transport", id),
		"unique_id":             fmt.Sprintf("ledit_%d_transport", id),
		"state_topic":           fmt.Sprintf("ledit/device/%d/transport", id),
		"device":                deviceBlock,
		"availability_topic":    availTopic,
		"payload_available":     "true",
		"payload_not_available": "false",
	})

	publishHAStateForDevice(d)
}

func publishHAStateForDevice(d *ent.DeviceSettings) {
	if d == nil {
		return
	}
	id := d.ID
	brightness := 100
	if d.BrightnessOverride != nil {
		brightness = *d.BrightnessOverride
	}
	PublishOutbound(fmt.Sprintf("ledit/device/%d/brightness/state", id), fmt.Sprintf("%d", brightness), true)
	PublishOutbound(fmt.Sprintf("ledit/device/%d/firmware_version", id), d.FirmwareVersion, true)
	PublishOutbound(fmt.Sprintf("ledit/device/%d/transport", id), d.Transport, true)
}

func publishHAGlobalDiscovery() {
	publishGlobalConfigs()
}

func publishGlobalConfigs() {
	deviceBlock := map[string]any{
		"identifiers":  []string{"ledit_global"},
		"name":         "LEDit",
		"manufacturer": "LEDit",
		"model":        "LEDit",
	}
	// sensor current_source
	payload, _ := json.Marshal(map[string]any{
		"name":        "LEDit current source",
		"unique_id":   "ledit_current_source",
		"state_topic": "ledit/status/current_source",
		"device":      deviceBlock,
	})
	PublishOutbound("homeassistant/sensor/ledit_current_source/config", string(payload), true)

	payload, _ = json.Marshal(map[string]any{
		"name":          "LEDit paused",
		"unique_id":     "ledit_paused",
		"state_topic":   "ledit/status/paused",
		"command_topic": "ledit/control",
		"payload_on":    "pause",
		"payload_off":   "resume",
		"state_on":      "true",
		"state_off":     "false",
		"device":        deviceBlock,
	})
	PublishOutbound("homeassistant/switch/ledit_paused/config", string(payload), true)

	payload, _ = json.Marshal(map[string]any{
		"name":          "LEDit next",
		"unique_id":     "ledit_next",
		"command_topic": "ledit/control",
		"payload_press": "next",
		"device":        deviceBlock,
	})
	PublishOutbound("homeassistant/button/ledit_next/config", string(payload), true)
}

func publishDiscovery(prefix, component string, id int, slug string, payload map[string]any) {
	topic := fmt.Sprintf("%s/%s/ledit_%d_%s/config", prefix, component, id, slug)
	// For global we handle separately; this helper is for per-device.
	b, _ := json.Marshal(payload)
	PublishOutbound(topic, string(b), true)
}

// PublishHADiscoveryForAll publishes global + every device if enabled.
func PublishHADiscoveryForAll(s *Server) {
	if s == nil || s.DB == nil {
		return
	}
	if !haDiscoveryEnabled(s) {
		return
	}
	publishHAGlobalDiscovery()
	devs, err := s.DB.DeviceSettings.Query().All(s.Ctx)
	if err != nil {
		return
	}
	for _, d := range devs {
		publishHADiscoveryForDevice(d)
	}
}

func clearHADiscoveryForDevice(deviceID int) {
	topics := []string{
		fmt.Sprintf("homeassistant/binary_sensor/ledit_%d_online/config", deviceID),
		fmt.Sprintf("homeassistant/number/ledit_%d_brightness/config", deviceID),
		fmt.Sprintf("homeassistant/switch/ledit_%d_paused/config", deviceID),
		fmt.Sprintf("homeassistant/button/ledit_%d_next/config", deviceID),
		fmt.Sprintf("homeassistant/sensor/ledit_%d_firmware/config", deviceID),
		fmt.Sprintf("homeassistant/sensor/ledit_%d_transport/config", deviceID),
	}
	for _, t := range topics {
		PublishOutbound(t, "", true)
	}
	// clear state topics
	PublishOutbound(fmt.Sprintf("ledit/device/%d/brightness/state", deviceID), "", true)
	PublishOutbound(fmt.Sprintf("ledit/device/%d/firmware_version", deviceID), "", true)
	PublishOutbound(fmt.Sprintf("ledit/device/%d/transport", deviceID), "", true)
}
