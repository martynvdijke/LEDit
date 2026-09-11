package nowplaying

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type spotifyNowPlaying struct {
	IsPlaying  bool `json:"is_playing"`
	ProgressMS int  `json:"progress_ms"`
	Item       *struct {
		Name       string `json:"name"`
		DurationMS int    `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name   string `json:"name"`
			Images []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"images"`
		} `json:"album"`
	} `json:"item"`
}

// ParseSpotifyNowPlaying parses the Spotify /me/player/currently-playing JSON
// body into the shared NowPlaying model. An empty body (Spotify's 204 when
// nothing is playing) or a response without an item yields a stop state and a
// nil error; malformed JSON returns an error.
func ParseSpotifyNowPlaying(body []byte) (*NowPlaying, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return &NowPlaying{State: "stop"}, nil
	}
	var r spotifyNowPlaying
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse spotify: %w", err)
	}
	if r.Item == nil {
		return &NowPlaying{State: "stop"}, nil
	}
	np := &NowPlaying{}
	if r.IsPlaying {
		np.State = "play"
	} else {
		np.State = "pause"
	}
	np.Track = r.Item.Name
	if len(r.Item.Artists) > 0 {
		np.Artist = r.Item.Artists[0].Name
	}
	np.Album = r.Item.Album.Name
	np.Position = r.ProgressMS / 1000
	np.Duration = r.Item.DurationMS / 1000
	np.ArtURL = largestImage(r.Item.Album.Images)
	return np, nil
}

func largestImage(images []struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}) string {
	best := ""
	bestArea := -1
	for _, img := range images {
		if img.URL == "" {
			continue
		}
		area := img.Width * img.Height
		if area > bestArea {
			bestArea = area
			best = img.URL
		}
	}
	return best
}
