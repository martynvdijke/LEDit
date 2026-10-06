package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"

	"ledit/ent"
	"ledit/ent/playlist"
	"ledit/ent/scene"
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

// haDeviceBlock builds the shared Home Assistant device registry block.
func haDeviceBlock(d *ent.DeviceSettings) map[string]any {
	return map[string]any{
		"identifiers":  []string{fmt.Sprintf("ledit_%d", d.ID)},
		"name":         d.Name,
		"manufacturer": "LEDit",
		"model":        fmt.Sprintf("%dx%d %s", d.Width, d.Height, d.Transport),
		"sw_version":   d.FirmwareVersion,
	}
}

// haAvailTopic is the per-device availability topic shared by all entities.
func haAvailTopic(id int) string {
	return fmt.Sprintf("ledit/device/%d/online", id)
}

// haSelectPayload builds the content select entity config. Kept separate so
// playlist/scene changes can republish just this entity.
func haSelectPayload(s *Server, d *ent.DeviceSettings) map[string]any {
	return map[string]any{
		"name":                  fmt.Sprintf("LEDit %d content", d.ID),
		"unique_id":             fmt.Sprintf("ledit_%d_content", d.ID),
		"command_topic":         fmt.Sprintf("ledit/device/%d/select/set", d.ID),
		"state_topic":           fmt.Sprintf("ledit/device/%d/select/state", d.ID),
		"options":               buildHASelectOptions(s, d.ID),
		"device":                haDeviceBlock(d),
		"availability_topic":    haAvailTopic(d.ID),
		"payload_available":     "true",
		"payload_not_available": "false",
	}
}

func publishHASelectConfig(s *Server, d *ent.DeviceSettings) {
	if d == nil {
		return
	}
	publishDiscovery("homeassistant", "select", d.ID, "content", haSelectPayload(s, d))
}

// republishHASelectsForAll refreshes the content select options after playlist
// or scene configuration changes. No-op when discovery is disabled.
func republishHASelectsForAll(s *Server) {
	if s == nil || s.DB == nil || !haDiscoveryEnabled(s) {
		return
	}
	devs, err := s.DB.DeviceSettings.Query().All(s.Ctx)
	if err != nil {
		return
	}
	for _, d := range devs {
		publishHASelectConfig(s, d)
	}
}

