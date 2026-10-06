package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Shared actuation layer: Home Assistant entities, inbound MQTT commands, and
// event-rule then-actions all converge here so one set of feed-controller and
// brightness semantics applies everywhere (precedence: notification > incident
// > alarm > scene > pin > rotation; manual actions reclaim the wall from
// ambient automation).

// ErrSelectInvalid marks a syntactically invalid, unknown, or unavailable
// content selection.
var ErrSelectInvalid = errors.New("invalid selection")

// ApplyDeviceBrightness persists a manual brightness override for deviceID and
// pushes an immediate hint to the device's live feed. level is clamped
// defensively; source names the caller for logs ("mqtt", "ha", "rule").
func (s *Server) ApplyDeviceBrightness(deviceID, level int, source string) {
	if s == nil || s.DB == nil || deviceID <= 0 {
		return
	}
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	if _, err := s.DB.DeviceSettings.Get(s.Ctx, deviceID); err != nil {
		slog.Debug("brightness apply: unknown device", "device", deviceID, "source", source)
		return
	}
	_, err := s.DB.DeviceSettings.UpdateOneID(deviceID).SetBrightnessOverride(level).Save(s.Ctx)
	if err != nil {
		// Background-context fallback mirrors the pre-existing MQTT path: a
		// cancelled request context must not lose the write.
		_, err = s.DB.DeviceSettings.UpdateOneID(deviceID).SetBrightnessOverride(level).Save(context.Background())
	}
	if err != nil {
		slog.Warn("brightness apply failed", "device", deviceID, "source", source, "error", err)
		return
	}
	// A manual override reclaims the wall from ambient automation.
	SuppressActiveScene()
	// Restart push transports first: it re-registers the device feed for
	// non-websocket transports, so the hint must land on the final controller.
	RestartTransportDevice(s, deviceID)
	if fc, ok := getDeviceFeed(deviceID); ok {
		// Ephemeral hint: the WebSocket brightness closure consults it before
		// the database-derived value so live connections dim immediately.
		fc.SetBrightnessHint(level)
	}
	PublishOutbound(fmt.Sprintf("ledit/device/%d/brightness/state", deviceID), strconv.Itoa(level), true)
}

// ApplyDeviceSelect applies a content selection with the grammar
// "source:<type>:<id>", "playlist:<id>", or "scene:<id>". Off-rotation
// sources are ignored without touching the active pin. Malformed or
// unavailable selections return ErrSelectInvalid and change nothing.
func (s *Server) ApplyDeviceSelect(deviceID int, value string) error {
	if s == nil || s.DB == nil || deviceID <= 0 {
		return ErrSelectInvalid
	}
	sel := strings.TrimSpace(value)
	if sel == "" {
		return fmt.Errorf("%w: empty", ErrSelectInvalid)
	}
	dev, err := s.DB.DeviceSettings.Get(s.Ctx, deviceID)
	if err != nil {
		return fmt.Errorf("%w: device %d not found", ErrSelectInvalid, deviceID)
	}
	if !dev.Enabled {
		return fmt.Errorf("%w: device %d disabled", ErrSelectInvalid, deviceID)
	}
	parts := strings.Split(sel, ":")
	switch parts[0] {
	case "source":
		if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
		}
		if _, err := strconv.Atoi(parts[2]); err != nil {
			return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
		}
		fc, ok := getDeviceFeed(deviceID)
		if !ok {
			return fmt.Errorf("%w: device %d has no live feed", ErrSelectInvalid, deviceID)
		}
		key := parts[1] + ":" + parts[2]
		if !fc.HasRotationKey(key) {
			slog.Info("select: source not in rotation, ignored", "device", deviceID, "selection", sel)
			return nil
		}
		fc.Pin(key, "select")
		publishSelectState(deviceID, sel)
		return nil
	case "playlist":
		if len(parts) != 2 {
			return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
		}
		plID, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
		}
		pl, err := s.DB.Playlist.Get(s.Ctx, plID)
		if err != nil || pl == nil || !pl.Enabled {
			return fmt.Errorf("%w: playlist %d unavailable", ErrSelectInvalid, plID)
		}
		if err := s.persistPlaylistContent(deviceID, plID); err != nil {
			return fmt.Errorf("%w: playlist %d persist failed", ErrSelectInvalid, plID)
		}
		RestartTransportDevice(s, deviceID)
		publishSelectState(deviceID, sel)
		return nil
	case "scene":
		if len(parts) != 2 {
			return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
		}
		scID, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
		}
		row, err := s.DB.Scene.Get(s.Ctx, scID)
		if err != nil || row == nil || !row.Enabled {
			return fmt.Errorf("%w: scene %d unavailable", ErrSelectInvalid, scID)
		}
		sc, err := sceneFromEnt(row)
		if err != nil {
			return fmt.Errorf("%w: scene %d parse failed", ErrSelectInvalid, scID)
		}
		d := 60 * time.Second
		if sc.TTLSeconds != nil && *sc.TTLSeconds > 0 {
			d = time.Duration(*sc.TTLSeconds) * time.Second
		}
		if !PreviewScene(sc, func(x *Scene) (*sourceWithName, bool) { return resolveSceneSource(s.DB, x) }, d) {
			return fmt.Errorf("%w: scene %d not resolvable", ErrSelectInvalid, scID)
		}
		publishSelectState(deviceID, sel)
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrSelectInvalid, sel)
	}
}

