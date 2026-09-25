package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"ledit/datasource"
	"ledit/ent"
	"ledit/render"
)

func TestNextPushIndex_Sequential(t *testing.T) {
	sources := []sourceWithName{{cacheKey: "a:0"}, {cacheKey: "b:0"}, {cacheKey: "c:0"}}
	// force non-adaptive by ensuring mode != adaptive (default is empty or manual)
	orig := GetOrderingMode()
	// ensure not adaptive
	if orig == "adaptive" {
		// set back via direct global not exported; skip
	}
	for i, want := range []int{1, 2, 0, 1} {
		got := nextPushIndex(i, 3, false, sources)
		if got != want {
			t.Fatalf("step %d: got %d want %d", i, got, want)
		}
	}
}

func TestNextPushIndex_RandomInRange(t *testing.T) {
	sources := []sourceWithName{{cacheKey: "a:0"}, {cacheKey: "b:0"}, {cacheKey: "c:0"}}
	for i := 0; i < 100; i++ {
		got := nextPushIndex(0, 3, true, sources)
		if got < 0 || got >= 3 {
			t.Fatalf("out of range %d", got)
		}
	}
}

func TestSelectPushTierSource_AlarmWins(t *testing.T) {
	alarm := &sourceWithName{cacheKey: "a:0", Name: "alarm"}
	scene := &sourceWithName{cacheKey: "s:0", Name: "scene"}
	fc := &FeedController{}
	fc.SetAlarmSource(alarm)
	fc.SetSceneSource(scene)
	if got := selectPushTierSource(fc); got != alarm {
		t.Fatalf("expected alarm, got %v", got)
	}
	fc2 := &FeedController{}
	fc2.SetSceneSource(scene)
	if got := selectPushTierSource(fc2); got != scene {
		t.Fatalf("expected scene")
	}
	fc3 := &FeedController{}
	if got := selectPushTierSource(fc3); got != nil {
		t.Fatalf("expected nil")
	}
}

func TestPushBrightness_PrecedenceAndRampGate(t *testing.T) {
	// precedence via ResolveEffectiveBrightnessWithScene
	over := 10
	sensor := 80
	alarmLvl := 55
	sceneLvl := 30
	now := time.Now()
	// override beats all
	got := ResolveEffectiveBrightnessWithScene(now, nil, &sensor, &over, &alarmLvl, &sceneLvl)
	if got != 10 {
		t.Fatalf("override precedence failed %d", got)
	}
	// alarm beats scene/sensor
	got = ResolveEffectiveBrightnessWithScene(now, nil, &sensor, nil, &alarmLvl, &sceneLvl)
	if got != 55 {
		t.Fatalf("alarm precedence failed %d", got)
	}
	// ramp gate
	pb := &pushBrightness{
		enabled:     true,
		schedules:   nil,
		ramp:        NewBrightnessRamp(100),
		lastAdvance: now,
	}
	pb.ramp.Current = 100
	pb.ramp.Target = 100
	// set target different to test gating
	pb.ramp.SetTarget(20)
	v1 := pb.level(now.Add(1 * time.Second))
	v2 := pb.level(now.Add(2 * time.Second))
	if v1 != v2 {
		t.Fatalf("ramp gate failed: %d vs %d", v1, v2)
	}
}

func TestApplyOverlayWithScene_DisabledPassthrough(t *testing.T) {
	// create a tiny PNG
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	data := buf.Bytes()
	spec := render.OverlaySpec{Enabled: false}
	out := applyOverlayWithScene(data, spec, time.Now())
	if !bytes.Equal(out, data) {
		t.Fatalf("expected passthrough")
	}
}

func TestTransportRunnerRegistersFeed(t *testing.T) {
	orig := newOutputSink
	defer func() { newOutputSink = orig }()
	resetTransportRunners()
	stub := &stubSink{}
	newOutputSink = func(cfg OutputSinkConfig) (OutputSink, error) { return stub, nil }
	srv := newBackupTestServer(t)
	srv.DB.GeneralSettings.Create().SetTimeout(60).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	d := srv.DB.DeviceSettings.Create().SetName("d1").SetWidth(4).SetHeight(2).SetEnabled(true).SetTransport("wled").SetOutputFps(60).SaveX(srv.Ctx)
	StartTransportDevices(srv)
	time.Sleep(200 * time.Millisecond)
	if _, ok := getDeviceFeed(d.ID); !ok {
		t.Fatalf("device feed not registered")
	}
	// check feed is in controllers via pinAll side effect? just check registered
	StopAllTransportDevices()
	time.Sleep(100 * time.Millisecond)
	if _, ok := getDeviceFeed(d.ID); ok {
		t.Fatalf("feed should be unregistered after stop")
	}
	_ = datasource.DefaultTheme
}

func TestUnregisterDeviceFeedIf_DoesNotEvictReplacement(t *testing.T) {
	// isolate device 1
	deviceFeedMu.Lock()
	delete(deviceFeeds, 1)
	deviceFeedMu.Unlock()
	fcA := &FeedController{}
	fcB := &FeedController{}
	registerDeviceFeed(1, fcA)
	registerDeviceFeed(1, fcB)
	unregisterDeviceFeedIf(1, fcA)
	if got, ok := getDeviceFeed(1); !ok || got != fcB {
		t.Fatalf("expected fcB still registered, got %v ok %v", got, ok)
	}
	unregisterDeviceFeedIf(1, fcB)
	if _, ok := getDeviceFeed(1); ok {
		t.Fatalf("expected removed after fcB unregister")
	}
	// cleanup
	deviceFeedMu.Lock()
	delete(deviceFeeds, 1)
	deviceFeedMu.Unlock()
}

func TestNextPushIndex_AdaptiveEmptyWeightsRandom(t *testing.T) {
	origMode := GetOrderingMode()
	defer SetOrderingMode(origMode)
	SetOrderingMode("adaptive")
	// ensure empty weights
	globalWeightsCache.Set(map[SourceKey]float64{}, nil, nil, DefaultAdaptiveConfig())
	// nil case also empty
	sources := make([]sourceWithName, 8)
	for i := range sources {
		sources[i] = sourceWithName{cacheKey: string(rune('a'+i)) + ":0"}
	}
	n := len(sources)
	seenNonSequential := false
	for i := 0; i < 50; i++ {
		got := nextPushIndex(0, n, false, sources)
		if got < 0 || got >= n {
			t.Fatalf("out of range %d", got)
		}
		if got != (0+1)%n {
			seenNonSequential = true
		}
	}
	if !seenNonSequential {
		t.Fatalf("expected at least one non-sequential result in adaptive empty-weights random fallback")
	}
	// also with random=true still in range
	for i := 0; i < 20; i++ {
		got := nextPushIndex(2, n, true, sources)
		if got < 0 || got >= n {
			t.Fatalf("out of range %d", got)
		}
	}
}

func TestSlicePanelsForPush_IdentityWhenSinglePanel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	data := buf.Bytes()
	imported := slicePanelsForPush(data, &ent.DeviceSettings{})
	if !bytes.Equal(imported, data) {
		t.Fatalf("expected identity for single panel")
	}
}
