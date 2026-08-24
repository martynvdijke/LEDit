package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"ledit/render"
)

// PiHoleDS fetches Pi-hole summary statistics.
//
// DEVIATION from house convention: Pi-hole authenticates via query parameter
// `auth` rather than the standard X-API-Key header. If the configured URL does
// not already contain an `auth=` query parameter, `&auth=<token>` (or
// `?auth=` when no query string exists) is appended. The token is NOT sent as
// X-API-Key; apiGet is called with an empty token so no X-API-Key header is
// added. Documented here intentionally.
type PiHoleDS struct {
	Token string
	URL   string
}

// piholeURL returns the effective request URL, appending auth token as query
// param when not already present.
func piholeURL(base, token string) string {
	if strings.Contains(base, "auth=") {
		return base
	}
	if token == "" {
		return base
	}
	// Prefer url.Parse for correct handling of existing query strings.
	u, err := url.Parse(base)
	if err != nil {
		// Fallback string logic
		if strings.Contains(base, "?") {
			return base + "&auth=" + url.QueryEscape(token)
		}
		return base + "?auth=" + url.QueryEscape(token)
	}
	q := u.Query()
	q.Set("auth", token)
	u.RawQuery = q.Encode()
	return u.String()
}

// BuildPiholeRows parses Pi-hole summary JSON and returns up to 4 rows.
// Expected JSON: {"status":"enabled","queries_today":1234,"ads_blocked_today":56,"ads_percentage_today":42.5}
func BuildPiholeRows(body []byte) ([][2]string, error) {
	var resp struct {
		Status             string  `json:"status"`
		QueriesToday       int     `json:"queries_today"`
		AdsBlockedToday    int     `json:"ads_blocked_today"`
		AdsPercentageToday float64 `json:"ads_percentage_today"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("pihole parse error: %w", err)
	}
	// status is required; if empty and all numeric zero we treat as malformed?
	// Use presence check: if status empty and queries zero and blocked zero and percentage zero -> still valid? But we require at least status field.
	// Instead just check that body contained expected fields; if status empty treat as error unless JSON had zero values intentionally.
	// For simplicity, if status == "" and QueriesToday == 0 && AdsBlockedToday == 0 && AdsPercentageToday == 0 {
	//   check raw map for existence
	// }
	if resp.Status == "" {
		// Verify raw JSON actually had a status key to distinguish missing field vs empty string
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err == nil {
			if _, ok := raw["status"]; !ok {
				return nil, fmt.Errorf("pihole: missing status field")
			}
		}
	}
	rows := [][2]string{
		{"STATUS", strings.ToUpper(resp.Status)},
		{"QUERIES", fmt.Sprintf("%d", resp.QueriesToday)},
		{"BLOCKED", fmt.Sprintf("%d", resp.AdsBlockedToday)},
		{"BLOCKED %", fmt.Sprintf("%.0f%%", resp.AdsPercentageToday)},
	}
	if len(rows) > 4 {
		rows = rows[:4]
	}
	// Truncate values to 28 chars
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
	}
	return rows, nil
}

func (p *PiHoleDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := "http://pi.hole/admin/api.php?summary"
	if p.URL != "" {
		base = p.URL
	}
	fetchURL := piholeURL(base, p.Token)

	slog.Info("fetching pihole data", "source", "pihole")
	// Pass empty token so apiGet does NOT add X-API-Key; auth is in URL.
	body, err := apiGet(fetchURL, "", nil)
	if err != nil {
		slog.Warn("pihole API call failed, using fallback", "source", "pihole", "error", err)
		return fallbackPihole(width, height), nil
	}
	rows, err := BuildPiholeRows(body)
	if err != nil {
		slog.Warn("pihole parse failed, using fallback", "source", "pihole", "error", err)
		return fallbackPihole(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "PIHOLE"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func fallbackPihole(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "PIHOLE"
	data := map[string]string{"PIHOLE": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}
