package handlers

import (
	"context"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestParseInputEvent covers the closed vocabulary and per-event validation:
// every accepted shape normalizes to the documented value, everything else is
// dropped with a descriptive error and never tears down the connection.
func TestParseInputEvent(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"button next press", `{"type":"input","source":"button:next","event":"press"}`, false},
		{"button pause press", `{"source":"button:pause","event":"press"}`, false},
		{"button with value rejected", `{"source":"button:next","event":"press","value":"1"}`, true},
		{"button wrong event", `{"source":"button:next","event":"tap"}`, true},
		{"encoder rotate up", `{"source":"encoder","event":"rotate","value":1}`, false},
		{"encoder rotate down", `{"source":"encoder","event":"rotate","value":-1}`, false},
		{"encoder rotate zero rejected", `{"source":"encoder","event":"rotate","value":0}`, true},
		{"encoder rotate string rejected", `{"source":"encoder","event":"rotate","value":"x"}`, true},
		{"encoder press", `{"source":"encoder","event":"press"}`, false},
		{"encoder press with value rejected", `{"source":"encoder","event":"press","value":1}`, true},
		{"nfc tap", `{"source":"nfc","event":"tap","value":"04a1b2c3"}`, false},
		{"nfc empty tag rejected", `{"source":"nfc","event":"tap","value":""}`, true},
		{"nfc missing tag rejected", `{"source":"nfc","event":"tap"}`, true},
		{"pir presence present", `{"source":"pir","event":"presence","value":"present"}`, false},
		{"pir presence absent", `{"source":"pir","event":"presence","value":"absent"}`, false},
		{"pir presence bool true", `{"source":"pir","event":"presence","value":true}`, false},
		{"mmwave presence bool false", `{"source":"mmwave","event":"presence","value":false}`, false},
		{"presence invalid string rejected", `{"source":"pir","event":"presence","value":"maybe"}`, true},
		{"presence missing value rejected", `{"source":"pir","event":"presence"}`, true},
		{"lux numeric", `{"source":"lux","event":"lux","value":123.4}`, false},
		{"lux zero ok", `{"source":"lux","event":"lux","value":0}`, false},
		{"lux negative rejected", `{"source":"lux","event":"lux","value":-1}`, true},
		{"lux absurd rejected", `{"source":"lux","event":"lux","value":1e12}`, true},
		{"lux string rejected", `{"source":"lux","event":"lux","value":"x"}`, true},
		{"lux missing rejected", `{"source":"lux","event":"lux"}`, true},
		{"unknown source", `{"source":"camera","event":"press"}`, true},
		{"unknown event", `{"source":"encoder","event":"spin","value":1}`, true},
		{"missing source", `{"event":"press"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseInputEvent([]byte(tc.raw))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %s", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.raw, err)
			}
		})
	}
}

func TestParseInputEventNormalizes(t *testing.T) {
	// Rotate steps are clamped to a sane bound.
	ev, err := ParseInputEvent([]byte(`{"source":"encoder","event":"rotate","value":999}`))
	if err != nil {
		t.Fatalf("rotate clamp parse: %v", err)
	}
	if n, ok := ev.ValueInt(); !ok || n != maxRotateStep {
		t.Fatalf("rotate clamp = %v (%v), want %d", n, ok, maxRotateStep)
	}
	ev, err = ParseInputEvent([]byte(`{"source":"encoder","event":"rotate","value":-999}`))
	if err != nil {
		t.Fatalf("rotate clamp parse: %v", err)
	}
	if n, ok := ev.ValueInt(); !ok || n != -maxRotateStep {
		t.Fatalf("rotate negative clamp = %v (%v), want %d", n, ok, -maxRotateStep)
	}

	// Boolean presence normalizes to the string vocabulary.
	ev, err = ParseInputEvent([]byte(`{"source":"pir","event":"presence","value":true}`))
	if err != nil {
		t.Fatalf("presence bool parse: %v", err)
	}
	if s, _ := ev.ValueString(); s != "present" {
		t.Fatalf("presence bool true = %q, want present", s)
	}

	// Lux survives as a finite float.
	ev, err = ParseInputEvent([]byte(`{"source":"lux","event":"lux","value":42.5}`))
	if err != nil {
		t.Fatalf("lux parse: %v", err)
	}
	if f, ok := ev.ValueFloat(); !ok || f != 42.5 {
		t.Fatalf("lux value = %v (%v), want 42.5", f, ok)
	}

	// Tags may be up to the cap; a longer one is rejected.
	tag := strings.Repeat("a", maxInputTagLen)
	if _, err := ParseInputEvent([]byte(fmt.Sprintf(`{"source":"nfc","event":"tap","value":%q}`, tag))); err != nil {
		t.Fatalf("max-length tag rejected: %v", err)
	}
	long := strings.Repeat("a", maxInputTagLen+1)
	if _, err := ParseInputEvent([]byte(fmt.Sprintf(`{"source":"nfc","event":"tap","value":%q}`, long))); err == nil {
		t.Fatalf("over-length tag accepted")
	}

	// Oversized frames are rejected before parsing.
	oversized := []byte(`{"source":"nfc","event":"tap","value":"` + strings.Repeat("a", MaxInputEventBytes) + `"}`)
	if _, err := ParseInputEvent(oversized); err == nil {
		t.Fatalf("oversized input event accepted")
	}
}

// TestDeviceInputDispatchV2 verifies the end-to-end path: a v2 device with a
// registered sink gets its validated event dispatched, while malformed events
// are dropped without disturbing the connection.
func TestDeviceInputDispatchV2(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("input-v2").
		SetToken("input-v2-token").
		SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())

	events := make(chan InputEvent, 8)
	registerInputSink(dev.ID, func(ev InputEvent) { events <- ev })
	defer unregisterInputSink(dev.ID)

	conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%s?protocol=2", dev.Token))
	defer conn.Close()
	if msg := readFrame(t, conn); msg["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", msg)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"input","source":"nfc","event":"tap","value":"04a1b2c3"}`)); err != nil {
		t.Fatalf("write tap: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Source != InputSourceNFC || ev.Event != InputEventTap {
			t.Fatalf("unexpected event: %+v", ev)
		}
		if s, _ := ev.ValueString(); s != "04a1b2c3" {
			t.Fatalf("tap value = %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("tap event never reached the sink")
	}

	// Malformed events are dropped, not fatal.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"input","source":"bogus","event":"tap"}`)); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("malformed event dispatched: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}

	// The feed is still streaming afterwards.
	if msg := readFrame(t, conn); msg["image"] == nil {
		t.Fatalf("expected frame after input traffic, got %v", msg)
	}
}

// TestDeviceInputIgnoredV1 verifies a v1 connection's input-shaped traffic is
// ignored exactly like other unsupported messages.
func TestDeviceInputIgnoredV1(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("input-v1").
		SetToken("input-v1-token").
		SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())

	events := make(chan InputEvent, 4)
	registerInputSink(dev.ID, func(ev InputEvent) { events <- ev })
	defer unregisterInputSink(dev.ID)

	conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%s", dev.Token))
	defer conn.Close()
	if msg := readFrame(t, conn); msg["image"] == nil {
		t.Fatalf("expected v1 frame, got %v", msg)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"input","source":"nfc","event":"tap","value":"04a1b2c3"}`)); err != nil {
		t.Fatalf("write input: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("v1 connection dispatched an input event: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
	if msg := readFrame(t, conn); msg["image"] == nil {
		t.Fatalf("expected v1 frame after input, got %v", msg)
	}
}

// TestDeviceInputRateLimited floods one connection: excess events are dropped
// while the feed keeps streaming.
func TestDeviceInputRateLimited(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("input-rl").
		SetToken("input-rl-token").
		SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())

	var count int32
	registerInputSink(dev.ID, func(ev InputEvent) { atomic.AddInt32(&count, 1) })
	defer unregisterInputSink(dev.ID)

	conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%s?protocol=2", dev.Token))
	defer conn.Close()
	if msg := readFrame(t, conn); msg["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", msg)
	}

	for i := 0; i < 60; i++ {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"input","source":"encoder","event":"rotate","value":1}`)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	// Give the read loop a moment to drain, then confirm throttling.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := atomic.LoadInt32(&count)
	if got < 1 {
		t.Fatalf("expected at least one dispatched event")
	}
	if got > 45 {
		t.Fatalf("rate limit ineffective: dispatched %d of 60", got)
	}
	if msg := readFrame(t, conn); msg["image"] == nil {
		t.Fatalf("expected frame after flood, got %v", msg)
	}
}

// TestDeviceInputPreviewIgnored verifies the admin preview feed (protocol 0)
// can never dispatch input events even with a registered sink.
func TestDeviceInputPreviewIgnored(t *testing.T) {
	events := make(chan InputEvent, 4)
	registerInputSink(42, func(ev InputEvent) { events <- ev })
	defer unregisterInputSink(42)

	src := &fakeColorSource{c: color.RGBA{200, 200, 200, 255}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		sources := []sourceWithName{{Name: "White", Source: src, cacheKey: "white:preview"}}
		serveFeed(conn, feedConn{deviceID: 42, protocol: 0}, sources, false, 80*time.Millisecond, 16, 16, &FeedController{DeviceID: 42}, "none", 500, nil)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"input","source":"nfc","event":"tap","value":"04a1b2c3"}`)); err != nil {
		t.Fatalf("write input: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("preview feed dispatched an input event: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}
