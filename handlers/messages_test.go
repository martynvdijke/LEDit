package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"
)

// isolateMessages resets package-global message state so bus/sink/ack tests do
// not leak into one another.
func isolateMessages(t *testing.T) {
	t.Helper()
	GlobalBus.Reset()
	prevMqttSink := GlobalMqttSink
	GlobalMqttSink = NewMqttSink()
	prevLog := GlobalDeliveryLog
	GlobalDeliveryLog = nil
	prevCtrl := mqttCtrlGlobal
	mqttCtrlGlobal = nil
	clearNotifHistory()
	setIncidentCache(nil)
	resetAckStore()
	resetRecentIDs()
	resetPublishedMessages()
	t.Cleanup(func() {
		GlobalBus.Reset()
		GlobalMqttSink = prevMqttSink
		GlobalDeliveryLog = prevLog
		mqttCtrlGlobal = prevCtrl
		clearNotifHistory()
		setIncidentCache(nil)
		resetAckStore()
		resetRecentIDs()
		resetPublishedMessages()
	})
}

func resetPublishedMessages() {
	publishedMessages.mu.Lock()
	publishedMessages.m = map[string]Message{}
	publishedMessages.mu.Unlock()
}

func resetAckStore() {
	ackStore.mu.Lock()
	ackStore.m = map[int]DeviceAck{}
	ackStore.mu.Unlock()
}

func resetRecentIDs() {
	recentIDs.mu.Lock()
	recentIDs.m = map[string]time.Time{}
	recentIDs.mu.Unlock()
}

func TestActiveMessagesUnionTTLAndSort(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	client := srv.DB

	// Live notification with TTL, plus one already expired.
	live := addToMemoryQueueWithOptions("live", "body", WithTTL(time.Minute))
	addToMemoryQueueWithOptions("gone", "body", withExpiresAt(time.Now().Add(-time.Minute)))

	// Open incident created after the notification; a resolved incident excluded.
	now := time.Now()
	client.Incident.Create().
		SetFingerprint("open").SetTitle("OUT").SetMessage("m").SetSeverity("critical").
		SetActive(true).SetCreatedAt(now.Add(time.Second)).SetExpiresAt(now.Add(time.Hour)).
		SaveX(context.Background())
	client.Incident.Create().
		SetFingerprint("resolved").SetTitle("old").SetMessage("m").SetSeverity("info").
		SetActive(false).SetResolvedAt(now).SetCreatedAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Hour)).
		SaveX(context.Background())
	reloadIncidents(client)

	msgs := ActiveMessages()
	if len(msgs) != 2 {
		t.Fatalf("active messages = %d, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Kind != MessageKindIncident || msgs[0].Severity != "critical" {
		t.Fatalf("incident should sort first (newest): %+v", msgs[0])
	}
	wantNotifID := "notif:" + strconv.Itoa(live.ID)
	if msgs[1].ID != wantNotifID || msgs[1].Kind != MessageKindNotification {
		t.Fatalf("notification message = %+v, want id %s", msgs[1], wantNotifID)
	}
	if msgs[1].ExpiresAt == nil || !msgs[1].ExpiresAt.After(now) {
		t.Fatalf("notification should carry future expiry: %+v", msgs[1].ExpiresAt)
	}
	if msgs[0].ExpiresAt != nil {
		t.Fatalf("open incident should have nil ExpiresAt: %+v", msgs[0].ExpiresAt)
	}
}

func TestNotificationToMessageTTL(t *testing.T) {
	isolateMessages(t)
	e := addToMemoryQueueWithOptions("title", "message", WithTTL(60*time.Second))
	m := NotificationToMessage(e)
	if m.Kind != MessageKindNotification || m.Title != "title" || m.Body != "message" {
		t.Fatalf("unexpected mapping: %+v", m)
	}
	if m.ID != "notif:"+strconv.Itoa(e.ID) {
		t.Fatalf("id = %s", m.ID)
	}
	if m.ExpiresAt == nil {
		t.Fatal("expected TTL expiry")
	}
	// Expiry is CreatedAt + TTL (within a small window).
	if got := m.ExpiresAt.Sub(m.CreatedAt); got < 59*time.Second || got > 61*time.Second {
		t.Fatalf("ttl = %v, want ~60s", got)
	}
}

