package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

// previewTestServer spins up an in-memory ent client and a gin router exposing
// both the device preview endpoint and the hardware device endpoint. Each test
// gets its own uniquely-named in-memory database.
func previewTestServer(t *testing.T) (*httptest.Server, *ent.Client) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// Serialize all access through one connection: shared-cache in-memory
	// SQLite reports "database table is locked" (SQLITE_LOCKED) when pooled
	// connections race — e.g. the device feed goroutine writing last_seen_at
	// while the test reads — and _busy_timeout does not retry SQLITE_LOCKED.
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	hub := NewWSHub(client)
	r.GET("/ws/device/:token/preview", hub.HandleDevicePreviewWS)
	r.GET("/ws/device/:token", hub.HandleDeviceWS)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, client
}

func seedGeneralSettings(t *testing.T, client *ent.Client) {
	t.Helper()
	if _, err := client.GeneralSettings.Create().
		SetTimeout(1.0).SetRandom(false).SetWidth(64).SetHeight(64).
		Save(context.Background()); err != nil {
		t.Fatalf("create general settings: %v", err)
	}
}

func dialWS(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	return msg
}

// Task 1.4: the preview endpoint must never write last_seen_at, even across a
// full connect/stream/disconnect cycle.
func TestPreviewWSDoesNotMarkLastSeen(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("Kitchen").SetWidth(32).SetHeight(64).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())

	conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%d/preview", dev.ID))
	msg := readFrame(t, conn)
	if _, ok := msg["image"]; !ok {
		t.Fatalf("expected an image frame, got %v", msg)
	}
	conn.Close()
	time.Sleep(200 * time.Millisecond) // give any disconnect handler time to run

	fresh, err := client.DeviceSettings.Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if fresh.LastSeenAt != nil {
		t.Fatalf("preview must NOT mark last_seen_at, got %v", *fresh.LastSeenAt)
	}
}

// Task 1.4: missing device id yields 404.
func TestPreviewWSRejectsMissingDevice(t *testing.T) {
	srv, _ := previewTestServer(t)
	resp, err := http.Get(srv.URL + "/ws/device/999/preview")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing device, got %d", resp.StatusCode)
	}
}

// Task 1.4: disabled device yields 403.
func TestPreviewWSRejectsDisabledDevice(t *testing.T) {
	srv, client := previewTestServer(t)
	dev := client.DeviceSettings.Create().
		SetName("Off").SetEnabled(false).
		SaveX(context.Background())
	resp, err := http.Get(srv.URL + fmt.Sprintf("/ws/device/%d/preview", dev.ID))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled device, got %d", resp.StatusCode)
	}
}

// Task 1.4: invalid device id yields 400.
func TestPreviewWSRejectsInvalidID(t *testing.T) {
	srv, _ := previewTestServer(t)
	resp, err := http.Get(srv.URL + "/ws/device/not-a-number/preview")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", resp.StatusCode)
	}
}

// Task 4.3: the hardware device feed (token-authed) must still mark
// last_seen_at on connect and clear it on disconnect — untouched by the
// preview refactor.
func TestDeviceWSStillMarksLastSeen(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("Hardware").SetWidth(64).SetHeight(64).SetRefreshInterval(1).SetEnabled(true).
		SetToken("hw-token").
		SaveX(context.Background())

	conn := dialWS(t, srv, "/ws/device/hw-token")
	readFrame(t, conn) // ensure the feed is actually streaming

	fresh, err := client.DeviceSettings.Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if fresh.LastSeenAt == nil {
		t.Fatal("hardware feed must mark last_seen_at on connect")
	}

	conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fresh, _ = client.DeviceSettings.Get(context.Background(), dev.ID)
		if fresh.LastSeenAt == nil {
			return // cleared on disconnect as before
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("hardware feed must clear last_seen_at on disconnect")
}

// Task 2.1: the hardware device feed increments the persisted frames_served
// counter once per served frame.
func TestDeviceWSIncrementsFramesServed(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("Hardware").SetWidth(64).SetHeight(64).SetRefreshInterval(1).SetEnabled(true).
		SetToken("frames-token").
		SaveX(context.Background())

	conn := dialWS(t, srv, "/ws/device/frames-token")
	readFrame(t, conn) // ensure the feed is actually streaming

	fresh, err := client.DeviceSettings.Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if fresh.FramesServed < 1 {
		t.Fatalf("expected frames_served >= 1, got %d", fresh.FramesServed)
	}
	conn.Close()
}

// Task 4.1: the serveFeed refactor must keep /ws/feed and /ws/device/:token
// byte-identical. The only behavioral input is the feedConn cache-key prefix:
// the zero value must reproduce the legacy key exactly, and a preview must be
// namespaced per device.
func TestFeedConnCacheKeyNamespacing(t *testing.T) {
	legacy := lkgCacheKey(feedConn{}.cacheKeyPrefix+"systemstats:0", 64, 64)
	if legacy != "systemstats:0@64x64" {
		t.Fatalf("legacy cache key changed: %s", legacy)
	}
	preview := lkgCacheKey(feedConn{cacheKeyPrefix: "device:7:"}.cacheKeyPrefix+"systemstats:0", 32, 64)
	if preview != "device:7:systemstats:0@32x64" {
		t.Fatalf("unexpected preview cache key: %s", preview)
	}
}
