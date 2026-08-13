package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"ledit/render"
)

// GenericAPIConfig is the JSON configuration for a GenericAPIDS.
type GenericAPIConfig struct {
	Title   string            `json:"title"`
	Headers map[string]string `json:"headers"`
	Rows    []GenericAPIRow   `json:"rows"`
}

// GenericAPIRow maps a label to a dot-path in the API response, e.g.
// {Label: "BTC", Path: "bitcoin.usd"}.
type GenericAPIRow struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// GenericAPIRowValue is a resolved label/value pair, used by the admin
// test-preview action and by rendering.
type GenericAPIRowValue struct {
	Label string
	Value string
}

// GenericAPIDS fetches any public JSON API and renders configured fields.
type GenericAPIDS struct {
	Token  string
	URL    string
	Config string // JSON GenericAPIConfig
}

// ParseGenericAPIConfig parses the config JSON, tolerating malformed input
// (falls back to an empty config).
func ParseGenericAPIConfig(raw string) GenericAPIConfig {
	var cfg GenericAPIConfig
	if raw == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("invalid generic api config, using defaults", "source", "genericapi", "error", err)
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	return cfg
}

// GenericAPITitle returns the configured title, or "" when unset.
func GenericAPITitle(raw string) string {
	return ParseGenericAPIConfig(raw).Title
}

func (g *GenericAPIDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	cfg := ParseGenericAPIConfig(g.Config)

	body, err := apiGet(g.URL, g.Token, cfg.Headers)
	if err != nil {
		slog.Warn("generic api fetch failed, using fallback", "source", "genericapi", "error", err)
		return fallbackGenericAPI(cfg.Title, width, height), nil
	}

	title, rows, err := extractRows(body, cfg)
	if err != nil {
		slog.Warn("generic api response could not be parsed, using fallback", "source", "genericapi", "error", err)
		return fallbackGenericAPI(cfg.Title, width, height), nil
	}

	data := map[string]string{}
	if title != "" {
		data["source"] = title
	}
	for i, row := range rows {
		if i >= 4 {
			break
		}
		key := row.Label
		if key == "" {
			key = fmt.Sprintf("%d", i+1)
		}
		val := row.Value
		if len(val) > 28 {
			val = val[:28] + "..."
		}
		data[key] = val
	}
	if len(data) == 0 {
		data["status"] = "no rows configured"
	}

	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
}

// Extract fetches the configured URL and resolves the row mappings. Used by
// the admin test-preview action to show extracted rows or an error.
func (g *GenericAPIDS) Extract() (string, []GenericAPIRowValue, error) {
	cfg := ParseGenericAPIConfig(g.Config)
	body, err := apiGet(g.URL, g.Token, cfg.Headers)
	if err != nil {
		return cfg.Title, nil, err
	}
	title, rows, err := extractRows(body, cfg)
	if err != nil {
		return cfg.Title, nil, err
	}
	return title, rows, nil
}

func extractRows(body []byte, cfg GenericAPIConfig) (string, []GenericAPIRowValue, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return cfg.Title, nil, fmt.Errorf("response is not valid JSON: %w", err)
	}
	var rows []GenericAPIRowValue
	for _, r := range cfg.Rows {
		val, ok := extractDotPath(root, r.Path)
		if !ok {
			val = "n/a"
		}
		rows = append(rows, GenericAPIRowValue{Label: r.Label, Value: val})
	}
	return cfg.Title, rows, nil
}

// extractDotPath walks a decoded JSON value following dot-paths like
// "data.btc.usd" or "items.0.name". Returns (value, true) when resolved.
func extractDotPath(root any, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	cur := root
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return "", false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	case nil:
		return "", false
	default:
		// Nested object or array at the leaf: render as compact JSON.
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

func fallbackGenericAPI(title string, width, height int) *render.RenderedImage {
	data := map[string]string{
		"source": "API",
		"status": "unavailable",
	}
	if title != "" {
		data["source"] = title
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
