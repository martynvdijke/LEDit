package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPublishHADiscoveryForDevice(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("MyDevice").SetWidth(64).SetHeight(32).SetTransport("wled").SetFirmwareVersion("1.2.3").SaveX(srv.Ctx)
	fc := &fakeClient{connected: true}
	mqttCtrlGlobal = &MQTTController{client: fc}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })

	publishHADiscoveryForDevice(srv, d)

	// find brightness config
	found := false
	for _, p := range fc.published {
		if p.topic == fmt.Sprintf("homeassistant/number/ledit_%d_brightness/config", d.ID) && p.retained {
			if !strings.Contains(p.payload, "command_topic") {
				t.Fatalf("payload missing command_topic: %s", p.payload)
			}
			if !strings.Contains(p.payload, fmt.Sprintf("ledit/device/%d/brightness/set", d.ID)) {
				t.Fatalf("payload missing brightness set topic: %s", p.payload)
			}
			// check identifiers
			var m map[string]any
			if err := json.Unmarshal([]byte(p.payload), &m); err != nil {
				t.Fatalf("json unmarshal: %v", err)
			}
			dev, ok := m["device"].(map[string]any)
			if !ok {
				t.Fatalf("device block missing")
			}
			ids, _ := dev["identifiers"].([]any)
			has := false
			for _, v := range ids {
				if v == fmt.Sprintf("ledit_%d", d.ID) {
					has = true
				}
			}
			if !has {
				t.Fatalf("identifiers missing ledit_%d", d.ID)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("brightness config not published; topics: %+v", fc.published)
	}
}

func TestPublishHADiscoveryGate(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("D1").SetWidth(32).SetHeight(32).SetTransport("websocket").SaveX(srv.Ctx)
	_ = d

	// ensure default disabled
	st := EnsureOutboundSettings(srv.DB)
	// reset to disabled
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetHaDiscoveryEnabled(false).ExecX(srv.Ctx)

	fc := &fakeClient{connected: true}
	mqttCtrlGlobal = &MQTTController{client: fc}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })

	PublishHADiscoveryForAll(srv)
	if len(fc.published) != 0 {
		t.Fatalf("expected no publishes when disabled, got %d", len(fc.published))
	}

	// enable
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetHaDiscoveryEnabled(true).ExecX(srv.Ctx)
	fc2 := &fakeClient{connected: true}
	mqttCtrlGlobal = &MQTTController{client: fc2}
	SetGlobalMqttCtrl(mqttCtrlGlobal)

	PublishHADiscoveryForAll(srv)
	if len(fc2.published) == 0 {
		t.Fatalf("expected publishes when enabled")
	}
	hasGlobal := false
	hasDevice := false
	for _, p := range fc2.published {
		if p.topic == "homeassistant/sensor/ledit_current_source/config" {
			hasGlobal = true
		}
		if strings.Contains(p.topic, fmt.Sprintf("ledit_%d_", d.ID)) {
			hasDevice = true
		}
	}
	if !hasGlobal || !hasDevice {
		t.Fatalf("missing global %v device %v", hasGlobal, hasDevice)
	}
}

func TestHandlePerDeviceCommand_Brightness(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("D2").SetWidth(64).SetHeight(64).SetTransport("wled").SaveX(srv.Ctx)
	fc := &fakeClient{connected: true}
	ctrl := &MQTTController{s: srv, client: fc}
	mqttCtrlGlobal = &MQTTController{client: fc}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })

	ctrl.handlePerDeviceCommand(fmt.Sprintf("ledit/device/%d/brightness/set", d.ID), "42")
	updated, _ := srv.DB.DeviceSettings.Get(srv.Ctx, d.ID)
	if updated.BrightnessOverride == nil || *updated.BrightnessOverride != 42 {
		t.Fatalf("expected 42 got %v", updated.BrightnessOverride)
	}
	found := false
	for _, p := range fc.published {
		if p.topic == fmt.Sprintf("ledit/device/%d/brightness/state", d.ID) && p.payload == "42" && p.retained {
			found = true
		}
	}
	if !found {
		t.Fatalf("brightness state not published: %+v", fc.published)
	}
}

func TestHandlePerDeviceCommand_Paused(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("D3").SetWidth(64).SetHeight(32).SetTransport("wled").SaveX(srv.Ctx)
	fc := &fakeClient{connected: true}
	ctrl := &MQTTController{s: srv, client: fc}
	mqttCtrlGlobal = &MQTTController{client: fc}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })

	devFC := &FeedController{}
	registerDeviceFeed(d.ID, devFC)
	t.Cleanup(func() { unregisterDeviceFeed(d.ID) })

	ctrl.handlePerDeviceCommand(fmt.Sprintf("ledit/device/%d/paused/set", d.ID), "true")
	if !devFC.Paused {
		t.Fatalf("expected paused")
	}
	found := false
	for _, p := range fc.published {
		if p.topic == fmt.Sprintf("ledit/device/%d/paused", d.ID) && p.payload == "true" {
			found = true
		}
	}
	if !found {
		t.Fatalf("paused publish missing")
	}
	ctrl.handlePerDeviceCommand(fmt.Sprintf("ledit/device/%d/paused/set", d.ID), "false")
	if devFC.Paused {
		t.Fatalf("expected resumed")
	}
}

