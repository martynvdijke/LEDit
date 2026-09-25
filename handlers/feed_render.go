package handlers

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
	"ledit/render"
)

// loadAllSettings loads GeneralSettings with all edges needed for source composition.
func loadAllSettings(ctx context.Context, client *ent.Client) (*ent.GeneralSettings, error) {
	return client.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCompositions().WithCountdowns().WithAiDigests().WithTransits().WithUptimes().WithPiholes().WithGithubs().WithSports().WithSunmoons().WithJellyfins().WithImmichs().WithQbittorrents().WithSabnzbd().WithOverseerrs().WithUptimeKumas().WithSpeedtests().WithAdguards().WithFrigates().WithZigbee2mqtts().WithTransmissions().WithProxmoxs().WithWastes().WithAirqualities().WithParcels().
		Only(ctx)
}

// renderPushSourcePNG renders a source with theme-aware LKG caching, mirroring websocket feed.
func renderPushSourcePNG(sw sourceWithName, cachePrefix string, renderWidth, height int, deviceID int) (*render.RenderedImage, error) {
	sourceTheme := datasource.DefaultTheme()
	if parts := splitCacheKey(sw.cacheKey); len(parts) == 2 {
		datasource.SetChartContext(parts[0], parts[1])
		sourceTheme = ResolveTheme(parts[0], mustAtoi(parts[1]))
	}
	cacheKey := lkgCacheKey(cachePrefix+sw.cacheKey, renderWidth, height) + "|" + themeCacheSig(sourceTheme)
	img, _, err := defaultLKG.GetPNG(cacheKey, datasourceConfigSig(sw.Source), func() (*render.RenderedImage, error) {
		start := time.Now()
		img, err := datasource.RenderThemed(sw.Source, renderWidth, height, sourceTheme)
		dur := time.Since(start)
		if err != nil {
			Health.RecordFailure(sw.cacheKey, err, dur)
			if deviceID > 0 {
				Health.RecordFailure(deviceKey(deviceID), err, dur)
			}
		} else {
			Health.RecordSuccess(sw.cacheKey, dur)
		}
		return img, err
	})
	return img, err
}

// applyOverlayWithScene composites overlay, with scene overlay taking precedence.
func applyOverlayWithScene(data []byte, spec render.OverlaySpec, now time.Time) []byte {
	if s, ok := ActiveSceneOverlay(now); ok {
		spec = s
	}
	if !spec.Enabled {
		return data
	}
	out, err := render.CompositeOverlayPNG(data, spec, now)
	if err != nil {
		return data
	}
	return out
}

// selectPushTierSource returns alarm>scene tier source.
func selectPushTierSource(fc *FeedController) *sourceWithName {
	return selectTierSource(fc.GetAlarmSource(), fc.GetSceneSource())
}

// nextPushIndex chooses next rotation index.
func nextPushIndex(cur, n int, random bool, sources []sourceWithName) int {
	if n <= 0 {
		return 0
	}
	if GetOrderingMode() == "adaptive" {
		w := globalWeightsCache.GetWeights()
		if len(w) > 0 {
			pick := WeightedRandom(w, sources)
			for i, s := range sources {
				if s.cacheKey == pick.cacheKey {
					return i
				}
			}
		}
		return rand.Intn(n)
	}
	if random {
		return rand.Intn(n)
	}
	return (cur + 1) % n
}

// pushBrightness holds per-device brightness state for push transports.
type pushBrightness struct {
	enabled         bool
	schedules       []BrightnessWindow
	sensorCfg       *SensorConfig
	override        *int
	ramp            *BrightnessRamp
	sensorState     SensorFetchState
	lastSensorFetch time.Time
	sensorCache     *int
	sensorCacheTime time.Time
	lastAdvance     time.Time
}

func newPushBrightness(d *ent.DeviceSettings) *pushBrightness {
	ebEnabled, ebSchedules, ebOverride, ebSensorRaw := effectiveBrightnessConfig(d)
	schedules, _ := ParseBrightnessWindows(ebSchedules)
	var sensorCfg *SensorConfig
	if ebSensorRaw != nil {
		sensorCfg, _ = ParseSensorConfig(ebSensorRaw)
	}
	ramp := NewBrightnessRamp(100)
	var sensorLevel *int
	if ebEnabled && sensorCfg != nil {
		if lux, err := FetchSensorLux(sensorCfg.EntityID); err == nil {
			sensorLevel = SensorLevelForLux(lux, sensorCfg)
		}
	}
	target := ResolveBrightness(time.Now(), schedules, sensorLevel, ebOverride)
	if !ebEnabled {
		target = 100
	}
	ramp.Current = float64(target)
	ramp.Target = target
	return &pushBrightness{
		enabled:     ebEnabled,
		schedules:   schedules,
		sensorCfg:   sensorCfg,
		override:    ebOverride,
		ramp:        ramp,
		lastAdvance: time.Now(),
	}
}

func (pb *pushBrightness) level(now time.Time) int {
	if brightnessProviderForTest != nil {
		return brightnessProviderForTest()
	}
	if !pb.enabled {
		return 100
	}
	if pb.sensorCfg != nil && now.Sub(pb.lastSensorFetch) >= 5*time.Second {
		if pb.sensorState.ShouldFetch(now, pb.lastSensorFetch) {
			pb.lastSensorFetch = now
			if lux, err := FetchSensorLux(pb.sensorCfg.EntityID); err == nil {
				pb.sensorState.LastValue = lux
				pb.sensorState.LastTime = now
				pb.sensorState.Warned = false
				pb.sensorCache = SensorLevelForLux(lux, pb.sensorCfg)
				pb.sensorCacheTime = now
			} else {
				if !pb.sensorState.Warned {
					slog.Warn("brightness sensor fetch failed, degrading to schedule", "error", err)
					pb.sensorState.Warned = true
				}
			}
		}
	}
	var sensorLevel *int
	if pb.sensorCfg != nil && !pb.sensorState.IsStale(now) && pb.sensorCache != nil && now.Sub(pb.sensorCacheTime) <= 60*time.Second {
		sensorLevel = pb.sensorCache
	} else if pb.sensorCfg != nil && pb.sensorCache != nil && now.Sub(pb.sensorCacheTime) > 60*time.Second {
		pb.sensorCache = nil
	}
	target := ResolveEffectiveBrightnessWithScene(now, pb.schedules, sensorLevel, pb.override, ActiveAlarmBrightnessLevel(now), ActiveSceneBrightnessLevel(now))
	pb.ramp.SetTarget(target)
	if now.Sub(pb.lastAdvance) >= 3*time.Second {
		pb.lastAdvance = now
		return pb.ramp.Advance()
	}
	return int(pb.ramp.Current + 0.5)
}
