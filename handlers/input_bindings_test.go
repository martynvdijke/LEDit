package handlers

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func inputEvent(source, event, value string) InputEvent {
	ev := InputEvent{Source: source, Event: event}
	if value != "" {
		ev.Value = json.RawMessage(value)
	}
	return ev
}

func createInputBinding(t *testing.T, srv *Server, deviceID *int, source, event, match, action string, order int) int {
	t.Helper()
	c := srv.DB.InputBinding.Create().
		SetSource(source).
		SetEvent(event).
		SetMatch(match).
		SetAction(action).
		SetOrder(order).
		SetEnabled(true)
	if deviceID != nil {
		c = c.SetDeviceID(*deviceID)
	}
	return c.SaveX(srv.Ctx).ID
}

func deviceBrightnessOverride(t *testing.T, srv *Server, deviceID int) int {
	t.Helper()
	d, err := srv.DB.DeviceSettings.Get(srv.Ctx, deviceID)
	if err != nil {
		t.Fatalf("device get: %v", err)
	}
	if d.BrightnessOverride == nil {
		return -1
	}
	return *d.BrightnessOverride
}

func TestParseInputAction(t *testing.T) {
	valid := []string{
		`{"kind":"brightness","level":25}`,
		`{"kind":"feed","verb":"previous"}`,
		`{"kind":"feed","verb":"toggle"}`,
		`{"kind":"notification","message":"hello"}`,
		`{"kind":"scene","scene_id":3}`,
		`{"kind":"playlist","playlist_id":4}`,
		`{"kind":"rule","rule_id":5}`,
		`{"kind":"greeting","greeting_id":6}`,
	}
	for _, raw := range valid {
		if _, err := ParseInputAction(raw); err != nil {
			t.Errorf("ParseInputAction(%s) unexpected error: %v", raw, err)
		}
	}
	invalid := []string{
		``,
		`{}`,
		`{"kind":"bogus"}`,
		`{"kind":"brightness"}`,
		`{"kind":"brightness","level":101}`,
		`{"kind":"feed","verb":"sideways"}`,
		`{"kind":"notification","message":""}`,
		`{"kind":"scene"}`,
		`{"kind":"playlist","playlist_id":0}`,
	}
	for _, raw := range invalid {
		if _, err := ParseInputAction(raw); err == nil {
			t.Errorf("ParseInputAction(%s) expected error", raw)
		}
	}
}

func TestValidateInputActionTargets(t *testing.T) {
	srv := newTestServerWithDB(t)
	sc := srv.DB.Scene.Create().SetName("in-scene").SetEnabled(false).SaveX(srv.Ctx)
	if msg := srv.ValidateInputActionTargets(srv.Ctx, InputAction{Kind: "scene", SceneID: sc.ID}); msg == "" {
		t.Fatalf("expected disabled scene target rejected")
	}
	if msg := srv.ValidateInputActionTargets(srv.Ctx, InputAction{Kind: "scene", SceneID: 99999}); msg == "" {
		t.Fatalf("expected missing scene target rejected")
	}
	sc2 := srv.DB.Scene.Create().SetName("in-scene-2").SetEnabled(true).SaveX(srv.Ctx)
	if msg := srv.ValidateInputActionTargets(srv.Ctx, InputAction{Kind: "scene", SceneID: sc2.ID}); msg != "" {
		t.Fatalf("expected valid scene target, got %q", msg)
	}
	if msg := srv.ValidateInputActionTargets(srv.Ctx, InputAction{Kind: "notification", Message: "hi"}); msg != "" {
		t.Fatalf("expected valid notification target, got %q", msg)
	}
}

func TestInputBindingFirstMatchWins(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "button:next", "press", "", `{"kind":"brightness","level":75}`, 1)
	createInputBinding(t, srv, nil, "button:next", "press", "", `{"kind":"brightness","level":25}`, 0)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 25 {
		t.Fatalf("expected first (order 0) binding level 25, got %d", got)
	}
}

func TestInputBindingDeviceScopedBeatsGlobalSameOrder(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-2").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "button:next", "press", "", `{"kind":"brightness","level":10}`, 0)
	createInputBinding(t, srv, &dev.ID, "button:next", "press", "", `{"kind":"brightness","level":20}`, 0)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 20 {
		t.Fatalf("expected device-scoped binding level 20, got %d", got)
	}
}

