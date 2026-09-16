package datasource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*ImmichDS)(nil)
var _ Ambienter = (*ImmichDS)(nil)

// ImmichDS fetches images from an Immich server.
//
// Endpoints used:
//   - memories mode: GET <URL>/api/memories  -> expects JSON array of {assets:[{id,type}]} or {memories:[...]} wrapper
//   - album mode:    GET <URL>/api/albums/<album> -> expects JSON object {assets:[{id,type}]} or array
//   - random mode:   GET <URL>/api/assets/random -> expects JSON array of assets or single asset object
//
// Thumbnail:         GET <URL>/api/assets/<id>/thumbnail?size=preview
// All listing/thumbnail requests send header x-api-key: <Token>.
type ImmichDS struct {
	URL    string
	Token  string
	Config string
}

// ImmichConfig holds tolerant parsed configuration for ImmichDS.
type ImmichConfig struct {
	Mode            string `json:"mode"`
	Album           string `json:"album"`
	IntervalSeconds int    `json:"interval_seconds"`
	Slideshow       bool   `json:"slideshow"`
}

// ParseImmichConfig parses config JSON tolerantly. Invalid/empty JSON -> safe defaults.
func ParseImmichConfig(config string) ImmichConfig {
	def := ImmichConfig{
		Mode:            "memories",
		IntervalSeconds: 300,
		Slideshow:       true,
	}
	if strings.TrimSpace(config) == "" {
		return def
	}
	// auxiliary with pointers to detect presence
	var raw struct {
		Mode            *string `json:"mode"`
		Album           *string `json:"album"`
		IntervalSeconds *int    `json:"interval_seconds"`
		Slideshow       *bool   `json:"slideshow"`
	}
	if err := json.Unmarshal([]byte(config), &raw); err != nil {
		return def
	}
	cfg := def
	hasSlideshow := false
	if raw.Mode != nil {
		m := strings.ToLower(strings.TrimSpace(*raw.Mode))
		switch m {
		case "memories", "random", "album":
			cfg.Mode = m
		case "":
			// keep default
		default:
			cfg.Mode = "memories"
		}
	}
	if raw.Album != nil {
		cfg.Album = strings.TrimSpace(*raw.Album)
	}
	if raw.IntervalSeconds != nil {
		v := *raw.IntervalSeconds
		if v == 0 {
			cfg.IntervalSeconds = 0
		} else {
			if v < 30 {
				v = 30
			}
			cfg.IntervalSeconds = v
		}
	}
	if raw.Slideshow != nil {
		hasSlideshow = true
		cfg.Slideshow = *raw.Slideshow
	}
	if !hasSlideshow {
		// Slideshow true when IntervalSeconds > 0 and mode is set; default true for memories/random
		if cfg.IntervalSeconds > 0 {
			cfg.Slideshow = true
		} else {
			cfg.Slideshow = false
		}
	}
	// clamp interval if disabled? keep 0 as disabled
	if cfg.IntervalSeconds != 0 && cfg.IntervalSeconds < 30 {
		cfg.IntervalSeconds = 30
	}
	return cfg
}

type immichAsset struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func immichIsImage(a immichAsset) bool {
	t := strings.ToUpper(strings.TrimSpace(a.Type))
	if t == "" {
		return true
	}
	if strings.Contains(t, "VIDEO") {
		return false
	}
	return t == "IMAGE"
}

func immichTrimURL(u string) string {
	return strings.TrimRight(strings.TrimSpace(u), "/")
}

