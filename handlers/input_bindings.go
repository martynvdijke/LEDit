package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"ledit/ent"
	"ledit/ent/inputbinding"
)

// InputAction is the declarative action a binding maps an input event to. It
// mirrors the ThenAction style (handlers/ruleactions.go): one JSON object, a
// closed kind vocabulary, structural validation up front and DB-backed target
// validation at admin-save time. Runtime execution treats missing targets as
// non-fatal (log and skip) so a deleted scene can never break the feed loop.
type InputAction struct {
	Kind       string `json:"kind"`
	SceneID    int    `json:"scene_id,omitempty"`
	PlaylistID int    `json:"playlist_id,omitempty"`
	Verb       string `json:"verb,omitempty"`
	Level      *int   `json:"level,omitempty"`
	RuleID     int    `json:"rule_id,omitempty"`
	GreetingID int    `json:"greeting_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Message    string `json:"message,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// ParseInputAction decodes and structurally validates a binding action.
func ParseInputAction(raw string) (InputAction, error) {
	var a InputAction
	s := strings.TrimSpace(raw)
	if s == "" || s == "{}" {
		return a, fmt.Errorf("action is required")
	}
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return a, fmt.Errorf("invalid action json: %w", err)
	}
	if msg := ValidateInputAction(a); msg != "" {
		return a, fmt.Errorf("%s", msg)
	}
	return a, nil
}

func validateFeedVerb(v string) bool {
	switch v {
	case "next", "previous", "pause", "resume", "toggle":
		return true
	}
	return false
}

// ValidateInputAction checks the structural shape of an action. Returns "" when
// valid, otherwise a human-readable reason.
func ValidateInputAction(a InputAction) string {
	switch a.Kind {
	case "scene":
		if a.SceneID <= 0 {
			return "scene_id is required for scene action"
		}
	case "playlist":
		if a.PlaylistID <= 0 {
			return "playlist_id is required for playlist action"
		}
	case "feed":
		if !validateFeedVerb(a.Verb) {
			return "verb must be one of next, previous, pause, resume, toggle"
		}
	case "brightness":
		if a.Level == nil {
			return "level is required for brightness action"
		}
		if *a.Level < 0 || *a.Level > 100 {
			return "level must be between 0 and 100"
		}
	case "rule":
		if a.RuleID <= 0 {
			return "rule_id is required for rule action"
		}
	case "greeting":
		if a.GreetingID <= 0 {
			return "greeting_id is required for greeting action"
		}
	case "notification":
		if strings.TrimSpace(a.Message) == "" {
			return "message is required for notification action"
		}
	case "":
		return "kind is required"
	default:
		return "unknown kind: " + a.Kind
	}
	return ""
}

// ValidateInputActionTargets is the admin-save-time check: structural
// validation plus existence/enabled checks for DB-backed targets.
func (s *Server) ValidateInputActionTargets(ctx context.Context, a InputAction) string {
	if s == nil || s.DB == nil {
		return "server unavailable"
	}
	if msg := ValidateInputAction(a); msg != "" {
		return msg
	}
	switch a.Kind {
	case "scene":
		row, err := s.DB.Scene.Get(ctx, a.SceneID)
		if err != nil || !row.Enabled {
			return "scene not found or disabled"
		}
	case "playlist":
		row, err := s.DB.Playlist.Get(ctx, a.PlaylistID)
		if err != nil || !row.Enabled {
			return "playlist not found or disabled"
		}
	case "rule":
		row, err := s.DB.DisplayRule.Get(ctx, a.RuleID)
		if err != nil || !row.Enabled {
			return "rule not found or disabled"
		}
	case "greeting":
		row, err := s.DB.GreetingRule.Get(ctx, a.GreetingID)
		if err != nil || !row.Enabled {
			return "greeting not found or disabled"
		}
	}
	return ""
}

func (s *Server) inputCtx() context.Context {
	if s != nil && s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

// resolveInputBinding performs ordered first-match resolution: enabled
// bindings scoped to the device or global (device_id IS NULL), sorted by
// order asc, then device-scoped before global, then id asc. At most one match
// is returned.
func (s *Server) resolveInputBinding(ctx context.Context, deviceID int, ev InputEvent) (InputAction, bool) {
	rows, err := s.DB.InputBinding.Query().
		Where(
			inputbinding.EnabledEQ(true),
			inputbinding.Or(inputbinding.DeviceIDIsNil(), inputbinding.DeviceIDEQ(deviceID)),
		).
		All(ctx)
	if err != nil {
		slog.Warn("input binding query failed", "device", deviceID, "error", err)
		return InputAction{}, false
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Order != rows[j].Order {
			return rows[i].Order < rows[j].Order
		}
		deviceScopedI := rows[i].DeviceID != nil
		deviceScopedJ := rows[j].DeviceID != nil
		if deviceScopedI != deviceScopedJ {
			return deviceScopedI
		}
		return rows[i].ID < rows[j].ID
	})
	for _, b := range rows {
		if b.Source != "" && b.Source != ev.Source {
			continue
		}
		if b.Event != "" && b.Event != ev.Event {
			continue
		}
		if evValue, _ := ev.ValueString(); strings.TrimSpace(b.Match) != "" && b.Match != evValue {
			continue
		}
		a, err := ParseInputAction(b.Action)
		if err != nil {
			slog.Warn("input binding action invalid", "binding", b.ID, "error", err)
			continue
		}
		return a, true
	}
	return InputAction{}, false
}

