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

	publishHADiscoveryForDevice(d)

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
	for _, p := range fc.published {
		if strings.HasPrefix(p.topic, "homeassistant/") && p.payload == "" && p.retained {
			emptyConfigs++
		}
	}
	if emptyConfigs != 6 {
		t.Fatalf("expected 6 empty configs, got %d: %+v", emptyConfigs, fc.published)
	}
}
