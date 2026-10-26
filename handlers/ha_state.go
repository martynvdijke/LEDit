package handlers

import (
	"fmt"
	"strings"
	"sync"
)

// HAStateSink republishes Home Assistant entity state topics on bus events.
//
// It is deliberately independent of the generic outbound MQTT event toggle
// (outbound_settings.mqtt_publish_enabled): Home Assistant entity state is
// gated only by ha_discovery_enabled (synced into SetEnabled by the settings
// load/save paths) and a live MQTT connection. Publishes are deduplicated per
// topic/value so repeated events do not spam retained messages.
type HAStateSink struct {
	mu      sync.Mutex
	enabled bool
	last    map[string]string
}

var GlobalHAStateSink = NewHAStateSink()

func NewHAStateSink() *HAStateSink {
	return &HAStateSink{last: map[string]string{}}
}

func (h *HAStateSink) Name() string { return "ha_state" }

func (h *HAStateSink) Enabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled
}

func (h *HAStateSink) SetEnabled(v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = v
}

func (h *HAStateSink) Handle(e Event) error {
	if !h.Enabled() {
		return nil
	}
	switch e.Type {
	case EventDeviceLivenessChanged:
		data, ok := e.Data.(map[string]any)
		if !ok {
			return nil
		}
		id, ok := data["device_id"].(int)
		if !ok || id <= 0 {
			return nil
		}
		val := "false"
		if online, _ := data["online"].(bool); online {
			val = "true"
		}
		publishHAState(fmt.Sprintf("ledit/device/%d/online", id), val)
	case EventFeedPaused, EventFeedResumed:
		val := "false"
		if e.Type == EventFeedPaused {
			val = "true"
		}
		publishHAState("ledit/status/paused", val)
		if id := eventDeviceID(e); id > 0 {
			publishHAState(fmt.Sprintf("ledit/device/%d/paused", id), val)
		}
	case EventSourceChanged:
		if data, ok := e.Data.(map[string]string); ok {
			publishHAState("ledit/status/current_source", data["to"])
		}
	}
	return nil
}

func eventDeviceID(e Event) int {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return 0
	}
	id, _ := data["device_id"].(int)
	return id
}

// publishHAState publishes a retained HA state topic, skipping duplicates for
// the same topic/value. No-op when MQTT is disconnected.
func publishHAState(topic, payload string) {
	GlobalHAStateSink.publish(topic, payload)
}

func (h *HAStateSink) publish(topic, payload string) {
	if !mqttConnected() {
		return
	}
	h.mu.Lock()
	if prev, ok := h.last[topic]; ok && prev == payload {
		h.mu.Unlock()
		return
	}
	h.last[topic] = payload
	h.mu.Unlock()
	PublishOutbound(topic, payload, true)
}

// forcePublishHAState publishes even when the same value was published before
// (connect/device-save republish) and records it for future dedupe.
func forcePublishHAState(topic, payload string) {
	if !mqttConnected() {
		return
	}
	GlobalHAStateSink.mu.Lock()
	GlobalHAStateSink.last[topic] = payload
	GlobalHAStateSink.mu.Unlock()
	PublishOutbound(topic, payload, true)
}

// forgetHAStateForDevice drops cached values for a device (device delete) so a
// recreated device with the same id republishes cleanly.
func forgetHAStateForDevice(deviceID int) {
	prefix := fmt.Sprintf("ledit/device/%d/", deviceID)
	GlobalHAStateSink.mu.Lock()
	for topic := range GlobalHAStateSink.last {
		if strings.HasPrefix(topic, prefix) {
			delete(GlobalHAStateSink.last, topic)
		}
	}
	GlobalHAStateSink.mu.Unlock()
}

// resetHAStateCache clears dedupe state on (re)connect so current values are
// republished even if the broker lost its retained store.
func resetHAStateCache() {
	GlobalHAStateSink.mu.Lock()
	GlobalHAStateSink.last = map[string]string{}
	GlobalHAStateSink.mu.Unlock()
}