// publishInputEvent mirrors accepted device input events to MQTT: a compact
// per-event topic payload plus the HA event-entity state payload. Both are
// no-ops when MQTT is disconnected (PublishOutbound gate). Payloads are
// bounded because values were validated at parse time.
func publishInputEvent(deviceID int, ev InputEvent) {
	if deviceID <= 0 {
		return
	}
	base := fmt.Sprintf("ledit/device/%d/input", deviceID)
	value := inputEventValue(ev)
	compact := map[string]any{"source": ev.Source, "event": ev.Event}
	if value != nil {
		compact["value"] = value
	}
	if b, err := json.Marshal(compact); err == nil {
		PublishOutbound(base+"/"+ev.Event, string(b), false)
	}
	haState := map[string]any{"event_type": ev.Event, "source": ev.Source}
	if value != nil {
		haState["value"] = value
	}
	if b, err := json.Marshal(haState); err == nil {
		PublishOutbound(base+"/event", string(b), false)
	}
}

// inputEventValue decodes the raw event value for JSON re-marshaling. Nil when
// the event carries no value.
func inputEventValue(ev InputEvent) any {
	if !hasInputValue(ev.Value) {
		return nil
	}
	var v any
	if err := json.Unmarshal(ev.Value, &v); err != nil {
		return nil
	}
	return v
}

// handleDeviceInput is the server-side sink for accepted device input events:
// resolve a binding and execute the first match, otherwise fall back to the
// built-in defaults.
func (s *Server) handleDeviceInput(deviceID int, ev InputEvent) {
	if s == nil || s.DB == nil {
		return
	}
	recordDeviceInput(deviceID, ev)
	if ev.Source == InputSourceLux {
		if lux, ok := ev.ValueFloat(); ok {
			recordDeviceLux(deviceID, lux)
		}
	}
	publishInputEvent(deviceID, ev)
	ctx := s.inputCtx()
	if a, ok := s.resolveInputBinding(ctx, deviceID, ev); ok {
		s.executeInputAction(ctx, deviceID, ev, a)
		return
	}
	s.applyInputDefault(deviceID, ev)
}

// applyInputDefault implements the built-in bindings used when no configured
// binding matches. All defaults are inert when the device has no live feed.
func (s *Server) applyInputDefault(deviceID int, ev InputEvent) {
	fc, ok := getDeviceFeed(deviceID)
	switch ev.Source {
	case InputSourceButtonNext:
		if ev.Event == InputEventPress && ok {
			fc.Next()
		}
	case InputSourceButtonPause:
		if ev.Event == InputEventPress && ok {
			fc.Pause()
		}
	case InputSourceEncoder:
		switch ev.Event {
		case InputEventRotate:
			if !ok {
				return
			}
			v, valid := ev.ValueInt()
			if !valid {
				return
			}
			if v > 0 {
				fc.Next()
			} else if v < 0 {
				fc.Previous()
			}
		case InputEventPress:
			s.cycleDeviceBrightness(deviceID)
		}
	case InputSourceNFC, InputSourcePIR, InputSourceMMWave, InputSourceLux:
		slog.Debug("input event has no binding and no default", "device", deviceID, "source", ev.Source, "event", ev.Event)
	}
}

// cycleDeviceBrightness steps through the 25/50/75/100 presets.
func (s *Server) cycleDeviceBrightness(deviceID int) {
	cur := s.currentDeviceBrightness(deviceID)
	next := 25
	switch {
	case cur >= 100:
		next = 25
	case cur >= 75:
		next = 100
	case cur >= 50:
		next = 75
	case cur >= 25:
		next = 50
	}
	s.ApplyDeviceBrightness(deviceID, next, "input")
}

func (s *Server) currentDeviceBrightness(deviceID int) int {
	if fc, ok := getDeviceFeed(deviceID); ok {
		if h, ok := fc.BrightnessHint(); ok {
			return h
		}
	}
	if s != nil && s.DB != nil {
		if d, err := s.DB.DeviceSettings.Get(s.inputCtx(), deviceID); err == nil && d.BrightnessOverride != nil {
			return *d.BrightnessOverride
		}
	}
	return 100
}

