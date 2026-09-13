package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"ledit/datasource"
)

// TestDeviceProtocolNegotiation covers the v1/v2 handshake matrix: only the
// exact string "2" opts into v2, and a v2 connection is welcomed before any
// frame. Everything else stays v1 with no welcome and no brightness.
func TestDeviceProtocolNegotiation(t *testing.T) {
	cases := []struct {
		name  string
		query string
		v2    bool
	}{
		{"missing", "", false},
		{"v1", "?protocol=1", false},
		{"v2", "?protocol=2", true},
		{"unknown", "?protocol=999", false},
		{"malformed", "?protocol=abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, client := previewTestServer(t)
			seedGeneralSettings(t, client)
			dev := client.DeviceSettings.Create().
				SetName("proto-" + tc.name).
				SetToken("proto-" + tc.name).
				SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
				SaveX(context.Background())

			conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%s%s", dev.Token, tc.query))

			msg := readFrame(t, conn)
			if tc.v2 {
				if msg["type"] != "welcome" {
					t.Fatalf("v2 first message must be welcome, got %v", msg)
				}
				if msg["protocol"] != float64(2) {
					t.Fatalf("welcome protocol = %v", msg["protocol"])
				}
				caps, _ := msg["capabilities"].([]any)
				got := map[string]bool{}
				for _, c := range caps {
					got[fmt.Sprint(c)] = true
				}
				for _, want := range []string{"brightness", "spectrum", "hold"} {
					if !got[want] {
						t.Fatalf("welcome missing capability %q in %v", want, caps)
					}
				}
				frame := readFrame(t, conn)
				if _, ok := frame["image"]; !ok {
					t.Fatalf("expected frame after welcome, got %v", frame)
				}
			} else {
				if _, ok := msg["type"]; ok {
					t.Fatalf("v1 must not receive a welcome, got %v", msg)
				}
				if _, ok := msg["image"]; !ok {
					t.Fatalf("expected v1 frame, got %v", msg)
				}
				if _, ok := msg["brightness"]; ok {
					t.Fatalf("v1 frame must not carry brightness, got %v", msg)
				}
			}
		})
	}
}

// TestFeedBrightnessHint verifies the v2 brightness field is present only when
// the effective level changed, and never on v1.
func TestFeedBrightnessHint(t *testing.T) {
	levels := []int{40, 40, 70, 70}
	var idx int32
	bFn := func() int {
		i := int(atomic.LoadInt32(&idx))
		if i >= len(levels) {
			return levels[len(levels)-1]
		}
		v := levels[i]
		atomic.AddInt32(&idx, 1)
		return v
	}

	run := func(protocol int) []map[string]any {
		src := &fakeColorSource{c: color.RGBA{200, 200, 200, 255}}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _ := upgrader.Upgrade(w, r, nil)
			defer conn.Close()
			sources := []sourceWithName{{Name: "White", Source: src, cacheKey: fmt.Sprintf("white:proto%d", protocol)}}
			serveFeed(conn, feedConn{protocol: protocol}, sources, false, 80*time.Millisecond, 16, 16, &FeedController{}, "none", 500, bFn)
		}))
		defer srv.Close()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		var out []map[string]any
		for i := 0; i < 3; i++ {
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read frame %d: %v", i, err)
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("unmarshal frame %d: %v", i, err)
			}
			out = append(out, m)
		}
		return out
	}

	// v2: frame1 carries the new level, frame2 unchanged (absent), frame3 changed again.
	frames := run(2)
	if frames[0]["brightness"] != float64(40) {
		t.Fatalf("frame1 brightness = %v, want 40", frames[0]["brightness"])
	}
	if _, ok := frames[1]["brightness"]; ok {
		t.Fatalf("frame2 must omit unchanged brightness, got %v", frames[1]["brightness"])
	}
	if frames[2]["brightness"] != float64(70) {
		t.Fatalf("frame3 brightness = %v, want 70", frames[2]["brightness"])
	}

	// v1: never any brightness field.
	for i, m := range run(0) {
		if _, ok := m["brightness"]; ok {
			t.Fatalf("v1 frame %d must not carry brightness: %v", i, m)
		}
	}
}

