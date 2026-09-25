package handlers

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/render"
)

// ponytail: push parity = rotation+tiering(incident>alarm>scene>pin)+theme+scene-overlay+effective brightness/ramp+sensor+panel slicing.
// Deferred: transitions (hard cut), notifications-as-pixels, Animator sub-slot 50ms granularity (animation still advances because renderPushSourcePNG re-renders via LKG every OutputFps tick).

var (
	transportRunners   = map[int]*transportRunner{}
	transportRunnersMu sync.Mutex
)

var newOutputSink = NewOutputSink

type transportRunner struct {
	cancel            context.CancelFunc
	done              chan struct{}
	fc                *FeedController
	pb                *pushBrightness
	sources           []sourceWithName
	settings          *ent.GeneralSettings
	lastSourceRefresh time.Time
	current           *sourceWithName
	cursor            int
	nextRotate        time.Time
}

func StartTransportDevices(s *Server) {
	if s == nil || s.DB == nil {
		return
	}
	devs, err := s.DB.DeviceSettings.Query().Where(devicesettings.EnabledEQ(true), devicesettings.TransportNEQ("websocket")).WithGroup().All(s.Ctx)
	if err != nil {
		slog.Warn("StartTransportDevices query failed", "error", err)
		return
	}
	for _, d := range devs {
		startTransportRunner(s, d)
	}
}

func RestartTransportDevice(s *Server, deviceID int) {
	StopTransportDevice(deviceID)
	if s == nil || s.DB == nil {
		return
	}
	d, err := s.DB.DeviceSettings.Query().Where(devicesettings.IDEQ(deviceID)).WithGroup().Only(s.Ctx)
	if err != nil {
		return
	}
	if !d.Enabled || d.Transport == "websocket" {
		return
	}
	startTransportRunner(s, d)
}

func StopTransportDevice(deviceID int) {
	transportRunnersMu.Lock()
	r, ok := transportRunners[deviceID]
	if ok {
		delete(transportRunners, deviceID)
	}
	transportRunnersMu.Unlock()
	if !ok {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
	}
}

func StopAllTransportDevices() {
	transportRunnersMu.Lock()
	ids := make([]int, 0, len(transportRunners))
	for id := range transportRunners {
		ids = append(ids, id)
	}
	transportRunnersMu.Unlock()
	for _, id := range ids {
		StopTransportDevice(id)
	}
}

