package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"ledit/ent"
)

func TestParseThenAction(t *testing.T) {
	cases := []struct {
		raw     string
		kind    string
		wantErr bool
	}{
		{"", "none", false},
		{"{}", "none", false},
		{`{"kind":"none"}`, "none", false},
		{`{"kind":"scene","scene_id":5}`, "scene", false},
		{`{"kind":"unknown"}`, "", true},
	}
	for _, c := range cases {
		ta, err := ParseThenAction(c.raw)
		if c.wantErr && err == nil {
			t.Fatalf("ParseThenAction %q expected error", c.raw)
		}
		if !c.wantErr && err != nil {
			t.Fatalf("ParseThenAction %q unexpected error: %v", c.raw, err)
		}
		if !c.wantErr && ta.Kind != c.kind {
			t.Fatalf("ParseThenAction %q kind=%q want %q", c.raw, ta.Kind, c.kind)
		}
	}
}

func TestValidateThenAction(t *testing.T) {
	if msg := validateThenAction(ThenAction{Kind: "scene"}); msg == "" {
		t.Fatal("scene without id should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "scene", SceneID: 1}); msg != "" {
		t.Fatalf("scene with id should pass, got %q", msg)
	}
	if msg := validateThenAction(ThenAction{Kind: "notification"}); msg == "" {
		t.Fatal("notification without message should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "notification", Message: "hi"}); msg != "" {
		t.Fatalf("notification with message should pass, got %q", msg)
	}
	if msg := validateThenAction(ThenAction{Kind: "notification", Message: "   "}); msg == "" {
		t.Fatal("notification whitespace message should fail")
	}
}

func TestParseThenActionNewKinds(t *testing.T) {
	cases := []struct{ raw, kind string }{
		{`{"kind":"brightness","device_id":3,"level":40}`, "brightness"},
		{`{"kind":"source","source_type":"dataset","source_id":7}`, "source"},
		{`{"kind":"playlist","device_id":1,"playlist_id":2}`, "playlist"},
		{`{"kind":"mqtt","topic":"ledit/test"}`, "mqtt"},
		{`{"kind":"http","url":"https://example.com/hook"}`, "http"},
	}
	for _, c := range cases {
		ta, err := ParseThenAction(c.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", c.raw, err)
		}
		if ta.Kind != c.kind {
			t.Fatalf("kind %q want %q", ta.Kind, c.kind)
		}
	}
}

func TestValidateThenActionNewKinds(t *testing.T) {
	level := 40
	if msg := validateThenAction(ThenAction{Kind: "brightness"}); msg == "" {
		t.Fatal("brightness without level should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "brightness", Level: &level, DeviceID: -1}); msg == "" {
		t.Fatal("negative device id should fail")
	}
	bad := 101
	if msg := validateThenAction(ThenAction{Kind: "brightness", Level: &bad}); msg == "" {
		t.Fatal("level 101 should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "brightness", Level: &level}); msg != "" {
		t.Fatalf("valid brightness should pass: %s", msg)
	}

	if msg := validateThenAction(ThenAction{Kind: "source"}); msg == "" {
		t.Fatal("source without type should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "source", SourceType: "dataset"}); msg == "" {
		t.Fatal("source without id should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "source", SourceType: "dataset", SourceID: 2}); msg != "" {
		t.Fatalf("valid source should pass: %s", msg)
	}

	if msg := validateThenAction(ThenAction{Kind: "playlist", PlaylistID: 1}); msg == "" {
		t.Fatal("playlist without device should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "playlist", DeviceID: 1}); msg == "" {
		t.Fatal("playlist without playlist id should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "playlist", DeviceID: 1, PlaylistID: 2}); msg != "" {
		t.Fatalf("valid playlist should pass: %s", msg)
	}

	if msg := validateThenAction(ThenAction{Kind: "mqtt"}); msg == "" {
		t.Fatal("mqtt without topic should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "mqtt", Topic: "a/+/b"}); msg == "" {
		t.Fatal("wildcard topic should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "mqtt", Topic: "$SYS/x"}); msg == "" {
		t.Fatal("$ topic should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "mqtt", Topic: "ledit/x", Payload: "[1]"}); msg == "" {
		t.Fatal("array payload should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "mqtt", Topic: "ledit/x", Payload: `{"a":1}`}); msg != "" {
		t.Fatalf("valid mqtt should pass: %s", msg)
	}

	if msg := validateThenAction(ThenAction{Kind: "http", URL: "ftp://x/y"}); msg == "" {
		t.Fatal("non-http url should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "http", URL: "https://example.com/hook", Method: "TRACE"}); msg == "" {
		t.Fatal("unsupported method should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "http", URL: "https://example.com/hook"}); msg != "" {
		t.Fatalf("valid http should pass: %s", msg)
	}
}

func TestThenActionLegacyRoundTrip(t *testing.T) {
	raws := []string{
		`{"kind":"scene","scene_id":5}`,
		`{"kind":"notification","title":"t","message":"m","ttl_seconds":10}`,
		`{"kind":"webhook","event":"my_event","payload":"{\"a\":1}"}`,
	}
	for _, raw := range raws {
		ta, err := ParseThenAction(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		out := marshalThenAction(ta)
		var want, got map[string]any
		if err := json.Unmarshal([]byte(raw), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("marshal round trip %q -> %q: %v", raw, out, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("round trip mismatch: %s -> %s", raw, out)
		}
	}
	if got := marshalThenAction(ThenAction{Kind: "none"}); got != "{}" {
		t.Fatalf("none should marshal to {}, got %s", got)
	}
}

func TestExecuteThenActionBrightness(t *testing.T) {
	srv := newTestServerWithDB(t)
	devID := createActuationDevice(t, srv, "d-brightness")
	fake := &fakeClient{connected: true}
	setActuationMqtt(t, fake)

	rule := &ent.DisplayRule{Name: "dim-it", ThenActions: fmt.Sprintf(`{"kind":"brightness","device_id":%d,"level":25}`, devID)}
	executeThenAction(context.Background(), srv, rule)

	dev := srv.DB.DeviceSettings.GetX(srv.Ctx, devID)
	if dev.BrightnessOverride == nil || *dev.BrightnessOverride != 25 {
		t.Fatalf("override = %v, want 25", dev.BrightnessOverride)
	}
	if !hasActuationPublish(fake.published, fmt.Sprintf("ledit/device/%d/brightness/state", devID), "25", true) {
		t.Fatalf("brightness state not published: %+v", fake.published)
	}

	// device_id 0 applies to enabled devices only
	srv2 := newTestServerWithDB(t)
	enabledID := createActuationDevice(t, srv2, "d-enabled")
	disabled := srv2.DB.DeviceSettings.Create().SetName("d-disabled").SetWidth(32).SetHeight(32).SetTransport("websocket").SetEnabled(false).SaveX(srv2.Ctx)
	rule2 := &ent.DisplayRule{Name: "all-off", ThenActions: `{"kind":"brightness","device_id":0,"level":0}`}
	executeThenAction(context.Background(), srv2, rule2)
	en := srv2.DB.DeviceSettings.GetX(srv2.Ctx, enabledID)
	if en.BrightnessOverride == nil || *en.BrightnessOverride != 0 {
		t.Fatalf("enabled device override = %v, want 0", en.BrightnessOverride)
	}
	dis := srv2.DB.DeviceSettings.GetX(srv2.Ctx, disabled.ID)
	if dis.BrightnessOverride != nil {
		t.Fatalf("disabled device should be untouched, got %v", dis.BrightnessOverride)
	}
}

func TestExecuteThenActionSourcePin(t *testing.T) {
	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { unpinAll(); leaveController(fc) })

	rule := &ent.DisplayRule{Name: "pin-it", ThenActions: `{"kind":"source","source_type":"dataset","source_id":42}`}
	executeThenAction(context.Background(), nil, rule)

	key, by, ok := fc.IsPinned()
	if !ok || key != "dataset:42" || by != "rule:pin-it" {
		t.Fatalf("pin = (%q,%q,%v)", key, by, ok)
	}
	unpinAll()
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("unpin should clear")
	}
}

func TestExecuteThenActionPlaylist(t *testing.T) {
	srv := newTestServerWithDB(t)
	devID := createActuationDevice(t, srv, "d-playlist")
	pl := srv.DB.Playlist.Create().SetName("pl-rule").SetItems("[]").SetEnabled(true).SaveX(srv.Ctx)
	rule := &ent.DisplayRule{Name: "switch", ThenActions: fmt.Sprintf(`{"kind":"playlist","device_id":%d,"playlist_id":%d}`, devID, pl.ID)}
	executeThenAction(context.Background(), srv, rule)
	dev := srv.DB.DeviceSettings.GetX(srv.Ctx, devID)
	if dev.ContentMode != "playlist" || dev.PlaylistID == nil || *dev.PlaylistID != pl.ID {
		t.Fatalf("content mode = %q playlist = %v", dev.ContentMode, dev.PlaylistID)
	}

	pl2 := srv.DB.Playlist.Create().SetName("pl-disabled").SetItems("[]").SetEnabled(false).SaveX(srv.Ctx)
	rule2 := &ent.DisplayRule{Name: "switch2", ThenActions: fmt.Sprintf(`{"kind":"playlist","device_id":%d,"playlist_id":%d}`, devID, pl2.ID)}
	executeThenAction(context.Background(), srv, rule2)
	dev2 := srv.DB.DeviceSettings.GetX(srv.Ctx, devID)
	if dev2.PlaylistID == nil || *dev2.PlaylistID != pl.ID {
		t.Fatalf("disabled playlist should not switch, got %v", dev2.PlaylistID)
	}
}

func TestExecuteThenActionMQTT(t *testing.T) {
	fake := &fakeClient{connected: true}
	setActuationMqtt(t, fake)
	rule := &ent.DisplayRule{Name: "notify-ha", ThenActions: `{"kind":"mqtt","topic":"ledit/rules/fired","retain":true}`}
	executeThenAction(context.Background(), nil, rule)
	if len(fake.published) != 1 {
		t.Fatalf("published %d, want 1: %+v", len(fake.published), fake.published)
	}
	p := fake.published[0]
	if p.topic != "ledit/rules/fired" || !p.retained {
		t.Fatalf("publish = %+v", p)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(p.payload), &env); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if env["event"] != EventRuleTriggered || env["rule"] != "notify-ha" {
		t.Fatalf("envelope = %+v", env)
	}

	fake2 := &fakeClient{connected: true}
	setActuationMqtt(t, fake2)
	rule2 := &ent.DisplayRule{Name: "r2", ThenActions: `{"kind":"mqtt","topic":"ledit/rules/custom","payload":"{\"a\":1}"}`}
	executeThenAction(context.Background(), nil, rule2)
	if len(fake2.published) != 1 || fake2.published[0].payload != `{"a":1}` {
		t.Fatalf("custom payload publish = %+v", fake2.published)
	}

	fake3 := &fakeClient{connected: false}
	setActuationMqtt(t, fake3)
	executeThenAction(context.Background(), nil, rule)
	if len(fake3.published) != 0 {
		t.Fatalf("disconnected should not publish: %+v", fake3.published)
	}
}

func TestExecuteThenActionHTTP(t *testing.T) {
	got := make(chan *http.Request, 4)
	bodies := make(chan []byte, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r
		bodies <- b
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	oldClient, oldSleep := ruleHTTPClient, ruleHTTPSleep
	ruleHTTPClient = srv.Client()
	ruleHTTPSleep = func(time.Duration) {}
	t.Cleanup(func() { ruleHTTPClient = oldClient; ruleHTTPSleep = oldSleep })

	rule := &ent.DisplayRule{ID: 7, Name: "hook", SourceType: "systemstats", SourceID: 3, ThenActions: fmt.Sprintf(`{"kind":"http","url":%q,"secret":"s3cret","event":"custom_event"}`, srv.URL)}
	executeThenAction(context.Background(), nil, rule)

	select {
	case req := <-got:
		body := <-bodies
		if req.Method != http.MethodPost {
			t.Fatalf("method = %q", req.Method)
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q", req.Header.Get("Content-Type"))
		}
		if req.Header.Get("X-LEDit-Event") != "custom_event" {
			t.Fatalf("event header = %q", req.Header.Get("X-LEDit-Event"))
		}
		if want := signRuleHTTPBody("s3cret", body); req.Header.Get("X-LEDit-Signature") != want {
			t.Fatalf("signature = %q want %q", req.Header.Get("X-LEDit-Signature"), want)
		}
		var env map[string]any
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatal(err)
		}
		if env["event"] != "custom_event" {
			t.Fatalf("env event = %v", env["event"])
		}
		data, _ := env["data"].(map[string]any)
		ruleData, _ := data["rule"].(map[string]any)
		if ruleData["name"] != "hook" {
			t.Fatalf("rule name = %v", ruleData["name"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no http delivery")
	}
}

func TestExecuteThenActionHTTPRetry(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		close(done)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	oldClient, oldSleep := ruleHTTPClient, ruleHTTPSleep
	ruleHTTPClient = srv.Client()
	ruleHTTPSleep = func(time.Duration) {}
	t.Cleanup(func() { ruleHTTPClient = oldClient; ruleHTTPSleep = oldSleep })

	rule := &ent.DisplayRule{Name: "retry", ThenActions: fmt.Sprintf(`{"kind":"http","url":%q}`, srv.URL)}
	executeThenAction(context.Background(), nil, rule)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("no successful retry; attempts=%d", attempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Fatalf("attempts = %d, want >= 2", attempts)
	}
}

func TestExecuteThenActionNoServerSafe(t *testing.T) {
	oldSleep := ruleHTTPSleep
	ruleHTTPSleep = func(time.Duration) {}
	t.Cleanup(func() { ruleHTTPSleep = oldSleep })

	rules := []string{
		`{"kind":"brightness","device_id":1,"level":50}`,
		`{"kind":"playlist","device_id":1,"playlist_id":1}`,
		`{"kind":"source","source_type":"dataset","source_id":1}`,
		`{"kind":"mqtt","topic":"ledit/x"}`,
		`{"kind":"http","url":"http://127.0.0.1:1/hook"}`,
	}
	for _, raw := range rules {
		executeThenAction(context.Background(), nil, &ent.DisplayRule{Name: "nil-safe", ThenActions: raw})
	}
}