// executeInputAction converges every action kind on the existing subsystem.
// Failures are logged and never propagate: the device connection keeps
// streaming regardless of action outcome.
func (s *Server) executeInputAction(ctx context.Context, deviceID int, ev InputEvent, a InputAction) {
	switch a.Kind {
	case "scene":
		row, err := s.DB.Scene.Get(ctx, a.SceneID)
		if err != nil || !row.Enabled {
			slog.Info("input scene action target missing", "device", deviceID, "scene", a.SceneID)
			return
		}
		sc, err := sceneFromEnt(row)
		if err != nil {
			slog.Warn("input scene action decode failed", "device", deviceID, "scene", a.SceneID, "error", err)
			return
		}
		d := 60 * time.Second
		if sc.TTLSeconds != nil && *sc.TTLSeconds > 0 {
			d = time.Duration(*sc.TTLSeconds) * time.Second
		}
		PreviewScene(sc, func(x *Scene) (*sourceWithName, bool) {
			return resolveSceneSource(s.DB, x)
		}, d)
	case "playlist":
		row, err := s.DB.Playlist.Get(ctx, a.PlaylistID)
		if err != nil || !row.Enabled {
			slog.Info("input playlist action target missing", "device", deviceID, "playlist", a.PlaylistID)
			return
		}
		if err := s.persistPlaylistContent(deviceID, a.PlaylistID); err != nil {
			slog.Warn("input playlist action failed", "device", deviceID, "playlist", a.PlaylistID, "error", err)
			return
		}
		if fc, ok := getDeviceFeed(deviceID); ok {
			fc.RequestReload()
		}
		RestartTransportDevice(s, deviceID)
	case "feed":
		fc, ok := getDeviceFeed(deviceID)
		if !ok {
			fc = GlobalFeed
		}
		switch a.Verb {
		case "next":
			fc.Next()
		case "previous":
			fc.Previous()
		case "pause":
			fc.Pause()
		case "resume":
			fc.Resume()
		case "toggle":
			if fc.IsPaused() {
				fc.Resume()
			} else {
				fc.Pause()
			}
		}
	case "brightness":
		if a.Level == nil {
			return
		}
		s.ApplyDeviceBrightness(deviceID, *a.Level, "input")
	case "rule":
		row, err := s.DB.DisplayRule.Get(ctx, a.RuleID)
		if err != nil || !row.Enabled {
			slog.Info("input rule action target missing", "device", deviceID, "rule", a.RuleID)
			return
		}
		executeThenAction(ctx, s, row)
	case "greeting":
		row, err := s.DB.GreetingRule.Get(ctx, a.GreetingID)
		if err != nil || !row.Enabled {
			slog.Info("input greeting action target missing", "device", deviceID, "greeting", a.GreetingID)
			return
		}
		s.executeInputGreeting(ctx, deviceID, ev, row)
	case "notification":
		title := strings.TrimSpace(a.Title)
		if title == "" {
			title = "Input"
		}
		opts := []NotifOption{withTargetDevice(deviceID)}
		if a.TTLSeconds > 0 {
			opts = append(opts, WithTTL(time.Duration(a.TTLSeconds)*time.Second))
		}
		s.AddNotification(title, a.Message, opts...)
	default:
		slog.Warn("input action unknown kind", "kind", a.Kind)
	}
}

// executeInputGreeting reuses the greeting watcher semantics (quiet hours,
// cooldown, template resolution, last_triggered_at persistence) for a manual
// input trigger.
func (s *Server) executeInputGreeting(ctx context.Context, deviceID int, ev InputEvent, row *ent.GreetingRule) {
	now := time.Now()
	if InQuietHours(now, row.QuietHoursStart, row.QuietHoursEnd) {
		slog.Debug("input greeting suppressed by quiet hours", "greeting", row.Name)
		return
	}
	if row.LastTriggeredAt != nil && now.Sub(*row.LastTriggeredAt) < time.Duration(row.CooldownMinutes)*time.Minute {
		slog.Debug("input greeting suppressed by cooldown", "greeting", row.Name)
		return
	}
	evValue, _ := ev.ValueString()
	msg := ResolveTemplate(row.MessageTemplate, row, evValue, now)
	ttl := row.TTLSeconds
	if ttl <= 0 {
		ttl = 30
	}
	s.AddNotification(row.Name, msg, WithTTL(time.Duration(ttl)*time.Second), withTargetDevice(deviceID))
	if err := s.DB.GreetingRule.UpdateOneID(row.ID).SetLastTriggeredAt(now).Exec(ctx); err != nil {
		slog.Warn("input greeting last-triggered update failed", "greeting", row.ID, "error", err)
	}
}
