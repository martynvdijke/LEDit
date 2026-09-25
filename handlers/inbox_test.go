package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/deliverylog"
	"ledit/ent/devicemessagestate"
)

func setGlobalDBForTest(t *testing.T, srv *Server) {
	t.Helper()
	prev := globalServerDB
	globalServerDB = func() *ent.Client { return srv.DB }
	prevLog := GlobalDeliveryLog
	GlobalDeliveryLog = NewDeliveryLogWriter(func() *ent.Client { return srv.DB })
	t.Cleanup(func() { globalServerDB = prev; GlobalDeliveryLog = prevLog })
}

func TestInboxPersistedRoundTrip(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	setGlobalDBForTest(t, srv)
	entry := srv.AddNotification("t", "b")
	id := "notif:" + strconv.Itoa(entry.ID)
	emitMessageFired(NotificationToMessage(entry))
	recordAck(1, id)
	time.Sleep(100 * time.Millisecond)
	rows := srv.DB.DeviceMessageState.Query().Where(devicemessagestate.DeviceID(1)).AllX(context.Background())
	if len(rows) != 1 || rows[0].Status != "acked" {
		t.Fatalf("acked rows=%+v", rows)
	}
	recordRead(1, id)
	time.Sleep(100 * time.Millisecond)
	row := srv.DB.DeviceMessageState.Query().Where(devicemessagestate.DeviceID(1), devicemessagestate.MessageIDEQ(id)).OnlyX(context.Background())
	if row.Status != "read" {
		t.Fatalf("read status=%s", row.Status)
	}
	recordDismiss(1, id)
	time.Sleep(100 * time.Millisecond)
	row = srv.DB.DeviceMessageState.Query().Where(devicemessagestate.DeviceID(1), devicemessagestate.MessageIDEQ(id)).OnlyX(context.Background())
	if row.Status != "dismissed" {
		t.Fatalf("dismissed status=%s", row.Status)
	}
	recordAck(1, "notif:9999")
	time.Sleep(50 * time.Millisecond)
	if n := srv.DB.DeviceMessageState.Query().Where(devicemessagestate.MessageIDEQ("notif:9999")).CountX(context.Background()); n != 0 {
		t.Fatalf("unknown id persisted")
	}
}

func TestUnreadMessagesFor(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	setGlobalDBForTest(t, srv)
	e1 := srv.AddNotification("a", "1")
	e2 := srv.AddNotification("b", "2")
	id1 := "notif:" + strconv.Itoa(e1.ID)
	emitMessageFired(NotificationToMessage(e1))
	emitMessageFired(NotificationToMessage(e2))
	if got := UnreadMessagesFor(5); len(got) != 2 {
		t.Fatalf("unread len=%d want 2", len(got))
	}
	recordRead(5, id1)
	time.Sleep(100 * time.Millisecond)
	got := UnreadMessagesFor(5)
	for _, m := range got {
		if m.ID == id1 {
			t.Fatalf("read message still in unread")
		}
	}
	if len(got) != 1 {
		t.Fatalf("unread len=%d want 1", len(got))
	}
}

func TestInboundDeliveryLogCorrelation(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	setGlobalDBForTest(t, srv)
	srv.DeliverInbound(InboundMessage{Source: "ntfy", SourceID: "chan1", Title: "hello", Body: "world", TTL: time.Minute})
	time.Sleep(150 * time.Millisecond)
	rows := srv.DB.DeliveryLog.Query().Where(deliverylog.SurfaceEQ(deliverylog.SurfaceInbound)).AllX(context.Background())
	if len(rows) != 1 {
		t.Fatalf("delivery rows=%d", len(rows))
	}
	if len(rows[0].MessageID) < 6 || rows[0].MessageID[:6] != "notif:" || rows[0].Kind != "notification" {
		t.Fatalf("inbound correlation wrong: %+v", rows[0])
	}
}

func TestAPINotificationFiltering(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	setGlobalDBForTest(t, srv)
	srv.AddNotification("low", "a", WithPriority(0))
	srv.AddNotification("high", "b", WithPriority(3))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/notifications", srv.APINotificationHistory)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/notifications?priority_min=3", nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp []notifEntry
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %v", err)
	}
	if len(resp) != 1 || resp[0].Title != "high" {
		t.Fatalf("filter priority_min failed %+v", resp)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/notifications?kind=incident", nil))
	var resp2 []notifEntry
	json.Unmarshal(w.Body.Bytes(), &resp2)
	if len(resp2) != 0 {
		t.Fatalf("kind incident should be empty %+v", resp2)
	}
	e := srv.AddNotification("unread_test", "c")
	id := "notif:" + strconv.Itoa(e.ID)
	emitMessageFired(NotificationToMessage(e))
	recordRead(99, id)
	time.Sleep(100 * time.Millisecond)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/notifications?unread_for=99", nil))
	var resp3 []notifEntry
	json.Unmarshal(w.Body.Bytes(), &resp3)
	for _, n := range resp3 {
		if n.ID == e.ID {
			t.Fatalf("unread_for should exclude read")
		}
	}
}

func TestAPIDeliveryLogFilters(t *testing.T) {
	isolateMessages(t)
	srv := newTestServerWithDB(t)
	srv.DB.DeliveryLog.Create().SetMessageID("notif:1").SetKind("notification").SetSurface(deliverylog.SurfaceMqtt).SetStatus(deliverylog.StatusDelivered).SaveX(context.Background())
	srv.DB.DeliveryLog.Create().SetMessageID("notif:2").SetKind("notification").SetSurface(deliverylog.SurfaceInbound).SetStatus(deliverylog.StatusFailed).SaveX(context.Background())
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/delivery-log", srv.APIDeliveryLog)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/delivery-log?surface=mqtt", nil))
	var resp struct {
		Deliveries []*struct {
			Surface string `json:"surface"`
		} `json:"deliveries"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Deliveries) != 1 || resp.Deliveries[0].Surface != "mqtt" {
		t.Fatalf("surface filter failed %+v", resp)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/delivery-log?status=failed", nil))
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Deliveries) != 1 {
		t.Fatalf("status filter failed")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/delivery-log?surface=unknown", nil))
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Deliveries) != 2 {
		t.Fatalf("unknown surface should be ignored len=%d", len(resp.Deliveries))
	}
}
