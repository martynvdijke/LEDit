package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*OverseerrDS)(nil)

// OverseerrDS fetches Overseerr/Jellyseerr request counts.
type OverseerrDS struct {
	Token string
	URL   string
}

// OverseerrCounts holds parsed overseerr counts.
type OverseerrCounts struct {
	Pending    int `json:"pending"`
	Approved   int `json:"approved"`
	Processing int `json:"processing"`
	Available  int `json:"available"`
	Total      int `json:"total"`
}

// BuildOverseerrRows parses overseerr count JSON and returns rows.
func BuildOverseerrRows(body []byte) ([][2]string, OverseerrCounts, error) {
	var c OverseerrCounts
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, OverseerrCounts{}, fmt.Errorf("overseerr parse error: %w", err)
	}
	rows := [][2]string{
		{"PENDING", fmt.Sprintf("%d", c.Pending)},
		{"APPROVED", fmt.Sprintf("%d", c.Approved)},
		{"PROCESSING", fmt.Sprintf("%d", c.Processing)},
		{"AVAILABLE", fmt.Sprintf("%d", c.Available)},
	}
	return rows, c, nil
}

func (o *OverseerrDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if strings.TrimSpace(o.URL) == "" || strings.TrimSpace(o.Token) == "" {
		slog.Warn("overseerr not configured, using fallback", "source", "overseerr")
		return fallbackOverseerr(width, height), nil
	}
	base := strings.TrimRight(o.URL, "/")
	fetchURL := base + "/api/v1/request/count"
	body, err := apiGet(fetchURL, o.Token, nil)
	if err != nil {
		slog.Warn("overseerr API call failed, using fallback", "source", "overseerr", "error", err)
		return fallbackOverseerr(width, height), nil
	}
	rows, _, err := BuildOverseerrRows(body)
	if err != nil {
		slog.Warn("overseerr parse failed, using fallback", "source", "overseerr", "error", err)
		return fallbackOverseerr(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "OVERSEERR"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (o *OverseerrDS) CurrentState(_ context.Context) (map[string]any, error) {
	if strings.TrimSpace(o.URL) == "" || strings.TrimSpace(o.Token) == "" {
		return map[string]any{"pending": 0, "approved": 0, "processing": 0, "available": 0, "total": 0}, nil
	}
	base := strings.TrimRight(o.URL, "/")
	fetchURL := base + "/api/v1/request/count"
	body, err := apiGet(fetchURL, o.Token, nil)
	if err != nil {
		return map[string]any{"pending": 0, "approved": 0, "processing": 0, "available": 0, "total": 0}, nil
	}
	var c OverseerrCounts
	if err := json.Unmarshal(body, &c); err != nil {
		return map[string]any{"pending": 0, "approved": 0, "processing": 0, "available": 0, "total": 0}, nil
	}
	return map[string]any{"pending": c.Pending, "approved": c.Approved, "processing": c.Processing, "available": c.Available, "total": c.Total}, nil
}

func fallbackOverseerr(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "OVERSEERR"
	data := map[string]string{"OVERSEERR": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}