func TestInputBindingOrderBeatsScope(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-3").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "button:next", "press", "", `{"kind":"brightness","level":10}`, -1)
	createInputBinding(t, srv, &dev.ID, "button:next", "press", "", `{"kind":"brightness","level":20}`, 0)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 10 {
		t.Fatalf("expected lower-order global binding level 10, got %d", got)
	}
}

func TestInputBindingDisabledSkipped(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-4").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	srv.DB.InputBinding.Create().SetSource("button:next").SetEvent("press").
		SetAction(`{"kind":"brightness","level":90}`).SetOrder(0).SetEnabled(false).SaveX(srv.Ctx)
	id := createInputBinding(t, srv, nil, "button:next", "press", "", `{"kind":"brightness","level":30}`, 1)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 30 {
		t.Fatalf("expected enabled binding level 30 (id %d), got %d", id, got)
	}
}

func TestInputBindingMatchValue(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-5").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "nfc", "tap", "04a1b2c3", `{"kind":"brightness","level":40}`, 0)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceNFC, InputEventTap, `"04ffff"`))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != -1 {
		t.Fatalf("unmatched tag must not apply action, got %d", got)
	}
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceNFC, InputEventTap, `"04a1b2c3"`))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 40 {
		t.Fatalf("matched tag expected level 40, got %d", got)
	}
}

func TestInputBindingDefaultsWhenNoMatch(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-6").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	fc := &FeedController{DeviceID: dev.ID}
	registerDeviceFeed(dev.ID, fc)
	defer unregisterDeviceFeed(dev.ID)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if !fc.ShouldSkip() {
		t.Fatalf("default button:next press should request next")
	}
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceEncoder, InputEventRotate, `-1`))
	if !fc.ShouldPrev() {
		t.Fatalf("default encoder rotate -1 should request previous")
	}
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonPause, InputEventPress, ""))
	if !fc.IsPaused() {
		t.Fatalf("default button:pause press should pause")
	}
	fc.Resume()
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceEncoder, InputEventRotate, `1`))
	if !fc.ShouldSkip() {
		t.Fatalf("default encoder rotate +1 should request next")
	}
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceEncoder, InputEventPress, ""))
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 25 {
		t.Fatalf("default encoder press should cycle brightness to 25, got %d", got)
	}
}

func TestInputBindingOverridesDefault(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-7").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)
	fc := &FeedController{DeviceID: dev.ID}
	registerDeviceFeed(dev.ID, fc)
	defer unregisterDeviceFeed(dev.ID)

	createInputBinding(t, srv, nil, "button:next", "press", "", `{"kind":"brightness","level":60}`, 0)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonNext, InputEventPress, ""))
	if fc.ShouldSkip() {
		t.Fatalf("configured binding must suppress the built-in next default")
	}
	if got := deviceBrightnessOverride(t, srv, dev.ID); got != 60 {
		t.Fatalf("expected binding level 60, got %d", got)
	}
}

func TestInputBindingFeedPrevious(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-8").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)
	fc := &FeedController{DeviceID: dev.ID}
	registerDeviceFeed(dev.ID, fc)
	defer unregisterDeviceFeed(dev.ID)

	createInputBinding(t, srv, nil, "encoder", "rotate", "", `{"kind":"feed","verb":"previous"}`, 0)

	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceEncoder, InputEventRotate, `1`))
	if !fc.ShouldPrev() {
		t.Fatalf("feed previous binding should request previous")
	}
}

func TestInputBindingRuleRunsThenActions(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-9").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)
	rule := srv.DB.DisplayRule.Create().SetName("input-rule").SetEnabled(true).
		SetThenActions(`{"kind":"notification","message":"rule fired from input"}`).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "pir", "presence", "", `{"kind":"rule","rule_id":`+strconv.Itoa(rule.ID)+`}`, 0)

	before := srv.DB.Notification.Query().CountX(srv.Ctx)
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourcePIR, InputEventPresence, `"present"`))
	after := srv.DB.Notification.Query().CountX(srv.Ctx)
	if after <= before {
		t.Fatalf("expected rule then-actions to create a notification, before=%d after=%d", before, after)
	}
}

