package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*FrigateDS)(nil)

// FrigateDS fetches Frigate NVR stats.
// ponytail: single stats endpoint, no per-camera history
type FrigateDS struct {
	Token string
	URL   string
}

// BuildFrigateRows parses Frigate stats JSON.
// Supports {"cameras":{"front":{"camera_fps":float,"detection_fps":float},...}} OR {"stats":{"camera_fps":...}}
func BuildFrigateRows(body []byte) ([][2]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("frigate parse error: %w", err)
	}
	// If top-level has stats wrapper, unwrap? But cameras case also possible.
	// Determine cameras map
	var cameras map[string]struct {
		CameraFps    float64 `json:"camera_fps"`
		DetectionFps float64 `json:"detection_fps"`
	}
	if v, ok := raw["cameras"]; ok {
		if err := json.Unmarshal(v, &cameras); err != nil {
			return nil, fmt.Errorf("frigate parse error: %w", err)
		}
	} else if v, ok := raw["stats"]; ok {
		// stats wrapper may contain cameras or direct fps
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(v, &inner); err == nil {
			if cv, ok := inner["cameras"]; ok {
				_ = json.Unmarshal(cv, &cameras)
			} else if _, ok := inner["camera_fps"]; ok {
				// single stats object, no camera breakdown
				var s struct {
					CameraFps float64 `json:"camera_fps"`
				}
				_ = json.Unmarshal(v, &s)
				return [][2]string{{"FPS", fmt.Sprintf("%.1f", s.CameraFps)}}, nil
			}
		}
		if cameras == nil {
			// try stats directly as camera_fps
			var s struct {
				CameraFps float64 `json:"camera_fps"`
			}
			if err := json.Unmarshal(v, &s); err == nil && s.CameraFps != 0 {
				return [][2]string{{"FPS", fmt.Sprintf("%.1f", s.CameraFps)}}, nil
			}
		}
	}
	if cameras == nil || len(cameras) == 0 {
		// fallback: check top-level camera_fps
		var s struct {
			CameraFps float64 `json:"camera_fps"`
		}
		if err := json.Unmarshal(body, &s); err == nil && s.CameraFps != 0 {
			return [][2]string{{"FPS", fmt.Sprintf("%.1f", s.CameraFps)}}, nil
		}
		return [][2]string{{"FRIGATE", "unavailable"}}, nil
	}
	// compute avg fps
	var total float64
	for _, c := range cameras {
		total += c.CameraFps
	}
	avg := total / float64(len(cameras))
	rows := [][2]string{
		{"CAMS", fmt.Sprintf("%d", len(cameras))},
		{"FPS", fmt.Sprintf("%.1f", avg)},
	}
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
	}
	return rows, nil
}

func fallbackFrigate(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "FRIGATE"
	data := map[string]string{"FRIGATE": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (f *FrigateDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := strings.TrimSpace(f.URL)
	if base == "" {
		base = "http://localhost:5000/api/stats"
	}
	slog.Info("fetching frigate data", "source", "frigate")
	body, err := apiGet(base, f.Token, nil)
	if err != nil {
		slog.Warn("frigate API call failed, using fallback", "source", "frigate", "error", err)
		return fallbackFrigate(width, height), nil
	}
	rows, err := BuildFrigateRows(body)
	if err != nil {
		slog.Warn("frigate parse failed, using fallback", "source", "frigate", "error", err)
		return fallbackFrigate(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "FRIGATE"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (f *FrigateDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := strings.TrimSpace(f.URL)
	if base == "" {
		base = "http://localhost:5000/api/stats"
	}
	body, err := apiGet(base, f.Token, nil)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var cameras map[string]struct {
		CameraFps    float64 `json:"camera_fps"`
		DetectionFps float64 `json:"detection_fps"`
	}
	var fps float64
	var count int
	if v, ok := raw["cameras"]; ok {
		if err := json.Unmarshal(v, &cameras); err == nil && len(cameras) > 0 {
			count = len(cameras)
			var tot float64
			for _, c := range cameras {
				tot += c.CameraFps
			}
			fps = tot / float64(count)
			return map[string]any{"cameras": count, "fps": fps}, nil
		}
	}
	if v, ok := raw["stats"]; ok {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(v, &inner); err == nil {
			if cv, ok := inner["cameras"]; ok {
				if err := json.Unmarshal(cv, &cameras); err == nil && len(cameras) > 0 {
					var tot float64
					for _, c := range cameras {
						tot += c.CameraFps
					}
					fps = tot / float64(len(cameras))
					return map[string]any{"cameras": len(cameras), "fps": fps}, nil
				}
			} else if _, ok := inner["camera_fps"]; ok {
				var s struct {
					CameraFps float64 `json:"camera_fps"`
				}
				_ = json.Unmarshal(v, &s)
				return map[string]any{"cameras": 0, "fps": s.CameraFps}, nil
			}
		}
		var s struct {
			CameraFps float64 `json:"camera_fps"`
		}
		if err := json.Unmarshal(v, &s); err == nil && s.CameraFps != 0 {
			return map[string]any{"cameras": 0, "fps": s.CameraFps}, nil
		}
	}
	var s struct {
		CameraFps float64 `json:"camera_fps"`
	}
	if err := json.Unmarshal(body, &s); err == nil && s.CameraFps != 0 {
		return map[string]any{"cameras": 0, "fps": s.CameraFps}, nil
	}
	return map[string]any{"cameras": 0, "fps": float64(0)}, nil
}
