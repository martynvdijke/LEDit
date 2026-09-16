package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ledit/render"
)

var _ StateProvider = (*SpeedtestDS)(nil)

// SpeedtestDS fetches speedtest-tracker latest result.
type SpeedtestDS struct {
	Token string
	URL   string
}

// SpeedtestResult holds normalized fields.
type SpeedtestResult struct {
	Download float64
	Upload   float64
	Ping     float64
	When     string
	Healthy  bool
}

func SpeedtestNormalizeMbps(v float64) float64 {
	if v > 100000 {
		return v / 1e6
	}
	return v
}

func SpeedtestFormatMbps(v float64) string {
	mbps := SpeedtestNormalizeMbps(v)
	return fmt.Sprintf("%.1f Mbps", mbps)
}

func SpeedtestParseBody(body []byte) (SpeedtestResult, error) {
	// Tolerate {"data":{...}} and bare object
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return SpeedtestResult{}, fmt.Errorf("speedtest parse error: %w", err)
	}
	var raw json.RawMessage
	if d, ok := wrapper["data"]; ok {
		// Check if data is not null
		if string(d) != "null" {
			raw = d
		} else {
			raw = body
		}
	} else {
		raw = body
	}
	// If wrapper had data key but inner is object, use it; otherwise use body
	// Detect if raw is empty -> use body
	if len(raw) == 0 {
		raw = body
	}
	// If body was bare object, wrapper will contain keys like download etc, not data; we already set raw=body
	// But if body had {"data":{...}}, raw is inner object.

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return SpeedtestResult{}, fmt.Errorf("speedtest parse error: %w", err)
	}
	// Must have at least one of download/upload/ping/created_at ?
	// Check fields existence
	var dlVal float64
	var ulVal float64
	var pingVal float64
	var createdAt string

	// download can be "download" or "download_bits" etc; try "download"
	if v, ok := obj["download"]; ok {
		var n json.Number
		if err := json.Unmarshal(v, &n); err == nil {
			if f, err := n.Float64(); err == nil {
				dlVal = f
			}
		} else {
			var f float64
			if err := json.Unmarshal(v, &f); err == nil {
				dlVal = f
			}
		}
	}
	if v, ok := obj["upload"]; ok {
		var n json.Number
		if err := json.Unmarshal(v, &n); err == nil {
			if f, err := n.Float64(); err == nil {
				ulVal = f
			}
		} else {
			var f float64
			if err := json.Unmarshal(v, &f); err == nil {
				ulVal = f
			}
		}
	}
	if v, ok := obj["ping"]; ok {
		var n json.Number
		if err := json.Unmarshal(v, &n); err == nil {
			if f, err := n.Float64(); err == nil {
				pingVal = f
			}
		} else {
			var f float64
			if err := json.Unmarshal(v, &f); err == nil {
				pingVal = f
			}
		}
	}
	if v, ok := obj["created_at"]; ok {
		_ = json.Unmarshal(v, &createdAt)
	} else if v, ok := obj["createdAt"]; ok {
		_ = json.Unmarshal(v, &createdAt)
	}

	// Validate at least download or ping present? If all zero and no created_at, treat as malformed if no expected keys present
	hasAny := false
	for _, k := range []string{"download", "upload", "ping", "created_at", "createdAt"} {
		if _, ok := obj[k]; ok {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return SpeedtestResult{}, fmt.Errorf("speedtest: missing fields")
	}

	when := ""
	if createdAt != "" {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			when = t.Format("2006-01-02 15:04")
		} else if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			when = t.Format("2006-01-02 15:04")
		} else {
			when = createdAt
		}
	}

	dlMbps := SpeedtestNormalizeMbps(dlVal)
	ulMbps := SpeedtestNormalizeMbps(ulVal)
	healthy := dlMbps > 0 || ulMbps > 0 || pingVal > 0

	return SpeedtestResult{Download: dlMbps, Upload: ulMbps, Ping: pingVal, When: when, Healthy: healthy}, nil
}

func SpeedtestBuildRows(r SpeedtestResult) [][2]string {
	rows := [][2]string{
		{"DL", fmt.Sprintf("%.1f Mbps", r.Download)},
		{"UL", fmt.Sprintf("%.1f Mbps", r.Upload)},
		{"PING", fmt.Sprintf("%.0f ms", r.Ping)},
		{"WHEN", r.When},
	}
	return rows
}

func (s *SpeedtestDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if strings.TrimSpace(s.URL) == "" || strings.TrimSpace(s.Token) == "" {
		slog.Warn("speedtest not configured, using fallback", "source", "speedtest")
		return fallbackSpeedtest(width, height), nil
	}
	base := strings.TrimRight(s.URL, "/")
	fetchURL := base + "/api/v1/results/latest"
	headers := map[string]string{"Accept": "application/json", "Authorization": "Bearer " + s.Token}
	body, err := apiGet(fetchURL, "", headers)
	if err != nil {
		slog.Warn("speedtest API call failed, using fallback", "source", "speedtest", "error", err)
		return fallbackSpeedtest(width, height), nil
	}
	res, err := SpeedtestParseBody(body)
	if err != nil {
		slog.Warn("speedtest parse failed, using fallback", "source", "speedtest", "error", err)
		return fallbackSpeedtest(width, height), nil
	}
	rows := SpeedtestBuildRows(res)
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "SPEEDTEST"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (s *SpeedtestDS) CurrentState(_ context.Context) (map[string]any, error) {
	if strings.TrimSpace(s.URL) == "" || strings.TrimSpace(s.Token) == "" {
		return map[string]any{"download": float64(0), "upload": float64(0), "ping": float64(0), "healthy": false}, nil
	}
	base := strings.TrimRight(s.URL, "/")
	fetchURL := base + "/api/v1/results/latest"
	headers := map[string]string{"Accept": "application/json", "Authorization": "Bearer " + s.Token}
	body, err := apiGet(fetchURL, "", headers)
	if err != nil {
		return map[string]any{"download": float64(0), "upload": float64(0), "ping": float64(0), "healthy": false}, nil
	}
	res, err := SpeedtestParseBody(body)
	if err != nil {
		return map[string]any{"download": float64(0), "upload": float64(0), "ping": float64(0), "healthy": false}, nil
	}
	return map[string]any{"download": res.Download, "upload": res.Upload, "ping": res.Ping, "healthy": res.Healthy}, nil
}

func fallbackSpeedtest(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "SPEEDTEST"
	data := map[string]string{"SPEEDTEST": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}
