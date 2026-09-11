package handlers

import (
	"image/color"
	"testing"
	"time"
)

// alarmWakeSource builds a distinct wake source for feed tests.
func alarmWakeSource() *sourceWithName {
	return &sourceWithName{Name: "Wake", Source: &fakeColorSource{c: color.RGBA{0, 0, 255, 255}}, cacheKey: "wake:9"}
}

func TestServeFeedAlarmOverridesAndHolds(t *testing.T) {
	normal := &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}
	sources := []sourceWithName{{Name: "Normal", Source: normal, cacheKey: "normal:1"}}
	fc := &FeedController{}
	fc.SetAlarmSource(alarmWakeSource())

	srv, _ := transitionTestServer(t, sources, 60*time.Millisecond, fc, "none", 500)
	conn := dialWS(t, srv, "/ws")

	// The wake source is not in the rotation list yet must render and hold.
	for i := 0; i < 3; i++ {
		if src, _ := readFrame(t, conn)["source"].(string); src != "Wake" {
			t.Fatalf("frame %d: got source %q, want Wake", i, src)
		}
	}
}

func TestServeFeedPinWaitsForAlarmEnd(t *testing.T) {
	normal := &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}
	sources := []sourceWithName{{Name: "Normal", Source: normal, cacheKey: "normal:1"}}
	fc := &FeedController{}
	fc.Pin("normal:1", "rule1")
	fc.SetAlarmSource(alarmWakeSource())

	srv, _ := transitionTestServer(t, sources, 60*time.Millisecond, fc, "none", 500)
	conn := dialWS(t, srv, "/ws")

	if src, _ := readFrame(t, conn)["source"].(string); src != "Wake" {
		t.Fatalf("alarm should outrank pin, got %q", src)
	}
	// Alarm ends: the pin (still true) takes over.
	fc.SetAlarmSource(nil)
	if src, _ := readFrame(t, conn)["source"].(string); src != "Normal" {
		t.Fatalf("pin should resume after alarm, got %q", src)
	}
}

func TestServeFeedWSDismissAlarmReleases(t *testing.T) {
	normal := &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}
	sources := []sourceWithName{{Name: "Normal", Source: normal, cacheKey: "normal:1"}}
	fc := &FeedController{}
	fc.SetAlarmSource(alarmWakeSource())

	srv, _ := transitionTestServer(t, sources, 60*time.Millisecond, fc, "none", 500)
	conn := dialWS(t, srv, "/ws")

	if src, _ := readFrame(t, conn)["source"].(string); src != "Wake" {
		t.Fatalf("expected alarm hold, got %q", src)
	}
	if err := conn.WriteJSON(map[string]string{"action": "dismiss_alarm"}); err != nil {
		t.Fatalf("write dismiss: %v", err)
	}
	// Subsequent frames must be normal rotation.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if src, _ := readFrame(t, conn)["source"].(string); src == "Normal" {
			return
		}
	}
	t.Fatal("dismiss_alarm did not release the wake screen")
}

func TestServeFeedNotificationInterruptsThenAlarmResumes(t *testing.T) {
	priorityMu.Lock()
	notifHistory = nil
	notifID = 0
	priorityMu.Unlock()
	t.Cleanup(func() {
		priorityMu.Lock()
		notifHistory = nil
		notifID = 0
		priorityMu.Unlock()
	})

	normal := &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}
	sources := []sourceWithName{{Name: "Normal", Source: normal, cacheKey: "normal:1"}}
	fc := &FeedController{}
	fc.SetAlarmSource(alarmWakeSource())

	srv, _ := transitionTestServer(t, sources, 60*time.Millisecond, fc, "none", 500)
	conn := dialWS(t, srv, "/ws")

	if src, _ := readFrame(t, conn)["source"].(string); src != "Wake" {
		t.Fatalf("expected alarm first, got %q", src)
	}
	addToMemoryQueue("Test", "Interrupt")
	if src, _ := readFrame(t, conn)["source"].(string); src != "NOTIFICATION" {
		t.Fatalf("expected notification to interrupt, got %q", src)
	}
	if src, _ := readFrame(t, conn)["source"].(string); src != "Wake" {
		t.Fatalf("expected alarm to resume after notification, got %q", src)
	}
}

func TestServeFeedPauseStillPausesAlarm(t *testing.T) {
	normal := &fakeColorSource{c: color.RGBA{255, 0, 0, 255}}
	sources := []sourceWithName{{Name: "Normal", Source: normal, cacheKey: "normal:1"}}
	fc := &FeedController{}
	fc.SetAlarmSource(alarmWakeSource())
	fc.Pause()

	srv, _ := transitionTestServer(t, sources, 60*time.Millisecond, fc, "none", 500)
	start := time.Now()
	conn := dialWS(t, srv, "/ws")

	go func() {
		time.Sleep(150 * time.Millisecond)
		fc.Resume()
	}()

	if src, _ := readFrame(t, conn)["source"].(string); src != "Wake" {
		t.Fatalf("expected wake frame after resume, got %q", src)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("paused feed sent a frame before resume (after %v)", elapsed)
	}
}