func TestTRMNLMessagesEndpoint(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/trmnl/messages", srv.APITrmnlMessages)
	r.POST("/api/trmnl/messages/refresh", srv.APITrmnlMessagesRefresh)
	srv.Router = r

	srv.AddNotification("hello", "world", WithTTL(time.Minute))
	addToMemoryQueueWithOptions("expired", "x", withExpiresAt(time.Now().Add(-time.Minute)))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/trmnl/messages", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Cache-Control"); ct != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", ct)
	}
	var resp struct {
		Messages    []Message `json:"messages"`
		GeneratedAt string    `json:"generated_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Messages) != 1 || resp.Messages[0].Title != "hello" {
		t.Fatalf("messages = %+v", resp.Messages)
	}
	if _, err := time.Parse(time.RFC3339, resp.GeneratedAt); err != nil {
		t.Fatalf("generated_at %q not RFC3339: %v", resp.GeneratedAt, err)
	}

	// Refresh trigger is a 200 no-op.
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/trmnl/messages/refresh", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("refresh status = %d", w2.Code)
	}
}

func TestRecordAckUnknownIgnoredAndValidRecorded(t *testing.T) {
	isolateMessages(t)

	// Unknown / malformed ids never update last-acked.
	recordAck(5, "notif:999")
	recordAck(5, "garbage")
	recordAck(0, "notif:1")
	if acks := DeviceAcks(); len(acks) != 0 {
		t.Fatalf("unknown ack recorded: %+v", acks)
	}

	// A recently fired id can be acked even after it leaves the active set.
	emitMessageFired(Message{ID: "notif:1", Kind: MessageKindNotification, Title: "t", CreatedAt: time.Now()})
	recordAck(5, "notif:1")
	acks := DeviceAcks()
	if len(acks) != 1 || acks[0].DeviceID != 5 || acks[0].LastAckedID != "notif:1" {
		t.Fatalf("valid ack not recorded: %+v", acks)
	}

	// An invalid id after a valid one does not clobber the record.
	recordAck(5, "incident:notanumber")
	if acks := DeviceAcks(); len(acks) != 1 || acks[0].LastAckedID != "notif:1" {
		t.Fatalf("invalid ack clobbered state: %+v", acks)
	}
}

// recordingClient captures MQTT publishes so topic/payload/retain can be
// asserted without a broker.
type recordingClient struct {
	fakeClient
	pub []recordedPublish
}

type recordedPublish struct {
	topic    string
	payload  string
	retained bool
}

func (r *recordingClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	s, _ := payload.(string)
	r.pub = append(r.pub, recordedPublish{topic: topic, payload: s, retained: retained})
	return fakeToken{}
}

func (r *recordingClient) find(topic string) (recordedPublish, bool) {
	for _, p := range r.pub {
		if p.topic == topic {
			return p, true
		}
	}
	return recordedPublish{}, false
}

func TestMQTTMessageLifecycleFiredAndResolved(t *testing.T) {
	isolateMessages(t)
	entry := addToMemoryQueueWithOptions("seed", "body", WithTTL(time.Minute))
	msg := NotificationToMessage(entry)

	fake := &recordingClient{}
	fake.connected = true
	SetGlobalMqttCtrl(&MQTTController{client: fake})
	defer SetGlobalMqttCtrl(nil)
	GlobalMqttSink.SetEnabled(true)

	if err := GlobalMqttSink.Handle(Event{Type: EventMessageFired, Data: msg}); err != nil {
		t.Fatalf("handle fired: %v", err)
	}
	base := "ledit/message/" + msg.ID
	state, ok := fake.find(base + "/state")
	if !ok || state.payload != "fired" || !state.retained {
		t.Fatalf("state publish = %+v ok=%v", state, ok)
	}
	js, ok := fake.find(base + "/json")
	if !ok || !js.retained || !strings.Contains(js.payload, `"id":"`+msg.ID+`"`) {
		t.Fatalf("json publish = %+v ok=%v", js, ok)
	}
	agg, ok := fake.find("ledit/messages/active")
	if !ok || !agg.retained || !strings.Contains(agg.payload, msg.ID) {
		t.Fatalf("aggregated publish = %+v ok=%v", agg, ok)
	}

	// Resolve clears the per-message JSON with an empty retained payload.
	fake.pub = nil
	if err := GlobalMqttSink.Handle(Event{Type: EventMessageResolved, Data: msg}); err != nil {
		t.Fatalf("handle resolved: %v", err)
	}
	if state, ok := fake.find(base + "/state"); !ok || state.payload != "resolved" {
		t.Fatalf("resolved state = %+v ok=%v", state, ok)
	}
	if js, ok := fake.find(base + "/json"); !ok || js.payload != "" || !js.retained {
		t.Fatalf("resolved json should be empty retained: %+v ok=%v", js, ok)
	}
}

func TestMQTTPublishNoopWhenUnconfigured(t *testing.T) {
	isolateMessages(t)
	GlobalMqttSink.SetEnabled(true)
	// No controller/client wired: must not panic or publish.
	if err := GlobalMqttSink.Handle(Event{Type: EventMessageFired, Data: Message{ID: "notif:1"}}); err != nil {
		t.Fatalf("unconfigured publish errored: %v", err)
	}
}

func TestReconcileTombstonesExpiredMessage(t *testing.T) {
	isolateMessages(t)
	fake := &recordingClient{}
	fake.connected = true
	SetGlobalMqttCtrl(&MQTTController{client: fake})
	defer SetGlobalMqttCtrl(nil)

	expired := time.Now().Add(-time.Minute)
	rememberPublishedMessage(Message{
		ID: "notif:42", Kind: MessageKindNotification, Body: "b",
		CreatedAt: time.Now().Add(-time.Hour), ExpiresAt: &expired,
	})
	reconcilePublishedMessages()

	base := "ledit/message/notif:42"
	if state, ok := fake.find(base + "/state"); !ok || state.payload != "resolved" {
		t.Fatalf("expiry sweep state = %+v ok=%v", state, ok)
	}
	if js, ok := fake.find(base + "/json"); !ok || js.payload != "" || !js.retained {
		t.Fatalf("expiry sweep json = %+v ok=%v", js, ok)
	}
}

func TestTruncateMessageBody(t *testing.T) {
	long := strings.Repeat("a", maxMessageBodyBytes+50)
	m := truncateMessageBody(Message{Body: long})
	if !strings.HasSuffix(m.Body, "…") {
		t.Fatalf("expected ellipsis, got %q", m.Body)
	}
	if len(m.Body) > maxMessageBodyBytes+len("…") {
		t.Fatalf("body not truncated: %d bytes", len(m.Body))
	}
	// Multibyte input must remain valid UTF-8.
	mb := truncateMessageBody(Message{Body: strings.Repeat("é", 800)})
	if !json.Valid([]byte(strconv.Quote(mb.Body))) {
		t.Fatal("truncated multibyte body is not valid UTF-8")
	}
}
