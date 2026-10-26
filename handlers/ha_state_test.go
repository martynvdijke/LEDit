package handlers

import (
	"fmt"
	"strings"
	"testing"
)

func setHASinkEnabled(t *testing.T, enabled bool) {
	t.Helper()
	GlobalHAStateSink.SetEnabled(enabled)
	resetHAStateCache()
	t.Cleanup(func() {
		GlobalHAStateSink.SetEnabled(false)
		resetHAStateCache()
	})
}

func TestHAStateSinkLivenessDedupe(t *testing.T) {
	srv := newTestServerWithDB(t)
	id := createActuationDevice(t, srv, "ha1")
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	setHASinkEnabled(t, true)

	online := Event{Type: EventDeviceLivenessChanged, Data: map[string]any{"device_id": id, "online": true}}
	if err := GlobalHAStateSink.Handle(online); err != nil {
		t.Fatalf("handle: %v", err)
	}
	topic := fmt.Sprintf("ledit/device/%d/online", id)
	if !hasActuationPublish(fc.published, topic, "true", true) {
		t.Fatalf("expected online publish: %+v", fc.published)
	}
	// Duplicate value must not republish.
	n := len(fc.published)
	_ = GlobalHAStateSink.Handle(online)
	if len(fc.published) != n {
		t.Fatalf("duplicate event republished: %+v", fc.published)
	}
	// A value change publishes.
	_ = GlobalHAStateSink.Handle(Event{Type: EventDeviceLivenessChanged, Data: map[string]any{"device_id": id, "online": false}})
	if !hasActuationPublish(fc.published, topic, "false", true) {
		t.Fatalf("expected offline publish: %+v", fc.published)
	}
}

func TestHAStateSinkPauseResumePerDevice(t *testing.T) {
	srv := newTestServerWithDB(t)
	id := createActuationDevice(t, srv, "ha2")
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	setHASinkEnabled(t, true)

	_ = GlobalHAStateSink.Handle(Event{Type: EventFeedPaused, Data: map[string]any{"device_id": id}})
	if !hasActuationPublish(fc.published, "ledit/status/paused", "true", true) {
		t.Fatalf("expected global paused publish: %+v", fc.published)
	}
	if !hasActuationPublish(fc.published, fmt.Sprintf("ledit/device/%d/paused", id), "true", true) {
		t.Fatalf("expected per-device paused publish: %+v", fc.published)
	}
	_ = GlobalHAStateSink.Handle(Event{Type: EventFeedResumed, Data: map[string]any{"device_id": id}})
	if !hasActuationPublish(fc.published, fmt.Sprintf("ledit/device/%d/paused", id), "false", true) {
		t.Fatalf("expected per-device resumed publish: %+v", fc.published)
	}
}

func TestHAStateSinkSourceChanged(t *testing.T) {
	srv := newTestServerWithDB(t)
	_ = createActuationDevice(t, srv, "ha3")
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	setHASinkEnabled(t, true)

	_ = GlobalHAStateSink.Handle(Event{Type: EventSourceChanged, Data: map[string]string{"from": "a", "to": "clock:1"}})
	if !hasActuationPublish(fc.published, "ledit/status/current_source", "clock:1", true) {
		t.Fatalf("expected current_source publish: %+v", fc.published)
	}
}

func TestHAStateSinkDisabledOrDisconnected(t *testing.T) {
	// Disabled: Handle is a no-op even with a connected broker.
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	GlobalHAStateSink.SetEnabled(false)
	resetHAStateCache()
	_ = GlobalHAStateSink.Handle(Event{Type: EventDeviceLivenessChanged, Data: map[string]any{"device_id": 1, "online": true}})
	if len(fc.published) != 0 {
		t.Fatalf("disabled sink published: %+v", fc.published)
	}

	// Enabled but disconnected: no error, no publishes.
	fc2 := &fakeClient{connected: false}
	setActuationMqtt(t, fc2)
	GlobalHAStateSink.SetEnabled(true)
	resetHAStateCache()
	if err := GlobalHAStateSink.Handle(Event{Type: EventFeedPaused, Data: map[string]any{"device_id": 1}}); err != nil {
		t.Fatalf("handle disconnected: %v", err)
	}
	if len(fc2.published) != 0 {
		t.Fatalf("disconnected sink published: %+v", fc2.published)
	}
	GlobalHAStateSink.SetEnabled(false)
	resetHAStateCache()
}

