package handlers

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/deliverylog"
)

func TestDeliveryLogCapAndPrune(t *testing.T) {
	srv := newTestServerWithDB(t)
	client := srv.DB
	defer func(prev *DeliveryLogWriter) { GlobalDeliveryLog = prev }(GlobalDeliveryLog)

	w := NewDeliveryLogWriter(func() *ent.Client { return client })
	w.maxRows = 3
	w.ttl = time.Hour

	base := time.Now().Add(-time.Minute)
	for i := 0; i < 6; i++ {
		w.Record(DeliveryLogEntry{
			MessageID:   "notif:" + strconv.Itoa(i),
			Kind:        MessageKindNotification,
			Surface:     "mqtt",
			Target:      "t",
			Status:      "delivered",
			AttemptedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	rows := client.DeliveryLog.Query().
		Order(ent.Desc(deliverylog.FieldAttemptedAt)).AllX(context.Background())
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want capped at 3", len(rows))
	}
	if rows[0].MessageID != "notif:5" {
		t.Fatalf("newest row = %q, want notif:5", rows[0].MessageID)
	}

	// A row beyond the TTL is pruned on the next insert.
	w.Record(DeliveryLogEntry{
		MessageID:   "notif:old",
		Surface:     "webhook",
		Status:      "failed",
		AttemptedAt: time.Now().Add(-2 * time.Hour),
	})
	if n := client.DeliveryLog.Query().CountX(context.Background()); n != 3 {
		t.Fatalf("count after stale insert = %d, want 3", n)
	}
	if got := client.DeliveryLog.Query().Where(deliverylog.MessageIDEQ("notif:old")).CountX(context.Background()); got != 0 {
		t.Fatalf("stale row not pruned: %d", got)
	}
}

func TestDeliveryLogWriterNilDB(t *testing.T) {
	w := NewDeliveryLogWriter(func() *ent.Client { return nil })
	w.Record(DeliveryLogEntry{Surface: "mqtt", Status: "delivered"})
	var nilw *DeliveryLogWriter
	nilw.Record(DeliveryLogEntry{Surface: "mqtt", Status: "delivered"})
}

func TestWebhookMessageEventDelivers(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	client := srv.DB

	got := make(chan []byte, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- b
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hook := client.OutboundWebhook.Create().
		SetURL(ts.URL).SetSecret("").SetEnabled(true).SaveX(context.Background())

	GlobalDeliveryLog = NewDeliveryLogWriter(func() *ent.Client { return client })
	sink := &WebhookSink{
		client: ts.Client(),
		db:     func() *ent.Client { return client },
		sleep:  func(time.Duration) {},
	}
	sink.sendToTarget(hook, Event{
		Type:      EventMessageFired,
		Timestamp: time.Now(),
		Data:      Message{ID: "incident:2", Kind: MessageKindIncident, Title: "t"},
	})

	body := <-got
	if !strings.Contains(string(body), `"event":"message_fired"`) ||
		!strings.Contains(string(body), `"id":"incident:2"`) {
		t.Fatalf("webhook payload missing message: %s", body)
	}
	rows := client.DeliveryLog.Query().AllX(context.Background())
	if len(rows) != 1 || rows[0].Surface != deliverylog.SurfaceWebhook ||
		rows[0].Status != deliverylog.StatusDelivered || rows[0].MessageID != "incident:2" {
		t.Fatalf("delivery log rows = %+v", rows)
	}
}

func TestAPIDeliveryLogEndpoint(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	client := srv.DB

	client.DeliveryLog.Create().
		SetMessageID("notif:1").SetKind(MessageKindNotification).
		SetSurface(deliverylog.SurfaceMqtt).SetTarget("t").
		SetStatus(deliverylog.StatusDelivered).SaveX(context.Background())

	emitMessageFired(Message{ID: "notif:1", Kind: MessageKindNotification, CreatedAt: time.Now()})
	recordAck(3, "notif:1")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/delivery-log", srv.APIDeliveryLog)
	srv.Router = r

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/delivery-log?limit=10", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []struct {
			MessageID string `json:"message_id"`
			Surface   string `json:"surface"`
			Status    string `json:"status"`
		} `json:"deliveries"`
		DeviceAcks []DeviceAck `json:"device_acks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deliveries) != 1 || resp.Deliveries[0].MessageID != "notif:1" {
		t.Fatalf("deliveries = %+v", resp.Deliveries)
	}
	if len(resp.DeviceAcks) != 1 || resp.DeviceAcks[0].LastAckedID != "notif:1" {
		t.Fatalf("device_acks = %+v", resp.DeviceAcks)
	}
}

func TestAdminDeliveryLogPage(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)

	gin.SetMode(gin.TestMode)
	tmpl := template.Must(template.ParseFiles(
		"../web/templates/admin/flash.html",
		"../web/templates/admin/sidebar.html",
		"../web/templates/admin/delivery.html",
	))
	r := gin.New()
	r.SetHTMLTemplate(tmpl)
	r.GET("/admin/delivery", srv.AdminDeliveryLog)
	srv.Router = r

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/delivery", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Message Delivery") {
		t.Fatalf("admin page missing heading")
	}
}
