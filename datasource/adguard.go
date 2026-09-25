package datasource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*AdGuardDS)(nil)

// AdGuardDS fetches AdGuard Home stats.
// ponytail: single stats endpoint, no history
type AdGuardDS struct {
	Token string
	URL   string
}

// BuildAdGuardRows parses AdGuard stats JSON.
// Supports flat {"num_blocked_filtering":int,"num_dns_queries":int,"avg_processing_time":float} OR {"stats":{...}}
func BuildAdGuardRows(body []byte) ([][2]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("adguard parse error: %w", err)
	}
	// If top-level has "stats", unwrap it
	if v, ok := raw["stats"]; ok {
		body = v
	}
	var s struct {
		NumBlockedFiltering int     `json:"num_blocked_filtering"`
		NumDNSQueries       int     `json:"num_dns_queries"`
		AvgProcessingTime   float64 `json:"avg_processing_time"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("adguard parse error: %w", err)
	}
	// Require at least one field present in raw
	if _, ok := raw["stats"]; !ok {
		if _, a := raw["num_blocked_filtering"]; !a {
			if _, b := raw["num_dns_queries"]; !b {
				if _, c := raw["avg_processing_time"]; !c {
					return nil, fmt.Errorf("adguard: missing stats fields")
				}
			}
		}
	}
	rows := [][2]string{
		{"BLOCKED", fmt.Sprintf("%d", s.NumBlockedFiltering)},
		{"QUERIES", fmt.Sprintf("%d", s.NumDNSQueries)},
	}
	if s.AvgProcessingTime != 0 {
		rows = append(rows, [2]string{"AVG MS", fmt.Sprintf("%.1f", s.AvgProcessingTime)})
	}
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
	}
	return rows, nil
}

func fallbackAdGuard(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "ADGUARD"
	data := map[string]string{"ADGUARD": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (a *AdGuardDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := a.URL
	if strings.TrimSpace(base) == "" {
		base = "http://localhost:3000/control/stats"
	}
	headers := map[string]string{}
	if strings.Contains(a.Token, ":") {
		parts := strings.SplitN(a.Token, ":", 2)
		creds := base64.StdEncoding.EncodeToString([]byte(parts[0] + ":" + parts[1]))
		headers["Authorization"] = "Basic " + creds
	}
	var token string
	if !strings.Contains(a.Token, ":") {
		token = a.Token
	}
	slog.Info("fetching adguard data", "source", "adguard")
	body, err := apiGet(base, token, headers)
	if err != nil {
		slog.Warn("adguard API call failed, using fallback", "source", "adguard", "error", err)
		return fallbackAdGuard(width, height), nil
	}
	rows, err := BuildAdGuardRows(body)
	if err != nil {
		slog.Warn("adguard parse failed, using fallback", "source", "adguard", "error", err)
		return fallbackAdGuard(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "ADGUARD"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (a *AdGuardDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := a.URL
	if strings.TrimSpace(base) == "" {
		base = "http://localhost:3000/control/stats"
	}
	headers := map[string]string{}
	if strings.Contains(a.Token, ":") {
		parts := strings.SplitN(a.Token, ":", 2)
		creds := base64.StdEncoding.EncodeToString([]byte(parts[0] + ":" + parts[1]))
		headers["Authorization"] = "Basic " + creds
	}
	var token string
	if !strings.Contains(a.Token, ":") {
		token = a.Token
	}
	body, err := apiGet(base, token, headers)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if v, ok := raw["stats"]; ok {
		body = v
	}
	var s struct {
		NumBlockedFiltering int `json:"num_blocked_filtering"`
		NumDNSQueries       int `json:"num_dns_queries"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return map[string]any{"blocked": s.NumBlockedFiltering, "queries": s.NumDNSQueries}, nil
}
