package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

// JellyfinDS fetches Jellyfin active sessions.
//
// Note: Jellyfin authenticates via the `X-Emby-Token` header, not the house
// default `X-API-Key`. Therefore apiGet is called with an empty token and an
// explicit headers map `{ "X-Emby-Token": <token> }` so that no X-API-Key
// header is sent.
type JellyfinDS struct {
	Token string
	URL   string
}

// BuildJellyfinRows parses Jellyfin /Sessions JSON and returns rows for the
// first session with a non-nil NowPlayingItem. Values truncated to 28 chars,
// capped at 4 rows.
//
// Contract: if no session is currently playing, returns a sentinel error. The
// caller (GetPNG) converts that error into the fallback image
// ("JELLYFIN":"Nothing playing"). This is cleaner than returning an empty slice
// because it distinguishes "nothing playing" from a genuine parse failure while
// keeping the same fallback path.
func BuildJellyfinRows(body []byte) ([][2]string, error) {
	var sessions []struct {
		UserName       string `json:"UserName"`
		NowPlayingItem *struct {
			Name         string `json:"Name"`
			RunTimeTicks *int64 `json:"RunTimeTicks"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			PositionTicks int64 `json:"PositionTicks"`
			IsPaused      bool  `json:"IsPaused"`
		} `json:"PlayState"`
	}
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, fmt.Errorf("jellyfin parse error: %w", err)
	}
	// Find first session with NowPlayingItem
	var active *struct {
		UserName       string `json:"UserName"`
		NowPlayingItem *struct {
			Name         string `json:"Name"`
			RunTimeTicks *int64 `json:"RunTimeTicks"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			PositionTicks int64 `json:"PositionTicks"`
			IsPaused      bool  `json:"IsPaused"`
		} `json:"PlayState"`
	}
	for i := range sessions {
		if sessions[i].NowPlayingItem != nil {
			active = &sessions[i]
			break
		}
	}
	if active == nil {
		return nil, fmt.Errorf("nothing playing")
	}
	name := active.NowPlayingItem.Name
	if len(name) > 28 {
		name = name[:28]
	}
	rows := [][2]string{
		{"NOW PLAYING", name},
		{"USER", active.UserName},
	}
	// PAUSED vs progress
	if active.PlayState.IsPaused {
		rows = append(rows, [2]string{"STATUS", "PAUSED"})
	} else if active.NowPlayingItem.RunTimeTicks != nil && *active.NowPlayingItem.RunTimeTicks > 0 {
		pct := active.PlayState.PositionTicks * 100 / *active.NowPlayingItem.RunTimeTicks
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		rows = append(rows, [2]string{"PROGRESS", fmt.Sprintf("%d%%", pct)})
	}
	if len(rows) > 4 {
		rows = rows[:4]
	}
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
		if len(rows[i][0]) > 28 {
			rows[i][0] = rows[i][0][:28]
		}
	}
	return rows, nil
}

func (j *JellyfinDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := strings.TrimRight(j.URL, "/")
	fetchURL := base + "/Sessions"

	slog.Info("fetching jellyfin data", "source", "jellyfin")
	headers := map[string]string{}
	if j.Token != "" {
		headers["X-Emby-Token"] = j.Token
	}
	// Empty token => no X-API-Key; use explicit X-Emby-Token header instead.
	body, err := apiGet(fetchURL, "", headers)
	if err != nil {
		slog.Warn("jellyfin API call failed, using fallback", "source", "jellyfin", "error", err)
		return fallbackJellyfin(width, height), nil
	}
	rows, err := BuildJellyfinRows(body)
	if err != nil {
		slog.Warn("jellyfin no active session or parse failed, using fallback", "source", "jellyfin", "error", err)
		return fallbackJellyfin(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "JELLYFIN"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func fallbackJellyfin(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "JELLYFIN"
	data := map[string]string{"JELLYFIN": "Nothing playing"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}