func publishHADiscoveryForDevice(s *Server, d *ent.DeviceSettings) {
	if d == nil {
		return
	}
	id := d.ID
	prefix := "homeassistant"
	deviceBlock := haDeviceBlock(d)
	availTopic := haAvailTopic(id)

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
	// light brightness (additive; the number entity above is kept unchanged
	// for backwards compatibility). Template schema so off/on map to 0/100 on
	// the shared brightness command topic.
	publishDiscovery(prefix, "light", id, "light", map[string]any{
		"name":                        fmt.Sprintf("LEDit %d light", id),
		"unique_id":                   fmt.Sprintf("ledit_%d_light", id),
		"schema":                      "template",
		"state_topic":                 fmt.Sprintf("ledit/device/%d/brightness/state", id),
		"state_template":              "{{ 'on' if value | int(0) > 0 else 'off' }}",
		"command_topic":               fmt.Sprintf("ledit/device/%d/brightness/set", id),
		"command_on_template":         "100",
		"command_off_template":        "0",
		"brightness_template":         "{{ value }}",
		"brightness_command_topic":    fmt.Sprintf("ledit/device/%d/brightness/set", id),
		"brightness_command_template": "{{ brightness }}",
		"brightness_scale":            100,
		"device":                      deviceBlock,
		"availability_topic":          availTopic,
		"payload_available":           "true",
		"payload_not_available":       "false",
	})
	// select content: rotation sources + enabled playlists + enabled scenes
	publishDiscovery(prefix, "select", id, "content", haSelectPayload(s, d))
	// text message: renders plain text on this device only
	publishDiscovery(prefix, "text", id, "message", map[string]any{
		"name":                  fmt.Sprintf("LEDit %d message", id),
		"unique_id":             fmt.Sprintf("ledit_%d_message", id),
		"command_topic":         fmt.Sprintf("ledit/device/%d/message/set", id),
		"state_topic":           fmt.Sprintf("ledit/device/%d/message/state", id),
		"max":                   255,
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
	// Paused state comes from the live controller; when no controller is
	// connected, leave the retained value untouched instead of guessing.
	if fc, ok := getDeviceFeed(id); ok {
		forcePublishHAState(fmt.Sprintf("ledit/device/%d/paused", id), strconv.FormatBool(fc.IsPaused()))
	}
	// Last applied select option, derived from persisted content config or a
	// live source pin.
	if sel := currentHASelectState(d); sel != "" {
		forcePublishHAState(fmt.Sprintf("ledit/device/%d/select/state", id), sel)
	}
	// Message state is republished only while a targeted message is in flight.
	if txt, ok := deviceMessageText(id); ok {
		forcePublishHAState(fmt.Sprintf("ledit/device/%d/message/state", id), txt)
	}
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
	// A (re)connect republishes current state even if the broker lost its
	// retained store; clearing the dedupe cache lets event publishes through.
	resetHAStateCache()
	publishHAGlobalDiscovery()
	devs, err := s.DB.DeviceSettings.Query().All(s.Ctx)
	if err != nil {
		return
	}
	for _, d := range devs {
		publishHADiscoveryForDevice(s, d)
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
		fmt.Sprintf("homeassistant/light/ledit_%d_light/config", deviceID),
		fmt.Sprintf("homeassistant/select/ledit_%d_content/config", deviceID),
		fmt.Sprintf("homeassistant/text/ledit_%d_message/config", deviceID),
	}
	for _, t := range topics {
		forcePublishHAState(t, "")
	}
	// clear state topics
	forcePublishHAState(fmt.Sprintf("ledit/device/%d/brightness/state", deviceID), "")
	forcePublishHAState(fmt.Sprintf("ledit/device/%d/firmware_version", deviceID), "")
	forcePublishHAState(fmt.Sprintf("ledit/device/%d/transport", deviceID), "")
	forcePublishHAState(fmt.Sprintf("ledit/device/%d/select/state", deviceID), "")
	forcePublishHAState(fmt.Sprintf("ledit/device/%d/message/state", deviceID), "")
	// Drop cached values so a recreated device with the same id publishes fresh.
	forgetHAStateForDevice(deviceID)
}

// buildHASelectOptions returns the stable machine-readable option values for
// the per-device content select entity: one "source:<type>:<id>" per rotation
// source on the live controller, then "playlist:<id>" and "scene:<id>" for
// enabled configuration. Values are deduped and order-stable.
func buildHASelectOptions(s *Server, deviceID int) []string {
	opts := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		opts = append(opts, v)
	}
	if fc, ok := getDeviceFeed(deviceID); ok {
		for _, key := range fc.RotationKeys() {
			add("source:" + key)
		}
	}
	if s != nil && s.DB != nil {
		if pls, err := s.DB.Playlist.Query().Where(playlist.EnabledEQ(true)).Order(ent.Asc(playlist.FieldID)).All(s.Ctx); err == nil {
			for _, p := range pls {
				add(fmt.Sprintf("playlist:%d", p.ID))
			}
		}
		if scs, err := s.DB.Scene.Query().Where(scene.EnabledEQ(true)).Order(ent.Asc(scene.FieldID)).All(s.Ctx); err == nil {
			for _, sc := range scs {
				add(fmt.Sprintf("scene:%d", sc.ID))
			}
		}
	}
	return opts
}

// currentHASelectState derives the last-applied select option from persisted
// content configuration, falling back to a live source pin.
func currentHASelectState(d *ent.DeviceSettings) string {
	if d == nil {
		return ""
	}
	if d.ContentMode == "playlist" && d.PlaylistID != nil && *d.PlaylistID > 0 {
		return fmt.Sprintf("playlist:%d", *d.PlaylistID)
	}
	if fc, ok := getDeviceFeed(d.ID); ok {
		if key, _, pinned := fc.IsPinned(); pinned && key != "" {
			return "source:" + key
		}
	}
	return ""
}
