package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*Zigbee2MQTTDS)(nil)

// Zigbee2MQTTDS fetches Zigbee2MQTT device list.
// ponytail: single device list endpoint, no per-device history
type Zigbee2MQTTDS struct {
	Token string
	URL   string
}

// BuildZigbee2MQTTRows parses Zigbee2MQTT devices array.
// Expected: [{"friendly_name":string,"last_seen":string,"linkquality":int}]
func BuildZigbee2MQTTRows(body []byte) ([][2]string, error) {
	var devices []struct {
		FriendlyName string `json:"friendly_name"`
		LastSeen     string `json:"last_seen"`
		LinkQuality  *int   `json:"linkquality"`
	}
	if err := json.Unmarshal(body, &devices); err != nil {
		return nil, fmt.Errorf("zigbee2mqtt parse error: %w", err)
	}
	if len(devices) == 0 {
		return [][2]string{{"ZIGBEE", "unavailable"}}, nil
	}
	low := 0
	for _, d := range devices {
		if d.LinkQuality != nil && *d.LinkQuality < 50 {
			low++
		}
	}
	rows := [][2]string{
		{"DEVICES", fmt.Sprintf("%d", len(devices))},
	}
	if low > 0 {
		rows = append(rows, [2]string{"LOW", fmt.Sprintf("%d", low)})
	} else {
		rows = append(rows, [2]string{"LINK", "OK"})
	}
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
	}
	return rows, nil
}

func fallbackZigbee2MQTT(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "ZIGBEE"
	data := map[string]string{"ZIGBEE": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (z *Zigbee2MQTTDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := strings.TrimSpace(z.URL)
	if base == "" {
		base = "http://localhost:8080/api/devices"
	}
	slog.Info("fetching zigbee2mqtt data", "source", "zigbee2mqtt")
	body, err := apiGet(base, z.Token, nil)
	if err != nil {
		slog.Warn("zigbee2mqtt API call failed, using fallback", "source", "zigbee2mqtt", "error", err)
		return fallbackZigbee2MQTT(width, height), nil
	}
	rows, err := BuildZigbee2MQTTRows(body)
	if err != nil {
		slog.Warn("zigbee2mqtt parse failed, using fallback", "source", "zigbee2mqtt", "error", err)
		return fallbackZigbee2MQTT(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "ZIGBEE"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (z *Zigbee2MQTTDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := strings.TrimSpace(z.URL)
	if base == "" {
		base = "http://localhost:8080/api/devices"
	}
	body, err := apiGet(base, z.Token, nil)
	if err != nil {
		return nil, err
	}
	var devices []struct {
		LinkQuality *int `json:"linkquality"`
	}
	if err := json.Unmarshal(body, &devices); err != nil {
		return nil, err
	}
	low := 0
	for _, d := range devices {
		if d.LinkQuality != nil && *d.LinkQuality < 50 {
			low++
		}
	}
	return map[string]any{"devices": len(devices), "low": low}, nil
}