func TestClearHADiscoveryForDevice(t *testing.T) {
	fc := &fakeClient{connected: true}
	mqttCtrlGlobal = &MQTTController{client: fc}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })

	clearHADiscoveryForDevice(99)
	emptyConfigs := 0
	cleared := map[string]bool{}
	for _, p := range fc.published {
		if strings.HasPrefix(p.topic, "homeassistant/") && p.payload == "" && p.retained {
			emptyConfigs++
		}
		if p.payload == "" && p.retained {
			cleared[p.topic] = true
		}
	}
	if emptyConfigs != 9 {
		t.Fatalf("expected 9 empty configs, got %d: %+v", emptyConfigs, fc.published)
	}
	for _, topic := range []string{
		"homeassistant/light/ledit_99_light/config",
		"homeassistant/select/ledit_99_content/config",
		"homeassistant/text/ledit_99_message/config",
		"ledit/device/99/select/state",
		"ledit/device/99/message/state",
	} {
		if !cleared[topic] {
			t.Fatalf("expected %s cleared, got %+v", topic, fc.published)
		}
	}
}

func TestPublishHADiscoveryForDevice_NewEntities(t *testing.T) {
	srv := newTestServerWithDB(t)
	d := srv.DB.DeviceSettings.Create().SetName("NewEnt").SetWidth(64).SetHeight(32).SetTransport("websocket").SaveX(srv.Ctx)
	pl := srv.DB.Playlist.Create().SetName("pl").SetItems("[]").SetEnabled(true).SaveX(srv.Ctx)
	sc := srv.DB.Scene.Create().SetName("sc").SetEnabled(true).SaveX(srv.Ctx)

	fc := &FeedController{}
	fc.SetRotationKeys([]string{"clock:1", "textslide:2"})
	registerDeviceFeed(d.ID, fc)
	t.Cleanup(func() { unregisterDeviceFeed(d.ID) })

	fake := &fakeClient{connected: true}
	mqttCtrlGlobal = &MQTTController{client: fake}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })

	publishHADiscoveryForDevice(srv, d)

	find := func(topic string) map[string]any {
		t.Helper()
		for _, p := range fake.published {
			if p.topic == topic && p.retained {
				var m map[string]any
				if err := json.Unmarshal([]byte(p.payload), &m); err != nil {
					t.Fatalf("unmarshal %s: %v", topic, err)
				}
				return m
			}
		}
		t.Fatalf("topic not published: %s (%+v)", topic, fake.published)
		return nil
	}

	// light: additive template entity sharing the brightness topics
	light := find(fmt.Sprintf("homeassistant/light/ledit_%d_light/config", d.ID))
	if light["unique_id"] != fmt.Sprintf("ledit_%d_light", d.ID) {
		t.Fatalf("bad light unique_id: %v", light["unique_id"])
	}
	if light["command_topic"] != fmt.Sprintf("ledit/device/%d/brightness/set", d.ID) {
		t.Fatalf("bad light command_topic: %v", light["command_topic"])
	}
	if light["state_topic"] != fmt.Sprintf("ledit/device/%d/brightness/state", d.ID) {
		t.Fatalf("bad light state_topic: %v", light["state_topic"])
	}
	if light["brightness_scale"] != float64(100) {
		t.Fatalf("bad light brightness_scale: %v", light["brightness_scale"])
	}

	// select: rotation sources + enabled playlists + enabled scenes
	sel := find(fmt.Sprintf("homeassistant/select/ledit_%d_content/config", d.ID))
	if sel["command_topic"] != fmt.Sprintf("ledit/device/%d/select/set", d.ID) {
		t.Fatalf("bad select command_topic: %v", sel["command_topic"])
	}
	if sel["state_topic"] != fmt.Sprintf("ledit/device/%d/select/state", d.ID) {
		t.Fatalf("bad select state_topic: %v", sel["state_topic"])
	}
	opts, _ := sel["options"].([]any)
	wantOpts := map[string]bool{
		"source:clock:1":                  false,
		"source:textslide:2":              false,
		fmt.Sprintf("playlist:%d", pl.ID): false,
		fmt.Sprintf("scene:%d", sc.ID):    false,
	}
	for _, o := range opts {
		s, _ := o.(string)
		if _, ok := wantOpts[s]; ok {
			wantOpts[s] = true
		}
	}
	for opt, seen := range wantOpts {
		if !seen {
			t.Fatalf("select option missing %s in %v", opt, opts)
		}
	}

	// text: device-scoped message entity
	txt := find(fmt.Sprintf("homeassistant/text/ledit_%d_message/config", d.ID))
	if txt["command_topic"] != fmt.Sprintf("ledit/device/%d/message/set", d.ID) {
		t.Fatalf("bad text command_topic: %v", txt["command_topic"])
	}
	if txt["state_topic"] != fmt.Sprintf("ledit/device/%d/message/state", d.ID) {
		t.Fatalf("bad text state_topic: %v", txt["state_topic"])
	}
	if txt["max"] != float64(255) {
		t.Fatalf("bad text max: %v", txt["max"])
	}
}
