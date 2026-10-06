package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func inputPublished(fake *fakeClient, topic, substr string) bool {
	for _, p := range fake.published {
		if p.topic == topic && strings.Contains(p.payload, substr) {
			return true
		}
	}
	return false
}

func setInputMqtt(t *testing.T, connected bool) *fakeClient {
	t.Helper()
	fake := &fakeClient{connected: connected}
	mqttCtrlGlobal = &MQTTController{client: fake}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })
	return fake
}

func TestPublishInputEventTopics(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("pub-in").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)
	fake := setInputMqtt(t, true)
	id := d.ID

	srv.handleDeviceInput(id, inputEvent(InputSourceNFC, InputEventTap, `"04a1b2c3"`))
	if !inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/tap", id), `"source":"nfc"`) {
		t.Fatalf("tap topic missing: %+v", fake.published)
	}
	if !inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/tap", id), `"value":"04a1b2c3"`) {
		t.Fatalf("tap value missing: %+v", fake.published)
	}
	if !inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/event", id), `"event_type":"tap"`) {
		t.Fatalf("HA event state missing: %+v", fake.published)
	}

	srv.handleDeviceInput(id, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if !inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/press", id), `"event":"press"`) {
		t.Fatalf("press topic missing: %+v", fake.published)
	}
	if inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/press", id), `"value"`) {
		t.Fatalf("valueless press must not publish a value: %+v", fake.published)
	}

	srv.handleDeviceInput(id, inputEvent(InputSourceEncoder, InputEventRotate, `-1`))
	if !inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/rotate", id), `"value":-1`) {
		t.Fatalf("rotate value missing: %+v", fake.published)
	}

	srv.handleDeviceInput(id, inputEvent(InputSourceLux, InputEventLux, `123.4`))
	if !inputPublished(fake, fmt.Sprintf("ledit/device/%d/input/lux", id), `"value":123.4`) {
		t.Fatalf("lux value missing: %+v", fake.published)
	}
}

func TestPublishInputEventNoopWhenDisconnected(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("pub-in2").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	fake := setInputMqtt(t, false)
	srv.handleDeviceInput(d.ID, inputEvent(InputSourceNFC, InputEventTap, `"aa"`))
	if len(fake.published) != 0 {
		t.Fatalf("disconnected broker must not receive publishes: %+v", fake.published)
	}

	mqttCtrlGlobal = nil
	SetGlobalMqttCtrl(nil)
	srv.handleDeviceInput(d.ID, inputEvent(InputSourceNFC, InputEventTap, `"aa"`)) // must not panic
}

func TestPublishHADiscoveryInputEventEntity(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("ha-in").SetWidth(64).SetHeight(32).SetTransport("websocket").SetToken("tok-secret-xyz").SaveX(srv.Ctx)
	fake := setInputMqtt(t, true)

	publishHADiscoveryForDevice(srv, d)

	topic := fmt.Sprintf("homeassistant/event/ledit_%d_input/config", d.ID)
	var raw string
	for _, p := range fake.published {
		if p.topic == topic && p.retained {
			raw = p.payload
		}
	}
	if raw == "" {
		t.Fatalf("event config not published: %+v", fake.published)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal event config: %v", err)
	}
	if payload["state_topic"] != fmt.Sprintf("ledit/device/%d/input/event", d.ID) {
		t.Fatalf("unexpected state_topic: %v", payload["state_topic"])
	}
	types, ok := payload["event_types"].([]any)
	if !ok || len(types) != 5 {
		t.Fatalf("event_types missing: %v", payload["event_types"])
	}
	want := []string{"press", "rotate", "tap", "presence", "lux"}
	for i, w := range want {
		if types[i] != w {
			t.Fatalf("event_types[%d]=%v want %s", i, types[i], w)
		}
	}
	if strings.Contains(raw, "tok-secret-xyz") {
		t.Fatalf("discovery payload leaked device token: %s", raw)
	}
}