func TestInputBindingDeletedTargetSkippedNonFatal(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-10").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "nfc", "tap", "", `{"kind":"scene","scene_id":99999}`, 0)
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceNFC, InputEventTap, `"deadbeef"`))

	createInputBinding(t, srv, nil, "button:pause", "press", "", `{"kind":"rule","rule_id":99999}`, 0)
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceButtonPause, InputEventPress, ""))
	// Reaching this point without panic/connection closure is the assertion.
}

func TestInputBindingGreetingQuietHoursAndCooldown(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-11").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)

	row := srv.DB.GreetingRule.Create().SetName("input-greet").SetEntityPath("sensor.x").
		SetMessageTemplate("Welcome home").SetCooldownMinutes(30).SetTTLSeconds(30).SaveX(srv.Ctx)
	createInputBinding(t, srv, nil, "mmwave", "presence", "", `{"kind":"greeting","greeting_id":`+strconv.Itoa(row.ID)+`}`, 0)

	before := srv.DB.Notification.Query().CountX(srv.Ctx)
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceMMWave, InputEventPresence, `"present"`))
	after := srv.DB.Notification.Query().CountX(srv.Ctx)
	if after != before+1 {
		t.Fatalf("expected greeting notification, before=%d after=%d", before, after)
	}
	updated := srv.DB.GreetingRule.GetX(srv.Ctx, row.ID)
	if updated.LastTriggeredAt == nil {
		t.Fatalf("expected last_triggered_at to be persisted")
	}
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceMMWave, InputEventPresence, `"present"`))
	if again := srv.DB.Notification.Query().CountX(srv.Ctx); again != after {
		t.Fatalf("cooldown should suppress repeat greeting, before=%d after=%d", after, again)
	}

	quiet := "00:00"
	quietEnd := "23:59"
	qr := srv.DB.GreetingRule.Create().SetName("input-quiet").SetEntityPath("sensor.y").
		SetMessageTemplate("Quiet hello").SetCooldownMinutes(1).SetTTLSeconds(30).
		SetNillableQuietHoursStart(&quiet).SetNillableQuietHoursEnd(&quietEnd).SaveX(srv.Ctx)
	createInputBinding(t, srv, nil, "pir", "presence", "", `{"kind":"greeting","greeting_id":`+strconv.Itoa(qr.ID)+`}`, 0)
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourcePIR, InputEventPresence, `"present"`))
	if got := srv.DB.Notification.Query().CountX(srv.Ctx); got != after {
		t.Fatalf("quiet hours should suppress greeting, got %d want %d", got, after)
	}
	if refreshed := srv.DB.GreetingRule.GetX(srv.Ctx, qr.ID); refreshed.LastTriggeredAt != nil {
		t.Fatalf("suppressed greeting must not set last_triggered_at")
	}
}

func TestPushBrightnessConsultsLiveHint(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-13").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)
	fc := &FeedController{DeviceID: dev.ID}
	registerDeviceFeed(dev.ID, fc)
	defer unregisterDeviceFeed(dev.ID)

	pb := newPushBrightness(dev)
	fc.SetBrightnessHint(37)
	if got := pb.level(time.Now()); got != 37 {
		t.Fatalf("push transport should honor live hint, got %d", got)
	}
	fc.ClearBrightnessHint()
	if got := pb.level(time.Now()); got != 100 {
		t.Fatalf("push transport should fall back to policy, got %d", got)
	}
}

func TestInputBindingSceneActionSetsSceneSource(t *testing.T) {
	srv := newTestServerWithDB(t)
	dev := srv.DB.DeviceSettings.Create().SetName("bind-dev-12").SetWidth(32).SetHeight(32).SaveX(srv.Ctx)
	sc := srv.DB.Scene.Create().SetName("input-scene").SetEnabled(true).
		SetTriggers(`[]`).SetActions(`{}`).SaveX(srv.Ctx)

	createInputBinding(t, srv, nil, "nfc", "tap", "", `{"kind":"scene","scene_id":`+strconv.Itoa(sc.ID)+`}`, 0)
	srv.handleDeviceInput(dev.ID, inputEvent(InputSourceNFC, InputEventTap, `"cafe"`))
	// PreviewScene must not error; the scene tier precedence over notifications
	// is enforced by serveFeed, covered by the scene tests.
}
