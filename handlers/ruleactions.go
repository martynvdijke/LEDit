package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ledit/ent"
)

// ThenAction describes the optional "then" effect fired on a rising edge.
type ThenAction struct {
	Kind       string `json:"kind"` // none|scene|notification|webhook
	SceneID    int    `json:"scene_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Message    string `json:"message,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
	Event      string `json:"event,omitempty"`   // webhook event name
	Payload    string `json:"payload,omitempty"` // optional JSON object string
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
	case "none", "scene", "notification", "webhook":
	default:
		return ThenAction{}, fmt.Errorf("unknown kind %q", ta.Kind)
	}
	return ta, nil
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
	default:
		return "unknown kind: " + ta.Kind
	}
}

func executeThenAction(ctx context.Context, client *ent.Client, rule *ent.DisplayRule) {
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
	}
}
