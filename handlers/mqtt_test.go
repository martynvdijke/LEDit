package handlers

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
)

// fake token
type fakeToken struct{ err error }

func (f fakeToken) Wait() bool                     { return true }
func (f fakeToken) WaitTimeout(time.Duration) bool { return true }
func (f fakeToken) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (f fakeToken) Error() error                   { return f.err }

// fake MQTT client for tests
type fakeClient struct {
	connected        bool
	connectErr       error
	subscribed       map[string]bool
	disconnectCalled bool
}

func (f *fakeClient) IsConnected() bool      { return f.connected }
func (f *fakeClient) IsConnectionOpen() bool { return f.connected }
func (f *fakeClient) Connect() mqtt.Token {
	if f.connectErr != nil {
		return fakeToken{err: f.connectErr}
	}
	f.connected = true
	return fakeToken{}
}
func (f *fakeClient) Disconnect(quiesce uint) { f.disconnectCalled = true; f.connected = false }
func (f *fakeClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	return fakeToken{}
}
func (f *fakeClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	if f.subscribed == nil {
		f.subscribed = map[string]bool{}
	}
	f.subscribed[topic] = true
	return fakeToken{}
}
func (f *fakeClient) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	return fakeToken{}
}
func (f *fakeClient) Unsubscribe(topics ...string) mqtt.Token             { return fakeToken{} }
func (f *fakeClient) AddRoute(topic string, callback mqtt.MessageHandler) {}
func (f *fakeClient) OptionsReader() mqtt.ClientOptionsReader             { return mqtt.ClientOptionsReader{} }
func newTestServerWithDB(t *testing.T) *Server {
	t.Helper()
	dsn := "file:memdb_mqtt_" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	drv, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db := ent.NewClient(ent.Driver(drv))
	if err := db.Schema.Create(context.Background()); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	srv := &Server{DB: db, Ctx: context.Background()}
	t.Cleanup(func() { db.Close() })
	return srv
}

func TestMQTTBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{6, 60 * time.Second}, // 64s capped
		{10, 60 * time.Second},
	}
	for _, c := range cases {
		got := BackoffDelay(c.attempt)
		if got != c.want {
			t.Errorf("BackoffDelay(%d)=%v want %v", c.attempt, got, c.want)
		}

	}
}

func TestMQTTPayloadMapping(t *testing.T) {
	// Ensure clean state
	GlobalFeed.Paused = false
	GlobalFeed.Skip = false

	HandleControlPayload("next")
	if st := GlobalFeed.Status(); st["paused"] == true {
		t.Errorf("next should not pause")
	}
	if !GlobalFeed.ShouldSkip() {
		t.Errorf("next should set skip")
	}

	GlobalFeed.Paused = false
	HandleControlPayload("skip")
	if !GlobalFeed.ShouldSkip() {
		t.Errorf("skip should set skip")
	}

	HandleControlPayload("pause")
	if !GlobalFeed.IsPaused() {
		t.Errorf("pause should pause")
	}

	HandleControlPayload("resume")
	if GlobalFeed.IsPaused() {
		t.Errorf("resume should unpause")
	}

	// unknown ignored
	GlobalFeed.Pause()
	HandleControlPayload("unknown_payload_xyz")
	if !GlobalFeed.IsPaused() {
		t.Errorf("unknown should not change paused")
	}
	// cleanup
	GlobalFeed.Resume()
	// trim handling
	HandleControlPayload("  next  ")
	if !GlobalFeed.ShouldSkip() {
		t.Errorf("trimmed next should work")
	}
}

func TestMQTTDisplayPayload(t *testing.T) {
	srv := newTestServerWithDB(t)
	// clear memory queue
	priorityMu.Lock()
	notifHistory = nil
	notifID = 0
	priorityMu.Unlock()

	HandleDisplayPayload(srv, "hello display")
	q := getMemoryQueue()
	if len(q) != 1 || q[0].Title != "hello display" {
		t.Fatalf("expected notification, got %+v", q)
	}
	after := NotificationsAfter(0)
	if len(after) != 1 {
		t.Fatalf("NotificationsAfter failed")
	}

	// empty payload ignored
	HandleDisplayPayload(srv, "   ")
	if len(getMemoryQueue()) != 1 {
		t.Errorf("empty payload should be ignored")
	}

	// also test controller method delegation
	ctrl := &MQTTController{s: srv}
	ctrl.handleDisplayPayload("second")
	if len(getMemoryQueue()) != 2 {
		t.Errorf("handleDisplayPayload via ctrl failed")
	}
}

