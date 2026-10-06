package handlers

import (
	"fmt"
	"strings"
	"testing"
)

func createActuationDevice(t *testing.T, srv *Server, name string) int {
	t.Helper()
	// websocket transport keeps these tests on the same feed-controller path
	// the WebSocket handler uses; push transports would register their own
	// runner controller via RestartTransportDevice.
	dev := srv.DB.DeviceSettings.Create().
		SetName(name).
		SetWidth(64).
		SetHeight(64).
		SetTransport("websocket").
		SetEnabled(true).
		SaveX(srv.Ctx)
	return dev.ID
}

func setActuationMqtt(t *testing.T, fake *fakeClient) {
	t.Helper()
	mqttCtrlGlobal = &MQTTController{client: fake}
	SetGlobalMqttCtrl(mqttCtrlGlobal)
	t.Cleanup(func() { mqttCtrlGlobal = nil })
}

func hasActuationPublish(msgs []publishedMsg, topic, payload string, retained bool) bool {
	for _, m := range msgs {
		if m.topic == topic && m.payload == payload && m.retained == retained {
			return true
		}
	}
	return false
}

func TestFeedControllerBrightnessHint(t *testing.T) {
	fc := &FeedController{}
	if _, ok := fc.BrightnessHint(); ok {
		t.Fatal("expected no brightness hint on fresh controller")
	}
	fc.SetBrightnessHint(37)
	if lvl, ok := fc.BrightnessHint(); !ok || lvl != 37 {
		t.Fatalf("BrightnessHint() = %d, %v; want 37, true", lvl, ok)
	}
	fc.ClearBrightnessHint()
	if _, ok := fc.BrightnessHint(); ok {
		t.Fatal("expected brightness hint to be cleared")
	}
}

func TestFeedControllerRotationKeys(t *testing.T) {
	fc := &FeedController{}
	fc.SetRotationKeys([]string{"weather:1", "", "rss:2"})
	if !fc.HasRotationKey("weather:1") || !fc.HasRotationKey("rss:2") {
		t.Fatal("expected weather:1 and rss:2 to be rotation keys")
	}
	if fc.HasRotationKey("") || fc.HasRotationKey("scene:9") {
		t.Fatal("unexpected rotation key match")
	}
}

func TestNotificationMatchesDevice(t *testing.T) {
	cases := []struct {
		target int
		device int
		want   bool
	}{
		{0, 1, true},
		{0, 42, true},
		{1, 1, true},
		{2, 1, false},
		{-1, 1, false},
	}
	for _, c := range cases {
		if got := notificationMatchesDevice(notifEntry{Target: c.target}, c.device); got != c.want {
			t.Fatalf("target %d device %d: got %v want %v", c.target, c.device, got, c.want)
		}
	}
}

func TestApplyDeviceBrightnessPersistsClampsAndHints(t *testing.T) {
	srv := newTestServerWithDB(t)
	devID := createActuationDevice(t, srv, "brightness-dev")
	fc := &FeedController{}
	registerDeviceFeed(devID, fc)
	defer unregisterDeviceFeed(devID)
	fake := &fakeClient{connected: true}
	setActuationMqtt(t, fake)

	srv.ApplyDeviceBrightness(devID, 142, "test")
	got := srv.DB.DeviceSettings.GetX(srv.Ctx, devID)
	if got.BrightnessOverride == nil || *got.BrightnessOverride != 100 {
		t.Fatalf("override = %v, want 100 (clamped)", got.BrightnessOverride)
	}
	if lvl, ok := fc.BrightnessHint(); !ok || lvl != 100 {
		t.Fatalf("hint = %d, %v; want 100, true", lvl, ok)
	}
	if !hasActuationPublish(fake.published, fmt.Sprintf("ledit/device/%d/brightness/state", devID), "100", true) {
		t.Fatal("expected retained brightness state publish")
	}

	srv.ApplyDeviceBrightness(devID, -5, "test")
	got = srv.DB.DeviceSettings.GetX(srv.Ctx, devID)
	if got.BrightnessOverride == nil || *got.BrightnessOverride != 0 {
		t.Fatalf("override = %v, want 0 (clamped)", got.BrightnessOverride)
	}
	if lvl, ok := fc.BrightnessHint(); !ok || lvl != 0 {
		t.Fatalf("hint = %d, %v; want 0, true", lvl, ok)
	}

	// Unknown devices are a logged no-op, never a panic.
	srv.ApplyDeviceBrightness(999999, 50, "test")
}

