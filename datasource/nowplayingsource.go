package datasource

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"ledit/datasource/nowplaying"
	"ledit/render"
)

const spotifyNowPlayingURL = "https://api.spotify.com/v1/me/player/currently-playing"

// NowPlayingSourceDS fetches the currently-playing item from a media provider
// (Spotify/Plex/Jellyfin) during the normal render cycle, with no background
// poller. Idle and unconfigured states render placeholders with a nil error;
// auth, network, and parse failures return an error so health degrades and the
// last-known-good cache can serve the previous frame.
type NowPlayingSourceDS struct {
	Provider     string
	URL          string
	Token        string
	Username     string
	ShowAlbumArt bool
}

func (n *NowPlayingSourceDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	provider := n.provider()
	if !n.configured(provider) {
		return render.RenderNowPlaying(render.NowPlayingParams{
			Track: "Not configured", Width: width, Height: height, Theme: DefaultTheme(),
		})
	}
	np, err := n.fetch(provider)
	if err != nil {
		return nil, err
	}
	params := render.NowPlayingParams{
		Width:        width,
		Height:       height,
		ShowAlbumArt: n.ShowAlbumArt,
		Theme:        DefaultTheme(),
	}
	if np != nil && np.State != "stop" {
		params.Track = np.Track
		params.Artist = np.Artist
		params.Album = np.Album
		params.Position = np.Position
		params.Duration = np.Duration
		params.State = np.State
		params.ArtURL = np.ArtURL
	}
	return render.RenderNowPlaying(params)
}

func (n *NowPlayingSourceDS) provider() string {
	p := strings.ToLower(strings.TrimSpace(n.Provider))
	if p == "" {
		return "jellyfin"
	}
	return p
}

// configured reports whether the fields each provider requires are present.
// Jellyfin/Plex need a server URL; Spotify needs a bearer token.
func (n *NowPlayingSourceDS) configured(provider string) bool {
	switch provider {
	case "spotify":
		return n.Token != ""
	default:
		return strings.TrimSpace(n.URL) != ""
	}
}

func (n *NowPlayingSourceDS) fetch(provider string) (*nowplaying.NowPlaying, error) {
	switch provider {
	case "spotify":
		return n.fetchSpotify()
	case "plex":
		return n.fetchPlex()
	case "jellyfin":
		return n.fetchJellyfin()
	default:
		return nil, fmt.Errorf("unknown now-playing provider %q", provider)
	}
}

func (n *NowPlayingSourceDS) fetchSpotify() (*nowplaying.NowPlaying, error) {
	req, err := http.NewRequest("GET", spotifyNowPlayingURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+n.Token)
	body, err := doGet(req)
	if err != nil {
		return nil, fmt.Errorf("spotify: %w", err)
	}
	return nowplaying.ParseSpotifyNowPlaying(body)
}

func (n *NowPlayingSourceDS) fetchPlex() (*nowplaying.NowPlaying, error) {
	base := strings.TrimRight(n.URL, "/")
	endpoint := base + "/status/sessions"
	if n.Token != "" {
		endpoint += "?" + url.Values{"X-Plex-Token": {n.Token}}.Encode()
	}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	body, err := doGet(req)
	if err != nil {
		return nil, fmt.Errorf("plex: %w", err)
	}
	np, err := nowplaying.ParsePlexSessions(body, n.Username)
	if err != nil {
		return nil, err
	}
	if np.ArtURL != "" {
		np.ArtURL = base + np.ArtURL
		if n.Token != "" {
			np.ArtURL += "?X-Plex-Token=" + url.QueryEscape(n.Token)
		}
	}
	return np, nil
}

func (n *NowPlayingSourceDS) fetchJellyfin() (*nowplaying.NowPlaying, error) {
	base := strings.TrimRight(n.URL, "/")
	req, err := http.NewRequest("GET", base+"/Sessions?activeWithinSeconds=10", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", n.Token)
	body, err := doGet(req)
	if err != nil {
		return nil, fmt.Errorf("jellyfin: %w", err)
	}
	np, err := nowplaying.ParseJellyfinSessions(body, n.Username)
	if err != nil {
		// The shared parser signals "no active session" for idle; that is not a
		// failure, so return a nil item and let the caller show the placeholder.
		if strings.Contains(err.Error(), "no active session") {
			return nil, nil
		}
		return nil, err
	}
	if np.ArtURL != "" {
		np.ArtURL = base + np.ArtURL
		if n.Token != "" {
			np.ArtURL += "?api_key=" + url.QueryEscape(n.Token)
		}
	}
	return np, nil
}

// doGet performs the request with the shared bounded client, mapping
// non-2xx statuses to errors (auth failures included) and reading the body.
func doGet(req *http.Request) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("authentication failed (status %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
