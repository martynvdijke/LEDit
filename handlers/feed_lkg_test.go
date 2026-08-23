package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestFeedStaleFlag drives serveFeed over a real WebSocket with a source that
// succeeds once then starts failing. The first frame must be live (no stale
// flag); the second cycle must serve the cached frame marked stale.
func TestFeedStaleFlag(t *testing.T) {
	ds := &toggleDS{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		sources := []sourceWithName{{Name: "Toggle", Source: ds, cacheKey: "toggle:1"}}
		serveFeed(conn, feedConn{}, sources, false, 50*time.Millisecond, 64, 64, &FeedController{}, "none", 500)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:]+"/feed", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Frame 1: live render, no stale flag.
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame 1: %v", err)
	}
	var f1 map[string]any
	json.Unmarshal(msg, &f1)
	if f1["source"] != "Toggle" {
		t.Fatalf("frame 1 source = %v", f1["source"])
	}
	if _, ok := f1["stale"]; ok {
		t.Fatalf("frame 1 must be live, got stale field: %v", f1)
	}

	// Frame 2: source now fails → cached frame served stale.
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame 2: %v", err)
	}
	var f2 map[string]any
	json.Unmarshal(msg, &f2)
	if f2["source"] != "Toggle" {
		t.Fatalf("frame 2 source = %v", f2["source"])
	}
	if stale, ok := f2["stale"].(bool); !ok || !stale {
		t.Fatalf("frame 2 must be stale=true, got: %v", f2)
	}
	if _, ok := f2["stale_age"]; !ok {
		t.Fatalf("frame 2 must carry stale_age, got: %v", f2)
	}
}