func TestApplyDeviceSelectSource(t *testing.T) {
	srv := newTestServerWithDB(t)
	devID := createActuationDevice(t, srv, "select-dev")
	fc := &FeedController{}
	fc.SetRotationKeys([]string{"weather:1"})
	registerDeviceFeed(devID, fc)
	defer unregisterDeviceFeed(devID)
	fake := &fakeClient{connected: true}
	setActuationMqtt(t, fake)

	if err := srv.ApplyDeviceSelect(devID, " source:weather:1 "); err != nil {
		t.Fatalf("in-rotation select failed: %v", err)
	}
	key, by, ok := fc.IsPinned()
	if !ok || key != "weather:1" || by != "select" {
		t.Fatalf("pin = %q by %q, %v; want weather:1 by select", key, by, ok)
	}
	if !hasActuationPublish(fake.published, fmt.Sprintf("ledit/device/%d/select/state", devID), "source:weather:1", true) {
		t.Fatal("expected retained select state publish")
	}

	// Off-rotation selections leave the active pin untouched and report success.
	if err := srv.ApplyDeviceSelect(devID, "source:rss:5"); err != nil {
		t.Fatalf("off-rotation select should be ignored, got %v", err)
	}
	key, _, ok = fc.IsPinned()
	if !ok || key != "weather:1" {
		t.Fatalf("off-rotation select changed pin to %q", key)
	}

	// Malformed selections are rejected.
	for _, bad := range []string{"", "bogus", "source:weather", "source:weather:abc", "source::1"} {
		if err := srv.ApplyDeviceSelect(devID, bad); err == nil {
			t.Fatalf("expected error for malformed selection %q", bad)
		}
	}
}

func TestApplyDeviceSelectPlaylist(t *testing.T) {
	srv := newTestServerWithDB(t)
	devID := createActuationDevice(t, srv, "select-playlist-dev")
	pl := srv.DB.Playlist.Create().SetName("pl").SetItems("[]").SetEnabled(true).SaveX(srv.Ctx)
	fake := &fakeClient{connected: true}
	setActuationMqtt(t, fake)

	if err := srv.ApplyDeviceSelect(devID, fmt.Sprintf("playlist:%d", pl.ID)); err != nil {
		t.Fatalf("playlist select failed: %v", err)
	}
	got := srv.DB.DeviceSettings.GetX(srv.Ctx, devID)
	if got.ContentMode != "playlist" {
		t.Fatalf("content mode = %q, want playlist", got.ContentMode)
	}
	if got.PlaylistID == nil || *got.PlaylistID != pl.ID {
		t.Fatalf("playlist id = %v, want %d", got.PlaylistID, pl.ID)
	}
	if got.ScheduledPlaylistIds != "[]" {
		t.Fatalf("scheduled playlists = %q, want []", got.ScheduledPlaylistIds)
	}
	if !hasActuationPublish(fake.published, fmt.Sprintf("ledit/device/%d/select/state", devID), fmt.Sprintf("playlist:%d", pl.ID), true) {
		t.Fatal("expected retained select state publish")
	}

	disabled := srv.DB.Playlist.Create().SetName("disabled").SetItems("[]").SetEnabled(false).SaveX(srv.Ctx)
	if err := srv.ApplyDeviceSelect(devID, fmt.Sprintf("playlist:%d", disabled.ID)); err == nil {
		t.Fatal("expected disabled playlist selection to be rejected")
	}
	if err := srv.ApplyDeviceSelect(devID, "scene:424242"); err == nil {
		t.Fatal("expected missing scene selection to be rejected")
	}
}

func TestApplyDeviceMessageTargeting(t *testing.T) {
	srv := newTestServerWithDB(t)
	devID := createActuationDevice(t, srv, "message-dev")
	fake := &fakeClient{connected: true}
	setActuationMqtt(t, fake)

	// Devices without a live feed are ignored: no notification, no state write.
	cursor := CurrentNotifSeq()
	if err := srv.ApplyDeviceMessage(devID, "hello"); err != nil {
		t.Fatalf("offline message returned error: %v", err)
	}
	if got := len(NotificationsAfter(cursor)); got != 0 {
		t.Fatalf("offline message produced %d notifications, want 0", got)
	}

	fc := &FeedController{}
	registerDeviceFeed(devID, fc)
	defer unregisterDeviceFeed(devID)

	if err := srv.ApplyDeviceMessage(devID, "  hello wall  "); err != nil {
		t.Fatalf("message failed: %v", err)
	}
	notifs := NotificationsAfter(cursor)
	if len(notifs) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifs))
	}
	n := notifs[0]
	if n.Title != "hello wall" {
		t.Fatalf("notification title = %q, want trimmed text", n.Title)
	}
	if n.Target != devID {
		t.Fatalf("notification target = %d, want %d", n.Target, devID)
	}
	if !notificationMatchesDevice(n, devID) {
		t.Fatal("targeted notification should match its device")
	}
	if notificationMatchesDevice(n, devID+1) {
		t.Fatal("targeted notification should not match another device")
	}
	if !hasActuationPublish(fake.published, fmt.Sprintf("ledit/device/%d/message/state", devID), "hello wall", true) {
		t.Fatal("expected retained message state publish")
	}

	if err := srv.ApplyDeviceMessage(devID, "   "); err != nil {
		t.Fatalf("empty message returned error: %v", err)
	}
	if got := len(NotificationsAfter(n.ID)); got != 0 {
		t.Fatalf("empty message produced %d notifications, want 0", got)
	}

	if err := srv.ApplyDeviceMessage(devID, strings.Repeat("x", 256)); err == nil {
		t.Fatal("expected over-length message to be rejected")
	}
}