func TestHAStateSinkIndependentOfMqttPublishToggle(t *testing.T) {
	srv := newTestServerWithDB(t)
	id := createActuationDevice(t, srv, "ha4")
	// Generic outbound MQTT events are switched off; HA entity state must
	// still republish. Dispatch through a local dispatcher so the disabled
	// MqttSink and the enabled HA sink are both present.
	st := EnsureOutboundSettings(srv.DB)
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetMqttPublishEnabled(false).ExecX(srv.Ctx)
	GlobalMqttSink.SetEnabled(false)
	t.Cleanup(func() { GlobalMqttSink.SetEnabled(false) })

	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	setHASinkEnabled(t, true)

	d := &Dispatcher{}
	d.Register(GlobalMqttSink)
	d.Register(GlobalHAStateSink)
	d.Dispatch(Event{Type: EventDeviceLivenessChanged, Data: map[string]any{"device_id": id, "online": true}})
	if !hasActuationPublish(fc.published, fmt.Sprintf("ledit/device/%d/online", id), "true", true) {
		t.Fatalf("expected liveness publish with generic MQTT off: %+v", fc.published)
	}
}

func TestHAStateSinkResetRepublishesOnConnect(t *testing.T) {
	srv := newTestServerWithDB(t)
	id := createActuationDevice(t, srv, "ha5")
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	setHASinkEnabled(t, true)

	online := Event{Type: EventDeviceLivenessChanged, Data: map[string]any{"device_id": id, "online": true}}
	_ = GlobalHAStateSink.Handle(online)
	if len(fc.published) != 1 {
		t.Fatalf("expected one publish, got %+v", fc.published)
	}
	// A (re)connect resets dedupe so the current value is republished.
	resetHAStateCache()
	_ = GlobalHAStateSink.Handle(online)
	if len(fc.published) != 2 {
		t.Fatalf("expected republish after cache reset: %+v", fc.published)
	}
}

func TestPublishHAStateForDeviceNewStates(t *testing.T) {
	srv := newTestServerWithDB(t)
	id := createActuationDevice(t, srv, "ha6")
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)
	setHASinkEnabled(t, true)

	// Live controller: paused + pinned source + in-flight message.
	devFC := &FeedController{DeviceID: id}
	devFC.Paused = true
	devFC.Pin("clock:1", "select")
	registerDeviceFeed(id, devFC)
	t.Cleanup(func() { unregisterDeviceFeed(id) })
	deviceMessageTexts.Store(id, "hello wall")
	t.Cleanup(func() { deviceMessageTexts.Delete(id) })

	d, err := srv.DB.DeviceSettings.Get(srv.Ctx, id)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	publishHAStateForDevice(d)

	if !hasActuationPublish(fc.published, fmt.Sprintf("ledit/device/%d/paused", id), "true", true) {
		t.Fatalf("expected paused state: %+v", fc.published)
	}
	if !hasActuationPublish(fc.published, fmt.Sprintf("ledit/device/%d/select/state", id), "source:clock:1", true) {
		t.Fatalf("expected select state: %+v", fc.published)
	}
	if !hasActuationPublish(fc.published, fmt.Sprintf("ledit/device/%d/message/state", id), "hello wall", true) {
		t.Fatalf("expected message state: %+v", fc.published)
	}
}

func TestRepublishHASelectsForAll(t *testing.T) {
	srv := newTestServerWithDB(t)
	id := createActuationDevice(t, srv, "ha7")
	pl := srv.DB.Playlist.Create().SetName("pl").SetItems("[]").SetEnabled(true).SaveX(srv.Ctx)
	sc := srv.DB.Scene.Create().SetName("sc1").SetEnabled(true).SaveX(srv.Ctx)
	fc := &fakeClient{connected: true}
	setActuationMqtt(t, fc)

	st := EnsureOutboundSettings(srv.DB)
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetHaDiscoveryEnabled(true).ExecX(srv.Ctx)
	setHASinkEnabled(t, true)

	republishHASelectsForAll(srv)
	topic := fmt.Sprintf("homeassistant/select/ledit_%d_content/config", id)
	found := false
	for _, p := range fc.published {
		if p.topic == topic && p.retained &&
			strings.Contains(p.payload, fmt.Sprintf("playlist:%d", pl.ID)) &&
			strings.Contains(p.payload, fmt.Sprintf("scene:%d", sc.ID)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("select config not republished with new options: %+v", fc.published)
	}

	// Disabled discovery must no-op.
	srv.DB.OutboundSettings.UpdateOneID(st.ID).SetHaDiscoveryEnabled(false).ExecX(srv.Ctx)
	n := len(fc.published)
	republishHASelectsForAll(srv)
	if len(fc.published) != n {
		t.Fatalf("disabled discovery republished selects: %+v", fc.published[n:])
	}
}