func startTransportRunner(s *Server, d *ent.DeviceSettings) {
	cfg := OutputSinkConfig{
		Transport:   d.Transport,
		Host:        sinkHost(d),
		Port:        sinkPort(d),
		WLEDMode:    d.WledRealtimeMode,
		WLEDChannel: d.WledChannel,
		Universe:    d.ArtnetUniverse,
		FPS:         d.OutputFps,
		ColorOrder:  d.OutputColorOrder,
		Gamma:       d.OutputGamma,
		Serpentine:  d.OutputMatrixLayout == "serpentine",
		Username:    d.Username,
		Password:    d.Password,
	}
	sink, err := newOutputSink(cfg)
	if err != nil {
		slog.Warn("transport sink create failed", "device", d.ID, "transport", d.Transport, "error", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &transportRunner{
		cancel:     cancel,
		done:       make(chan struct{}),
		fc:         &FeedController{},
		pb:         newPushBrightness(d),
		nextRotate: time.Now(),
	}
	registerDeviceFeed(d.ID, r.fc)
	joinController(r.fc)
	if src := AlarmSource(); src != nil {
		r.fc.SetAlarmSource(src)
	}
	if src := ActiveSceneSource(); src != nil {
		r.fc.SetSceneSource(src)
	}
	transportRunnersMu.Lock()
	if old, ok := transportRunners[d.ID]; ok {
		old.cancel()
	}
	transportRunners[d.ID] = r
	transportRunnersMu.Unlock()
	go runTransportLoop(ctx, r.done, s, d, sink, cfg, r)
}

func sinkHost(d *ent.DeviceSettings) string {
	switch d.Transport {
	case "wled":
		if d.WledHost != "" {
			return d.WledHost
		}
		return d.IP
	case "artnet":
		if d.ArtnetHost != "" {
			return d.ArtnetHost
		}
		return d.IP
	default:
		return d.IP
	}
}

func sinkPort(d *ent.DeviceSettings) int {
	switch d.Transport {
	case "wled":
		if d.WledPort != 0 {
			return d.WledPort
		}
		return 4048
	case "artnet":
		if d.ArtnetPort != 0 {
			return d.ArtnetPort
		}
		return 6454
	default:
		return 0
	}
}

func runTransportLoop(ctx context.Context, done chan struct{}, s *Server, d *ent.DeviceSettings, sink OutputSink, cfg OutputSinkConfig, r *transportRunner) {
	defer close(done)
	defer sink.Close()
	defer func() {
		leaveController(r.fc)
		unregisterDeviceFeedIf(d.ID, r.fc)
	}()
	interval := cfg.Interval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var seq uint32
	backoffAttempt := 0
	width := d.Width
	if width <= 0 {
		width = 64
	}
	height := d.Height
	if height <= 0 {
		height = 64
	}
	deviceID := d.ID
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		cur, err := s.DB.DeviceSettings.Query().Where(devicesettings.IDEQ(deviceID)).WithGroup().Only(s.Ctx)
		if err != nil {
			return
		}
		if !cur.Enabled || cur.Transport == "websocket" {
			return
		}
		d = cur
		width = d.Width
		if width <= 0 {
			width = 64
		}
		height = d.Height
		if height <= 0 {
			height = 64
		}
		now := time.Now()
		if r.settings == nil || len(r.sources) == 0 || now.Sub(r.lastSourceRefresh) >= 60*time.Second {
			if gs, gerr := loadAllSettings(s.Ctx, s.DB); gerr == nil {
				r.settings = gs
				r.sources = s.WSHub.composeDeviceSources(d, gs)
				r.lastSourceRefresh = now
			}
		}
		if len(r.sources) == 0 {
			continue
		}
		start := time.Now()
		pixels, err := renderPushFrame(s, r, d, width, height, now)
		if err != nil {
			dur := time.Since(start)
			Health.RecordFailure(deviceKey(deviceID), err, dur)
			GlobalMetricsSink.IncCounter("ledit_transport_errors_total", "")
			backoffAttempt++
			dur2 := BackoffDelay(backoffAttempt)
			if dur2 > 5*time.Second {
				dur2 = 5 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(dur2):
			}
			continue
		}
		if pixels == nil {
			continue
		}
		frame := OutputFrame{Width: width, Height: height, Pixels: pixels, Seq: seq, At: time.Now()}
		seq++
		err = sink.Send(frame)
		dur := time.Since(start)
		if err != nil {
			Health.RecordFailure(deviceKey(deviceID), err, dur)
			GlobalMetricsSink.IncCounter("ledit_transport_errors_total", "")
			backoffAttempt++
			dur2 := BackoffDelay(backoffAttempt)
			if dur2 > 5*time.Second {
				dur2 = 5 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(dur2):
			}
			continue
		}
		Health.RecordSuccess(deviceKey(deviceID), dur)
		GlobalMetricsSink.IncCounter("ledit_transport_frames_total", "")
		backoffAttempt = 0
	}
}

func deviceKey(id int) string { return "device:" + strconv.Itoa(id) }

func renderPushFrame(s *Server, r *transportRunner, d *ent.DeviceSettings, width, height int, now time.Time) ([]byte, error) {
	renderWidth := render.PanelLogicalWidth(width, d.PanelCols, d.PanelGap)
	if scene, ok := CurrentIncidentScene(); ok {
		if data, err := render.IncidentPNG(renderWidth, height, scene, now); err == nil {
			return frameToPixels(slicePanelsForPush(data, d), width, height, d)
		}
	}
	var sw *sourceWithName
	if tier := selectPushTierSource(r.fc); tier != nil {
		sw = tier
	} else if pinnedKey, _, ok := r.fc.IsPinned(); ok {
		for i := range r.sources {
			if r.sources[i].cacheKey == pinnedKey {
				sw = &r.sources[i]
				break
			}
		}
	}
	if sw == nil {
		slot := time.Duration(d.RefreshInterval) * time.Second
		if slot <= 0 {
			slot = 60 * time.Second
		}
		needRotate := r.current == nil || !containsSource(r.sources, *r.current) || now.After(r.nextRotate)
		if needRotate {
			idx := r.cursor % len(r.sources)
			cur := r.sources[idx]
			r.current = &cur
			random := false
			if r.settings != nil {
				random = r.settings.Random && !(d.ContentMode == "playlist" || d.ContentMode == "scheduled")
			}
			r.cursor = nextPushIndex(idx, len(r.sources), random, r.sources)
			r.nextRotate = now.Add(slot)
		}
		sw = r.current
	}
	if sw == nil {
		return nil, nil
	}
	img, err := renderPushSourcePNG(*sw, "device:"+strconv.Itoa(d.ID)+":", renderWidth, height, d.ID)
	if err != nil {
		return nil, err
	}
	lvl := r.pb.level(now)
	data := applyOverlayWithScene(img.Data, overlaySpecForDeviceWithGroup(d), now)
	if lvl != 100 {
		data = dimPNGBytes(data, lvl)
	}
	return frameToPixels(slicePanelsForPush(data, d), width, height, d)
}

func slicePanelsForPush(data []byte, d *ent.DeviceSettings) []byte {
	if d.PanelCols < 2 || d.PanelGap <= 0 {
		return data
	}
	out, err := render.SlicePanelGapsPNG(data, d.PanelCols, d.PanelGap)
	if err != nil {
		return data
	}
	return out
}

func frameToPixels(pngBytes []byte, width, height int, d *ent.DeviceSettings) ([]byte, error) {
	nrgba, err := render.DecodeNRGBA(pngBytes)
	if err != nil {
		return nil, err
	}
	pCfg := render.PixelMapConfig{
		Width:      width,
		Height:     height,
		ColorOrder: d.OutputColorOrder,
		Gamma:      d.OutputGamma,
		Serpentine: d.OutputMatrixLayout == "serpentine",
		OriginTop:  true,
	}
	pixels := render.FrameToPixels(nrgba, pCfg)
	return pixels, nil
}

func containsSource(sources []sourceWithName, sw sourceWithName) bool {
	for _, s := range sources {
		if s.cacheKey == sw.cacheKey {
			return true
		}
	}
	return false
}