// persistPlaylistContent mirrors the admin device-update path
// (handlers.go:1761-1783) for switching a device to playlist mode.
func (s *Server) persistPlaylistContent(deviceID, playlistID int) error {
	upd := func(ctx context.Context) error {
		return s.DB.DeviceSettings.UpdateOneID(deviceID).
			SetContentMode("playlist").
			SetScheduledPlaylistIds("[]").
			ClearFallbackPlaylistID().
			SetPlaylistID(playlistID).
			Exec(ctx)
	}
	if err := upd(s.Ctx); err != nil {
		return upd(context.Background())
	}
	return nil
}

// deviceMessageSeq tracks the last accepted message notification per device so
// a stale TTL timer cannot clear a newer message's retained state.
var deviceMessageSeq sync.Map // deviceID int -> notifEntry.ID int

// deviceMessageTexts remembers the last accepted message text per device so a
// connect/device-save republish can restore HA message state while it is still
// in flight; entries are removed when the message expires.
var deviceMessageTexts sync.Map // deviceID int -> string

// ApplyDeviceMessage turns plain text into a device-targeted notification and
// publishes retained message state. Empty text is ignored, text over 255
// characters is rejected, and devices without a live feed are ignored per the
// offline no-op rule. The TTL matches the webhook default.
func (s *Server) ApplyDeviceMessage(deviceID int, text string) error {
	if s == nil || s.DB == nil || deviceID <= 0 {
		return fmt.Errorf("message: invalid device")
	}
	msg := strings.TrimSpace(text)
	if msg == "" {
		return nil
	}
	if utf8.RuneCountInString(msg) > 255 {
		return fmt.Errorf("message: exceeds 255 characters")
	}
	dev, err := s.DB.DeviceSettings.Get(s.Ctx, deviceID)
	if err != nil {
		return fmt.Errorf("message: device %d not found", deviceID)
	}
	if !dev.Enabled {
		return fmt.Errorf("message: device %d disabled", deviceID)
	}
	if _, ok := getDeviceFeed(deviceID); !ok {
		slog.Debug("message: no live feed, ignored", "device", deviceID)
		return nil
	}
	ttl := time.Duration(s.webhookDefaultTTL()) * time.Second
	entry := s.AddNotification(msg, "", WithTTL(ttl), withTargetDevice(deviceID))
	PublishOutbound(fmt.Sprintf("ledit/device/%d/message/state", deviceID), msg, true)
	if ttl > 0 {
		deviceMessageSeq.Store(deviceID, entry.ID)
		deviceMessageTexts.Store(deviceID, msg)
		time.AfterFunc(ttl, func() {
			if cur, ok := deviceMessageSeq.Load(deviceID); ok && cur.(int) == entry.ID {
				deviceMessageSeq.Delete(deviceID)
				deviceMessageTexts.Delete(deviceID)
				PublishOutbound(fmt.Sprintf("ledit/device/%d/message/state", deviceID), "", true)
			}
		})
	}
	return nil
}

// deviceMessageText returns the last accepted message text for a device while
// it is still in flight.
func deviceMessageText(deviceID int) (string, bool) {
	if v, ok := deviceMessageTexts.Load(deviceID); ok {
		if txt, ok := v.(string); ok && txt != "" {
			return txt, true
		}
	}
	return "", false
}

func publishSelectState(deviceID int, value string) {
	PublishOutbound(fmt.Sprintf("ledit/device/%d/select/state", deviceID), value, true)
}

// notificationMatchesDevice reports whether a notification should render on a
// connection for deviceID: broadcast entries (target 0) render everywhere,
// targeted entries render only on the target device.
func notificationMatchesDevice(n notifEntry, deviceID int) bool {
	return n.Target == 0 || n.Target == deviceID
}
