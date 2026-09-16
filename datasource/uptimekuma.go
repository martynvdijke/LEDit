package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*UptimeKumaDS)(nil)

// UptimeKumaDS fetches Uptime Kuma public status page data.
type UptimeKumaDS struct {
	Token string
	URL   string
}

// BuildUptimeKumaRows builds display rows from parsed data.
func BuildUptimeKumaRows(names map[string]string, heartbeats map[string][]map[string]any, uptimeList map[string]float64) ([][2]string, int, int) {
	// Collect monitor IDs from heartbeats or names
	idSet := map[string]bool{}
	for k := range heartbeats {
		idSet[k] = true
	}
	for k := range names {
		idSet[k] = true
	}
	ids := make([]string, 0, len(idSet))
	for k := range idSet {
		ids = append(ids, k)
	}
	sort.Strings(ids)

	up := 0
	down := 0
	var rows [][2]string
	for _, id := range ids {
		if len(rows) >= 4 {
			break
		}
		name := names[id]
		if name == "" {
			name = id
		}
		if len(name) > 20 {
			name = name[:20]
		}
		// Determine status from latest heartbeat
		status := 0
		hasHB := false
		if hbs, ok := heartbeats[id]; ok && len(hbs) > 0 {
			last := hbs[len(hbs)-1]
			if v, ok := last["status"]; ok {
				switch sv := v.(type) {
				case float64:
					status = int(sv)
					hasHB = true
				case int:
					status = sv
					hasHB = true
				case json.Number:
					if iv, err := sv.Int64(); err == nil {
						status = int(iv)
						hasHB = true
					}
				}
			}
		}
		// If no heartbeat, count as down
		if !hasHB {
			down++
			rows = append(rows, [2]string{name, "DOWN"})
			continue
		}
		if status == 1 {
			up++
			// lookup uptime
			key := id + "_24"
			val, ok := uptimeList[key]
			if !ok {
				// try any key with prefix id+"_"
				for k, v := range uptimeList {
					if strings.HasPrefix(k, id+"_") {
						val = v
						ok = true
						break
					}
				}
			}
			display := "UP"
			if ok {
				// uptimeList values are 0..1 fraction
				pct := val * 100
				// if value already looks like percent (>1), use directly
				if val > 1 && val <= 100 {
					pct = val
				}
				display = fmt.Sprintf("UP %d%%", int(pct+0.5))
			} else {
				display = "UP"
			}
			rows = append(rows, [2]string{name, display})
		} else {
			down++
			rows = append(rows, [2]string{name, "DOWN"})
		}
	}
	return rows, up, down
}

func UptimeKumaOverallStatus(up, down int) string {
	total := up + down
	if total == 0 {
		return "DOWN"
	}
	if down == 0 {
		return "UP"
	}
	if up == 0 {
		return "DOWN"
	}
	return "DEGRADED"
}

func UptimeKumaParseManifest(body []byte) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	names := map[string]string{}
	// Try publicGroupList
	if v, ok := raw["publicGroupList"]; ok {
		var groups []struct {
			MonitorList []struct {
				ID   json.Number `json:"id"`
				Name string      `json:"name"`
			} `json:"monitorList"`
		}
		if err := json.Unmarshal(v, &groups); err == nil {
			for _, g := range groups {
				for _, m := range g.MonitorList {
					id := m.ID.String()
					names[id] = m.Name
				}
			}
			return names, nil
		}
	}
	// tolerate other shapes: try to find any monitorList at top level
	return names, nil
}