func fallbackImmich(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "IMMICH"
	data := map[string]string{"IMMICH": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func immichFetchAssetID(cfg ImmichConfig, baseURL, token string) (string, int, int, error) {
	// returns assetID, photos count, memories count
	switch cfg.Mode {
	case "album":
		return immichFetchAlbum(baseURL, token, cfg.Album)
	case "random":
		return immichFetchRandom(baseURL, token)
	default: // memories
		return immichFetchMemories(baseURL, token)
	}
}

func immichFetchMemories(baseURL, token string) (string, int, int, error) {
	url := immichTrimURL(baseURL) + "/api/memories"
	body, err := apiGet(url, "", map[string]string{"x-api-key": token})
	if err != nil {
		return "", 0, 0, err
	}
	// Try array of memories
	var memories []struct {
		Assets []immichAsset `json:"assets"`
	}
	if err := json.Unmarshal(body, &memories); err == nil && len(memories) > 0 {
		// count image assets and memories
		memCount := len(memories)
		photos := 0
		for _, m := range memories {
			for _, a := range m.Assets {
				if immichIsImage(a) {
					photos++
				}
			}
		}
		for _, m := range memories {
			for _, a := range m.Assets {
				if immichIsImage(a) {
					return a.ID, photos, memCount, nil
				}
			}
		}
		return "", photos, memCount, nil
	}
	// Try wrapper object {memories: [...]}
	var wrapper struct {
		Memories []struct {
			Assets []immichAsset `json:"assets"`
		} `json:"memories"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Memories) > 0 {
		memCount := len(wrapper.Memories)
		photos := 0
		for _, m := range wrapper.Memories {
			for _, a := range m.Assets {
				if immichIsImage(a) {
					photos++
				}
			}
		}
		for _, m := range wrapper.Memories {
			for _, a := range m.Assets {
				if immichIsImage(a) {
					return a.ID, photos, memCount, nil
				}
			}
		}
		return "", photos, memCount, nil
	}
	// Try direct assets array? treat as no memories
	return "", 0, 0, nil
}

func immichFetchAlbum(baseURL, token, album string) (string, int, int, error) {
	if strings.TrimSpace(album) == "" {
		return "", 0, 0, nil
	}
	url := immichTrimURL(baseURL) + "/api/albums/" + album
	body, err := apiGet(url, "", map[string]string{"x-api-key": token})
	if err != nil {
		return "", 0, 0, err
	}
	// Try object with assets field
	var obj struct {
		Assets []immichAsset `json:"assets"`
	}
	if err := json.Unmarshal(body, &obj); err == nil && obj.Assets != nil {
		photos := 0
		for _, a := range obj.Assets {
			if immichIsImage(a) {
				photos++
			}
		}
		for _, a := range obj.Assets {
			if immichIsImage(a) {
				return a.ID, photos, 0, nil
			}
		}
		return "", photos, 0, nil
	}
	// Try direct array
	var arr []immichAsset
	if err := json.Unmarshal(body, &arr); err == nil {
		photos := 0
		for _, a := range arr {
			if immichIsImage(a) {
				photos++
			}
		}
		for _, a := range arr {
			if immichIsImage(a) {
				return a.ID, photos, 0, nil
			}
		}
		return "", photos, 0, nil
	}
	return "", 0, 0, nil
}

func immichFetchRandom(baseURL, token string) (string, int, int, error) {
	url := immichTrimURL(baseURL) + "/api/assets/random"
	body, err := apiGet(url, "", map[string]string{"x-api-key": token})
	if err != nil {
		return "", 0, 0, err
	}
	// Try array
	var arr []immichAsset
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		photos := 0
		for _, a := range arr {
			if immichIsImage(a) {
				photos++
			}
		}
		for _, a := range arr {
			if immichIsImage(a) {
				return a.ID, photos, 0, nil
			}
		}
		return "", photos, 0, nil
	}
	// Try single object
	var single immichAsset
	if err := json.Unmarshal(body, &single); err == nil && single.ID != "" {
		if immichIsImage(single) {
			return single.ID, 1, 0, nil
		}
		return "", 0, 0, nil
	}
	return "", 0, 0, nil
}

func (i *ImmichDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if strings.TrimSpace(i.URL) == "" || strings.TrimSpace(i.Token) == "" {
		slog.Warn("immich not configured, using fallback", "source", "immich")
		return fallbackImmich(width, height), nil
	}
	cfg := ParseImmichConfig(i.Config)
	assetID, _, _, err := immichFetchAssetID(cfg, i.URL, i.Token)
	if err != nil {
		slog.Warn("immich fetch failed, using fallback", "source", "immich", "error", err)
		return fallbackImmich(width, height), nil
	}
	if assetID == "" {
		slog.Warn("immich no image asset, using fallback", "source", "immich")
		return fallbackImmich(width, height), nil
	}
	thumbURL := immichTrimURL(i.URL) + "/api/assets/" + assetID + "/thumbnail?size=preview"
	thumb, err := apiGet(thumbURL, "", map[string]string{"x-api-key": i.Token})
	if err != nil {
		slog.Warn("immich thumbnail fetch failed, using fallback", "source", "immich", "error", err)
		return fallbackImmich(width, height), nil
	}
	if len(thumb) == 0 {
		slog.Warn("immich empty thumbnail, using fallback", "source", "immich")
		return fallbackImmich(width, height), nil
	}
	enc := base64.StdEncoding.EncodeToString(thumb)
	return &render.RenderedImage{Format: "JPEG", Data: []byte(enc)}, nil
}

func (i *ImmichDS) CurrentState(ctx context.Context) (map[string]any, error) {
	if strings.TrimSpace(i.URL) == "" || strings.TrimSpace(i.Token) == "" {
		return map[string]any{"photos": 0, "memories": 0, "last_asset_id": ""}, nil
	}
	cfg := ParseImmichConfig(i.Config)
	assetID, photos, memories, err := immichFetchAssetID(cfg, i.URL, i.Token)
	if err != nil {
		return map[string]any{"photos": 0, "memories": 0, "last_asset_id": ""}, nil
	}
	return map[string]any{"photos": photos, "memories": memories, "last_asset_id": assetID}, nil
}

func (i *ImmichDS) Ambient() bool {
	cfg := ParseImmichConfig(i.Config)
	return cfg.Slideshow
}
