package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"ledit/render"
)

type stubSink struct {
	mu       sync.Mutex
	frames   []OutputFrame
	failN    int
	attempts int
}

func (s *stubSink) Name() string { return "stub" }
func (s *stubSink) Close() error { return nil }
func (s *stubSink) Send(f OutputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.failN > 0 {
		s.failN--
		return errString("fake fail")
	}
	s.frames = append(s.frames, f)
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

func resetTransportRunners() {
	transportRunnersMu.Lock()
	for _, r := range transportRunners {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(100 * time.Millisecond):
		}
	}
	transportRunners = map[int]*transportRunner{}
	transportRunnersMu.Unlock()
	GlobalMetricsSink.Reset()
	Health.Reset()
}

func solidPNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

var _ = solidPNG

func TestTransportSendsFrame(t *testing.T) {
	orig := newOutputSink
	defer func() { newOutputSink = orig }()
	resetTransportRunners()
	stub := &stubSink{}
	newOutputSink = func(cfg OutputSinkConfig) (OutputSink, error) { return stub, nil }

	srv := newBackupTestServer(t)
	srv.DB.GeneralSettings.Create().SetTimeout(60).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	d := srv.DB.DeviceSettings.Create().SetName("d1").SetWidth(4).SetHeight(2).SetEnabled(true).SetTransport("wled").SetWledHost("127.0.0.1").SetOutputFps(60).SaveX(srv.Ctx)

	StartTransportDevices(srv)
	time.Sleep(300 * time.Millisecond)
	StopAllTransportDevices()

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.frames) == 0 {
		t.Fatalf("expected at least one frame, attempts %d", stub.attempts)
	}
	f := stub.frames[0]
	if len(f.Pixels) != 4*2*3 {
		t.Fatalf("pixels len %d want %d", len(f.Pixels), 4*2*3)
	}
	_ = d
}

func TestTransportRetryOnFail(t *testing.T) {
	orig := newOutputSink
	defer func() { newOutputSink = orig }()
	resetTransportRunners()
	stub := &stubSink{failN: 1}
	newOutputSink = func(cfg OutputSinkConfig) (OutputSink, error) { return stub, nil }
	srv := newBackupTestServer(t)
	srv.DB.GeneralSettings.Create().SetTimeout(60).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	srv.DB.DeviceSettings.Create().SetName("d1").SetWidth(4).SetHeight(2).SetEnabled(true).SetTransport("artnet").SetArtnetHost("127.0.0.1").SetOutputFps(60).SaveX(srv.Ctx)
	StartTransportDevices(srv)
	time.Sleep(3000 * time.Millisecond)
	start := time.Now()
	StopTransportDevice(1)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("StopTransportDevice slow")
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.attempts < 2 {
		t.Fatalf("expected retries, attempts %d", stub.attempts)
	}
	if len(stub.frames) == 0 {
		t.Fatalf("expected eventual success after retries")
	}
}

func TestRestartDisablesStops(t *testing.T) {
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
	srv.DB.DeviceSettings.UpdateOneID(d.ID).SetEnabled(false).ExecX(srv.Ctx)
	RestartTransportDevice(srv, d.ID)
	time.Sleep(200 * time.Millisecond)
	stub.mu.Lock()
	countAfter := len(stub.frames)
	stub.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	stub.mu.Lock()
	countLater := len(stub.frames)
	stub.mu.Unlock()
	if countLater != countAfter {
		t.Fatalf("expected no further frames after disable: %d vs %d", countAfter, countLater)
	}
	StopAllTransportDevices()
}

func TestTransportSkipsWebsocket(t *testing.T) {
	orig := newOutputSink
	defer func() { newOutputSink = orig }()
	resetTransportRunners()
	called := false
	newOutputSink = func(cfg OutputSinkConfig) (OutputSink, error) { called = true; return &stubSink{}, nil }
	srv := newBackupTestServer(t)
	srv.DB.DeviceSettings.Create().SetName("ws").SetEnabled(true).SetTransport("websocket").SaveX(srv.Ctx)
	StartTransportDevices(srv)
	time.Sleep(150 * time.Millisecond)
	if called {
		t.Fatalf("should not create sink for websocket")
	}
	StopAllTransportDevices()
}

func TestFrameToPixelsConfig(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	img.Set(1, 0, color.RGBA{100, 150, 200, 255})
	p := render.FrameToPixels(img, render.PixelMapConfig{Width: 2, Height: 1, ColorOrder: "GRB", Gamma: 1, OriginTop: true})
	if p[0] != 20 || p[1] != 10 || p[2] != 30 {
		t.Fatalf("GRB failed %v", p[:3])
	}
	if p[3] != 150 || p[4] != 100 || p[5] != 200 {
		t.Fatalf("GRB second pixel %v", p[3:6])
	}
	img2 := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img2.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img2.Set(1, 0, color.RGBA{0, 255, 0, 255})
	img2.Set(0, 1, color.RGBA{0, 0, 255, 255})
	img2.Set(1, 1, color.RGBA{255, 255, 255, 255})
	pNorm := render.FrameToPixels(img2, render.PixelMapConfig{Width: 2, Height: 2, ColorOrder: "RGB", OriginTop: true})
	if pNorm[6] != 0 || pNorm[7] != 0 || pNorm[8] != 255 {
		t.Fatalf("row-major row1 col0 not blue %v", pNorm[6:9])
	}
	pSerp := render.FrameToPixels(img2, render.PixelMapConfig{Width: 2, Height: 2, ColorOrder: "RGB", Serpentine: true, OriginTop: true})
	if pSerp[6] != 255 || pSerp[7] != 255 || pSerp[8] != 255 {
		t.Fatalf("serpentine row1 col0 not white %v", pSerp[6:9])
	}
}