func UptimeKumaParseHeartbeat(body []byte) (map[string][]map[string]any, map[string]float64, error) {
	var raw struct {
		HeartbeatList map[string][]map[string]any `json:"heartbeatList"`
		UptimeList    map[string]float64          `json:"uptimeList"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, err
	}
	if raw.HeartbeatList == nil {
		raw.HeartbeatList = map[string][]map[string]any{}
	}
	if raw.UptimeList == nil {
		raw.UptimeList = map[string]float64{}
	}
	return raw.HeartbeatList, raw.UptimeList, nil
}

func (u *UptimeKumaDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if strings.TrimSpace(u.URL) == "" || strings.TrimSpace(u.Token) == "" {
		slog.Warn("uptimekuma not configured, using fallback", "source", "uptimekuma")
		return fallbackUptimeKuma(width, height), nil
	}
	base := strings.TrimRight(u.URL, "/")
	slug := u.Token

	// Manifest
	manifestURL := base + "/api/status-page/" + slug
	names := map[string]string{}
	manifestBody, err := apiGet(manifestURL, "", nil)
	if err != nil {
		slog.Warn("uptimekuma manifest fetch failed, falling back to IDs", "source", "uptimekuma", "error", err)
	} else {
		parsed, perr := UptimeKumaParseManifest(manifestBody)
		if perr != nil {
			slog.Warn("uptimekuma manifest parse failed, falling back to IDs", "source", "uptimekuma", "error", perr)
		} else {
			names = parsed
		}
	}

	// Heartbeat
	hbURL := base + "/api/status-page/heartbeat/" + slug
	hbBody, err := apiGet(hbURL, "", nil)
	if err != nil {
		slog.Warn("uptimekuma heartbeat fetch failed, using fallback", "source", "uptimekuma", "error", err)
		return fallbackUptimeKuma(width, height), nil
	}
	hbList, upList, err := UptimeKumaParseHeartbeat(hbBody)
	if err != nil {
		slog.Warn("uptimekuma heartbeat parse failed, using fallback", "source", "uptimekuma", "error", err)
		return fallbackUptimeKuma(width, height), nil
	}
	if len(hbList) == 0 {
		slog.Warn("uptimekuma no heartbeat data, using fallback", "source", "uptimekuma")
		return fallbackUptimeKuma(width, height), nil
	}

	rows, up, down := BuildUptimeKumaRows(names, hbList, upList)
	overall := UptimeKumaOverallStatus(up, down)

	data := make(map[string]string, len(rows)+1)
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	data["STATUS"] = overall

	theme := DefaultTheme()
	theme.Title = "UPTIME KUMA"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (u *UptimeKumaDS) CurrentState(_ context.Context) (map[string]any, error) {
	if strings.TrimSpace(u.URL) == "" || strings.TrimSpace(u.Token) == "" {
		return map[string]any{"up": 0, "down": 0, "total": 0, "status": "UNKNOWN"}, nil
	}
	base := strings.TrimRight(u.URL, "/")
	slug := u.Token
	hbURL := base + "/api/status-page/heartbeat/" + slug
	hbBody, err := apiGet(hbURL, "", nil)
	if err != nil {
		return map[string]any{"up": 0, "down": 0, "total": 0, "status": "UNKNOWN"}, nil
	}
	hbList, _, err := UptimeKumaParseHeartbeat(hbBody)
	if err != nil {
		return map[string]any{"up": 0, "down": 0, "total": 0, "status": "UNKNOWN"}, nil
	}
	up := 0
	down := 0
	for _, hbs := range hbList {
		if len(hbs) == 0 {
			down++
			continue
		}
		last := hbs[len(hbs)-1]
		status := 0
		has := false
		if v, ok := last["status"]; ok {
			switch sv := v.(type) {
			case float64:
				status = int(sv)
				has = true
			case int:
				status = sv
				has = true
			}
		}
		if !has {
			down++
			continue
		}
		if status == 1 {
			up++
		} else {
			down++
		}
	}
	total := up + down
	statusStr := UptimeKumaOverallStatus(up, down)
	if total == 0 {
		statusStr = "UNKNOWN"
	}
	return map[string]any{"up": up, "down": down, "total": total, "status": statusStr}, nil
}

func fallbackUptimeKuma(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "UPTIME KUMA"
	data := map[string]string{"UPTIME KUMA": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}
