package handlers

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
	"ledit/render"
)

// ponytail: simplified vs serveFeed: renders first/composed source only (no rotation/tiering/transitions).
// Reuses overlaySpecForDevice, brightness (via brightnessProviderForTest or 100), defaultLKG+datasourceConfigSig,
// PanelLogicalWidth handling via render width not needed for push (use device width directly).
// WLED realtime mode times out on its own; on stop we send no "off" packet.

var (
	transportRunners   = map[int]*transportRunner{}
	transportRunnersMu sync.Mutex
)

var newOutputSink = NewOutputSink

type transportRunner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func StartTransportDevices(s *Server) {
	if s == nil || s.DB == nil {
		return
	}
	devs, err := s.DB.DeviceSettings.Query().Where(devicesettings.EnabledEQ(true), devicesettings.TransportNEQ("websocket")).All(s.Ctx)
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
	d, err := s.DB.DeviceSettings.Get(s.Ctx, deviceID)
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
	r := &transportRunner{cancel: cancel, done: make(chan struct{})}
	transportRunnersMu.Lock()
	// if raced, cancel old
	if old, ok := transportRunners[d.ID]; ok {
		old.cancel()
	}
	transportRunners[d.ID] = r
	transportRunnersMu.Unlock()
	go runTransportLoop(ctx, r.done, s, d, sink, cfg)
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

func runTransportLoop(ctx context.Context, done chan struct{}, s *Server, d *ent.DeviceSettings, sink OutputSink, cfg OutputSinkConfig) {
	defer close(done)
	defer sink.Close()
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
		// Refresh device row each tick for config changes without restart (lightweight)
		// If device deleted/disabled/websocket, exit.
		cur, err := s.DB.DeviceSettings.Get(s.Ctx, deviceID)
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
		start := time.Now()
		pixels, err := renderDevicePixels(s, d, width, height)
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

func renderDevicePixels(s *Server, d *ent.DeviceSettings, width, height int) ([]byte, error) {
	// Load GeneralSettings with edges like HandleDeviceWS (minimal set sufficient for composeDeviceSources to work via loadSources)
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCompositions().WithCountdowns().WithAiDigests().WithTransits().WithUptimes().WithPiholes().WithGithubs().WithSports().WithSunmoons().WithJellyfins().WithImmichs().WithQbittorrents().WithSabnzbd().WithOverseerrs().WithUptimeKumas().WithSpeedtests().WithAdguards().WithFrigates().WithZigbee2mqtts().WithTransmissions().WithProxmoxs().WithWastes().WithAirqualities().WithParcels().
		Only(context.Background())
	if err != nil {
		return nil, err
	}
	sources := s.WSHub.composeDeviceSources(d, settings)
	if len(sources) == 0 {
		return nil, nil
	}
	sw := sources[0]
	renderWidth := render.PanelLogicalWidth(width, d.PanelCols, d.PanelGap)
	cacheKey := lkgCacheKey("device:"+strconv.Itoa(d.ID)+":"+sw.cacheKey, renderWidth, height)
	img, _, err := defaultLKG.GetPNG(cacheKey, datasourceConfigSig(sw.Source), func() (*render.RenderedImage, error) {
		return sw.Source.GetPNG(renderWidth, height)
	})
	if err != nil {
		return nil, err
	}
	data := img.Data
	// overlay
	spec := overlaySpecForDevice(d)
	if spec.Enabled {
		if out, oerr := render.CompositeOverlayPNG(data, spec, time.Now()); oerr == nil {
			data = out
		}
	}
	// brightness
	lvl := 100
	if brightnessProviderForTest != nil {
		lvl = brightnessProviderForTest()
	} else if d.BrightnessEnabled {
		// reuse simple resolve without sensor
		lvl = 100
		// If device has brightness schedules, honor them (without sensor blending)
		if d.BrightnessSchedules != "" && d.BrightnessSchedules != "[]" {
			if wins, perr := ParseBrightnessWindows(d.BrightnessSchedules); perr == nil {
				lvl = ResolveBrightness(time.Now(), wins, nil, d.BrightnessOverride)
			}
		} else if d.BrightnessOverride != nil {
			lvl = *d.BrightnessOverride
		}
	}
	if lvl != 100 {
		data = dimPNGBytes(data, lvl)
	}
	nrgba, err := render.DecodeNRGBA(data)
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
