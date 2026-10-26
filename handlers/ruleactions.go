package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"ledit/ent"
)

// ThenAction describes the optional "then" effect fired on a rising edge.
// Kinds: none | scene | notification | webhook | brightness | source | playlist | mqtt | http.
type ThenAction struct {
	Kind       string `json:"kind"`
	SceneID    int    `json:"scene_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Message    string `json:"message,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
	Event      string `json:"event,omitempty"`   // webhook/http event name
	Payload    string `json:"payload,omitempty"` // optional JSON object string (webhook/mqtt)

	// brightness
	DeviceID int  `json:"device_id,omitempty"` // 0 = all enabled devices (brightness only)
	Level    *int `json:"level,omitempty"`     // brightness 0..100; pointer so an explicit 0 is meaningful

	// source
	SourceType string `json:"source_type,omitempty"`
	SourceID   int    `json:"source_id,omitempty"`

	// playlist
	PlaylistID int `json:"playlist_id,omitempty"`

	// mqtt
	Topic  string `json:"topic,omitempty"`
	Retain bool   `json:"retain,omitempty"`

	// http
	URL    string `json:"url,omitempty"`
	Method string `json:"method,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// ParseThenAction decodes raw JSON. Empty string or "{}" maps to Kind "none".
func ParseThenAction(raw string) (ThenAction, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return ThenAction{Kind: "none"}, nil
	}
	var ta ThenAction
	if err := json.Unmarshal([]byte(trimmed), &ta); err != nil {
		return ThenAction{}, err
	}
	if strings.TrimSpace(ta.Kind) == "" {
		ta.Kind = "none"
	}
	switch ta.Kind {
	case "none", "scene", "notification", "webhook", "brightness", "source", "playlist", "mqtt", "http":
	default:
		return ThenAction{}, fmt.Errorf("unknown kind %q", ta.Kind)
	}
	return ta, nil
}

// validateMQTTTopic enforces the outbound topic shape: non-empty, bounded,
// no wildcards, and no leading '$' (reserved for broker internals).
func validateMQTTTopic(topic string) string {
	t := strings.TrimSpace(topic)
	if t == "" {
		return "topic is required for mqtt actions"
	}
	if len(t) > 256 {
		return "topic must be at most 256 bytes"
	}
	if strings.HasPrefix(t, "$") {
		return "topic must not start with $"
	}
	if strings.ContainsAny(t, "+#") {
		return "topic must not contain wildcards"
	}
	return ""
}

// validateThenAction returns "" if ok else a human-readable message.
func validateThenAction(ta ThenAction) string {
	switch ta.Kind {
	case "none", "":
		return ""
	case "scene":
		if ta.SceneID == 0 {
			return "scene_id is required for scene action"
		}
		return ""
	case "notification":
		if strings.TrimSpace(ta.Message) == "" {
			return "message is required for notification action"
		}
		return ""
	case "webhook":
		if strings.TrimSpace(ta.Payload) != "" {
			var js any
			if err := json.Unmarshal([]byte(ta.Payload), &js); err != nil {
				return "payload must be valid JSON"
			}
			if _, ok := js.(map[string]any); !ok {
				return "payload must be a JSON object"
			}
		}
		return ""
	case "brightness":
		if ta.DeviceID < 0 {
			return "device_id must be >= 0 for brightness action"
		}
		if ta.Level == nil {
			return "level is required for brightness action"
		}
		if *ta.Level < 0 || *ta.Level > 100 {
			return "level must be between 0 and 100"
		}
		return ""
	case "source":
		if strings.TrimSpace(ta.SourceType) == "" {
			return "source_type is required for source action"
		}
		if ta.SourceID <= 0 {
			return "source_id must be positive for source action"
		}
		return ""
	case "playlist":
		if ta.DeviceID <= 0 {
			return "device_id is required for playlist action"
		}
		if ta.PlaylistID <= 0 {
			return "playlist_id is required for playlist action"
		}
		return ""
	case "mqtt":
		if msg := validateMQTTTopic(ta.Topic); msg != "" {
			return msg
		}
		if len(ta.Payload) > 4096 {
			return "payload must be at most 4096 bytes"
		}
		if strings.TrimSpace(ta.Payload) != "" {
			var js any
			if err := json.Unmarshal([]byte(ta.Payload), &js); err != nil {
				return "payload must be valid JSON"
			}
			if _, ok := js.(map[string]any); !ok {
				return "payload must be a JSON object"
			}
		}
		return ""
	case "http":
		u, err := url.Parse(strings.TrimSpace(ta.URL))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "url must be a valid http(s) URL"
		}
		switch strings.ToUpper(strings.TrimSpace(ta.Method)) {
		case "", "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return "method must be one of GET, POST, PUT, PATCH, DELETE"
		}
		return ""
	default:
		return "unknown kind: " + ta.Kind
	}
}

func executeThenAction(ctx context.Context, s *Server, rule *ent.DisplayRule) {
	if rule == nil {
		return
	}
	raw := rule.ThenActions
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return
	}
	ta, err := ParseThenAction(raw)
	if err != nil {
		slog.Warn("event rule then_actions parse failed", "rule", rule.Name, "error", err)
		return
	}
	if ta.Kind == "none" || ta.Kind == "" {
		return
	}
	if msg := validateThenAction(ta); msg != "" {
		slog.Warn("event rule then_actions invalid, skipping", "rule", rule.Name, "error", msg)
		return
	}
	var client *ent.Client
	if s != nil {
		client = s.DB
	}
	switch ta.Kind {
	case "scene":
		if client == nil {
			slog.Warn("event rule scene action: no db client", "rule", rule.Name)
			return
		}
		row, err := client.Scene.Get(ctx, ta.SceneID)
		if err != nil {
			slog.Warn("event rule scene not found", "rule", rule.Name, "scene_id", ta.SceneID, "error", err)
			return
		}
		sc, err := sceneFromEnt(row)
		if err != nil {
			slog.Warn("event rule scene parse failed", "rule", rule.Name, "error", err)
			return
		}
		d := time.Duration(ta.TTLSeconds) * time.Second
		if d <= 0 {
			d = 60 * time.Second
		}
		ok := PreviewScene(sc, func(x *Scene) (*sourceWithName, bool) { return resolveSceneSource(client, x) }, d)
		if !ok {
			slog.Warn("event rule scene not resolvable", "rule", rule.Name, "scene_id", ta.SceneID)
		}
	case "notification":
		title := ta.Title
		if strings.TrimSpace(title) == "" {
			title = rule.Name
		}
		message := ta.Message
		var ttl time.Duration
		if ta.TTLSeconds > 0 {
			ttl = time.Duration(ta.TTLSeconds) * time.Second
		}
		if client != nil {
			if _, err := client.Notification.Create().SetTitle(title).SetMessage(message).SetCreatedAt(time.Now()).Save(ctx); err != nil {
				slog.Warn("event rule notification persist failed", "rule", rule.Name, "error", err)
			}
		}
		entry := addToMemoryQueueWithOptions(title, message, WithTTL(ttl))
		GlobalBus.Emit(Event{Type: EventNotificationFired, Timestamp: time.Now(), Data: map[string]any{"title": title, "message": message, "source": "eventrule", "rule": rule.Name}})
		emitMessageFired(NotificationToMessage(entry))
	case "webhook":
		evName := ta.Event
		if strings.TrimSpace(evName) == "" {
			evName = EventRuleTriggered
		}
		var payload any
		if strings.TrimSpace(ta.Payload) != "" {
			var m map[string]any
			if err := json.Unmarshal([]byte(ta.Payload), &m); err == nil {
				payload = m
			}
		}
		GlobalBus.Emit(Event{Type: evName, Timestamp: time.Now(), Data: map[string]any{"rule": rule.Name, "title": ta.Title, "message": ta.Message, "payload": payload}})
	case "brightness":
		if s == nil || s.DB == nil {
			slog.Warn("event rule brightness action: no server", "rule", rule.Name)
			return
		}
		src := "rule:" + rule.Name
		if ta.DeviceID > 0 {
			s.ApplyDeviceBrightness(ta.DeviceID, *ta.Level, src)
			return
		}
		devs, err := s.DB.DeviceSettings.Query().All(ctx)
		if err != nil {
			slog.Warn("event rule brightness action: device query failed", "rule", rule.Name, "error", err)
			return
		}
		for _, d := range devs {
			if d.Enabled {
				s.ApplyDeviceBrightness(d.ID, *ta.Level, src)
			}
		}
	case "source":
		key := fmt.Sprintf("%s:%d", ta.SourceType, ta.SourceID)
		pinAll(key, "rule:"+rule.Name)
	case "playlist":
		if s == nil || s.DB == nil {
			slog.Warn("event rule playlist action: no server", "rule", rule.Name)
			return
		}
		if _, err := s.DB.DeviceSettings.Get(ctx, ta.DeviceID); err != nil {
			slog.Warn("event rule playlist action: device not found", "rule", rule.Name, "device_id", ta.DeviceID, "error", err)
			return
		}
		pl, err := s.DB.Playlist.Get(ctx, ta.PlaylistID)
		if err != nil || !pl.Enabled {
			slog.Warn("event rule playlist action: playlist unavailable", "rule", rule.Name, "playlist_id", ta.PlaylistID)
			return
		}
		if err := s.persistPlaylistContent(ta.DeviceID, ta.PlaylistID); err != nil {
			slog.Warn("event rule playlist action: content update failed", "rule", rule.Name, "error", err)
			return
		}
		RestartTransportDevice(s, ta.DeviceID)
	case "mqtt":
		if !mqttConnected() {
			slog.Debug("event rule mqtt action skipped: mqtt disconnected", "rule", rule.Name, "topic", ta.Topic)
			return
		}
		body := strings.TrimSpace(ta.Payload)
		if body == "" {
			env, _ := json.Marshal(map[string]any{
				"event":     EventRuleTriggered,
				"rule":      rule.Name,
				"fire_time": time.Now().Format(time.RFC3339),
			})
			body = string(env)
		}
		PublishOutbound(ta.Topic, body, ta.Retain)
	case "http":
		enqueueRuleHTTP(rule, ta)
	}
}
