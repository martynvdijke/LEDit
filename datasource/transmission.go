package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*TransmissionDS)(nil)

// TransmissionDS fetches Transmission RPC torrent list.
// ponytail: single GET, no session retry if 409
type TransmissionDS struct {
	Token string
	URL   string
}

// BuildTransmissionRows parses Transmission RPC torrent-get response.
// Expected: {"arguments":{"torrents":[{"name":string,"percentDone":float,"status":int}]}}
func BuildTransmissionRows(body []byte) ([][2]string, error) {
	var resp struct {
		Arguments struct {
			Torrents []struct {
				Name        string  `json:"name"`
				PercentDone float64 `json:"percentDone"`
				Status      int     `json:"status"`
			} `json:"torrents"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("transmission parse error: %w", err)
	}
	// Also support flat {"torrents":[...]} for flexibility
	if resp.Arguments.Torrents == nil {
		var flat struct {
			Torrents []struct {
				Name        string  `json:"name"`
				PercentDone float64 `json:"percentDone"`
				Status      int     `json:"status"`
			} `json:"torrents"`
		}
		if err := json.Unmarshal(body, &flat); err == nil && flat.Torrents != nil {
			resp.Arguments.Torrents = flat.Torrents
		}
	}
	if resp.Arguments.Torrents == nil {
		return nil, fmt.Errorf("transmission: missing torrents field")
	}
	downloading := 0
	for _, t := range resp.Arguments.Torrents {
		if t.Status == 4 {
			downloading++
		}
	}
	rows := [][2]string{
		{"TORRENTS", fmt.Sprintf("%d", len(resp.Arguments.Torrents))},
		{"DOWNLOADING", fmt.Sprintf("%d", downloading)},
	}
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
	}
	return rows, nil
}

func fallbackTransmission(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "TRANS"
	data := map[string]string{"TRANS": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (t *TransmissionDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := strings.TrimSpace(t.URL)
	if base == "" {
		base = "http://localhost:9091/transmission/rpc"
	}
	slog.Info("fetching transmission data", "source", "transmission")
	// ponytail: single GET, no session retry if 409
	body, err := apiGet(base, t.Token, nil)
	if err != nil {
		slog.Warn("transmission API call failed, using fallback", "source", "transmission", "error", err)
		return fallbackTransmission(width, height), nil
	}
	rows, err := BuildTransmissionRows(body)
	if err != nil {
		slog.Warn("transmission parse failed, using fallback", "source", "transmission", "error", err)
		return fallbackTransmission(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "TRANS"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (t *TransmissionDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := strings.TrimSpace(t.URL)
	if base == "" {
		base = "http://localhost:9091/transmission/rpc"
	}
	body, err := apiGet(base, t.Token, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Arguments struct {
			Torrents []struct {
				Status int `json:"status"`
			} `json:"torrents"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Arguments.Torrents == nil {
		var flat struct {
			Torrents []struct {
				Status int `json:"status"`
			} `json:"torrents"`
		}
		if err := json.Unmarshal(body, &flat); err == nil && flat.Torrents != nil {
			resp.Arguments.Torrents = flat.Torrents
		}
	}
	downloading := 0
	for _, tor := range resp.Arguments.Torrents {
		if tor.Status == 4 {
			downloading++
		}
	}
	return map[string]any{"torrents": len(resp.Arguments.Torrents), "downloading": downloading}, nil
}