func TestMQTTSettingsGating(t *testing.T) {
	orig := mqttNewClient
	defer func() { mqttNewClient = orig }()

	srv := newTestServerWithDB(t)

	// No row -> disabled => nil
	if c := StartMQTT(srv); c != nil {
		t.Errorf("expected nil when no settings")
	}

	// Disabled row
	srv.DB.MQTTSettings.Create().SetEnabled(false).SetBroker("tcp://broker:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)
	if c := StartMQTT(srv); c != nil {
		t.Errorf("expected nil when disabled")
	}
	// cleanup
	srv.DB.MQTTSettings.Delete().ExecX(srv.Ctx)

	// Enabled but empty broker
	srv.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)
	if c := StartMQTT(srv); c != nil {
		t.Errorf("expected nil when broker empty")
	}
	srv.DB.MQTTSettings.Delete().ExecX(srv.Ctx)

	// Enabled with broker but mock client fails connect -> still returns controller (non-fatal)
	srv.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("tcp://broker:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)
	fake := &fakeClient{connected: false, connectErr: nil}
	// Simulate successful connect: fake will connect
	mqttNewClient = func(opts *mqtt.ClientOptions) mqtt.Client { return fake }
	ctrl := StartMQTT(srv)
	if ctrl == nil {
		t.Fatalf("expected non-nil controller when enabled")
	}
	ctrl.Stop()
	// idempotent stop
	ctrl.Stop()

	// Disabled gating with fake that would otherwise connect
	srv.DB.MQTTSettings.Delete().ExecX(srv.Ctx)
	srv.DB.MQTTSettings.Create().SetEnabled(false).SetBroker("tcp://broker:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)
	if c := StartMQTT(srv); c != nil {
		t.Errorf("disabled should still gate even with fake")
	}
}

func TestMQTTRestartWithSettings(t *testing.T) {
	orig := mqttNewClient
	defer func() { mqttNewClient = orig }()

	srv := newTestServerWithDB(t)
	srv.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("tcp://broker:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)

	callCount := 0
	fake1 := &fakeClient{connected: true}
	fake2 := &fakeClient{connected: true}
	mqttNewClient = func(opts *mqtt.ClientOptions) mqtt.Client {
		callCount++
		if callCount == 1 {
			return fake1
		}
		return fake2
	}
	ctrl := StartMQTT(srv)
	if ctrl == nil {
		t.Fatal("start failed")
	}
	if !fake1.connected {
		t.Error("fake1 not connected")
	}

	// Update settings broker
	srv.DB.MQTTSettings.Delete().ExecX(srv.Ctx)
	srv.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("tcp://broker2:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)

	ctrl2 := ctrl.RestartWithSettings(srv)
	if ctrl2 == nil {
		t.Fatal("restart returned nil")
	}
	if !fake1.disconnectCalled {
		t.Errorf("old client not disconnected")
	}
	if ctrl2.cfg.Broker != "tcp://broker2:1883" {
		t.Errorf("new broker not loaded: %s", ctrl2.cfg.Broker)
	}
	ctrl2.Stop()

	// Test package-level RestartMQTT helper
	fake3 := &fakeClient{connected: true}
	mqttNewClient = func(opts *mqtt.ClientOptions) mqtt.Client { return fake3 }
	srv.DB.MQTTSettings.Delete().ExecX(srv.Ctx)
	srv.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("tcp://broker3:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)
	ctrl3 := RestartMQTT(nil, srv)
	if ctrl3 == nil || ctrl3.cfg.Broker != "tcp://broker3:1883" {
		t.Errorf("RestartMQTT failed")
	}
	ctrl3.Stop()
}

func TestMQTTLoadSettingsNilSafe(t *testing.T) {
	if got := LoadMQTTSettings(nil); got != nil {
		t.Errorf("expected nil for nil server")
	}
	srv := &Server{DB: nil, Ctx: context.Background()}
	if got := LoadMQTTSettings(srv); got != nil {
		t.Errorf("expected nil for nil DB")
	}
}