func TestParseSpectrumBins(t *testing.T) {
	valid := make([]any, 16)
	for i := range valid {
		valid[i] = float64(i * 16)
	}
	if bins, ok := parseSpectrumBins(valid); !ok || len(bins) != 16 {
		t.Fatalf("valid 16 bins rejected: %v %v", bins, ok)
	}
	valid32 := make([]any, 32)
	for i := range valid32 {
		valid32[i] = float64(255)
	}
	if _, ok := parseSpectrumBins(valid32); !ok {
		t.Fatal("valid 32 bins rejected")
	}

	tooFew := []any{float64(1), float64(2)}
	if _, ok := parseSpectrumBins(tooFew); ok {
		t.Fatal("2 bins must be rejected")
	}
	tooMany := make([]any, 33)
	if _, ok := parseSpectrumBins(tooMany); ok {
		t.Fatal("33 bins must be rejected")
	}
	if _, ok := parseSpectrumBins([]any{float64(256), float64(1), float64(1)}); ok {
		t.Fatal("out-of-range bins must be rejected")
	}
	over := make([]any, 16)
	for i := range over {
		over[i] = float64(1)
	}
	over[3] = float64(256)
	if _, ok := parseSpectrumBins(over); ok {
		t.Fatal("value >255 must be rejected")
	}
	over[3] = float64(-1)
	if _, ok := parseSpectrumBins(over); ok {
		t.Fatal("negative value must be rejected")
	}
	over[3] = float64(1.5)
	if _, ok := parseSpectrumBins(over); ok {
		t.Fatal("non-integer value must be rejected")
	}
	over[3] = "x"
	if _, ok := parseSpectrumBins(over); ok {
		t.Fatal("non-numeric value must be rejected")
	}
	if _, ok := parseSpectrumBins("nope"); ok {
		t.Fatal("non-array must be rejected")
	}
}

// TestHoldGestureNoErrorAndV1Unaffected sends a hold control message and then
// confirms the feed keeps streaming; v1 next/pause handling is covered by the
// existing button tests but hold must not tear the connection down.
func TestHoldGestureNoErrorAndV1Unaffected(t *testing.T) {
	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("hold-dev").SetToken("hold-token").
		SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())

	conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%s?protocol=2", dev.Token))
	welcome := readFrame(t, conn)
	if welcome["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", welcome)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"hold"}`)); err != nil {
		t.Fatalf("write hold: %v", err)
	}
	// Connection must remain usable and keep sending frames.
	frame := readFrame(t, conn)
	if _, ok := frame["image"]; !ok {
		t.Fatalf("expected a frame after hold, got %v", frame)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"next"}`)); err != nil {
		t.Fatalf("write next after hold: %v", err)
	}
}

// TestSpectrumIngestionV2 exercises the full WS path: a v2 device sends valid
// spectrum bins and the server stores them; a v1 device's bins are ignored.
func TestSpectrumIngestionV2(t *testing.T) {
	datasource.SetVisualizerBins(nil)
	defer datasource.SetVisualizerBins(nil)

	srv, client := previewTestServer(t)
	seedGeneralSettings(t, client)
	dev := client.DeviceSettings.Create().
		SetName("spec-dev").SetToken("spec-token").
		SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())

	conn := dialWS(t, srv, fmt.Sprintf("/ws/device/%s?protocol=2", dev.Token))
	if welcome := readFrame(t, conn); welcome["type"] != "welcome" {
		t.Fatalf("expected welcome, got %v", welcome)
	}
	bins := make([]int, 16)
	for i := range bins {
		bins[i] = i * 10
	}
	payload, _ := json.Marshal(map[string]any{"type": "spectrum", "bins": bins})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write spectrum: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if got, ok := datasource.FreshVisualizerBins(time.Second); ok {
			if len(got) != 16 || got[1] != 10 {
				t.Fatalf("unexpected stored bins: %v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("v2 spectrum bins were not ingested")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// v1: same message must be ignored.
	datasource.SetVisualizerBins(nil)
	dev1 := client.DeviceSettings.Create().
		SetName("spec-v1").SetToken("spec-v1-token").
		SetWidth(32).SetHeight(32).SetRefreshInterval(1).SetEnabled(true).
		SaveX(context.Background())
	conn1 := dialWS(t, srv, fmt.Sprintf("/ws/device/%s", dev1.Token))
	if frame := readFrame(t, conn1); frame["type"] != nil {
		t.Fatalf("v1 must not get welcome, got %v", frame)
	}
	if err := conn1.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write v1 spectrum: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, ok := datasource.FreshVisualizerBins(time.Second); ok {
		t.Fatal("v1 spectrum must be ignored")
	}
}
