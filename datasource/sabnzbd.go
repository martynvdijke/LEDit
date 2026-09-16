package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*SabnzbdDS)(nil)

// SabnzbdDS fetches SABnzbd queue status.
//
// DEVIATION from house convention: SABnzbd authenticates via query parameter
// `apikey` rather than the standard X-API-Key header. The apikey is appended
// to the URL when not already present and apiGet is called with an empty token.
type SabnzbdDS struct {
	Token string
	URL   string
}

func sabnzbdURL(base, token string) string {
	if strings.Contains(base, "apikey=") {
		return base
	}
	if token == "" {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		if strings.Contains(base, "?") {
			return base + "&apikey=" + url.QueryEscape(token)
		}
		return base + "?apikey=" + url.QueryEscape(token)
	}
	q := u.Query()
	q.Set("apikey", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func sabnzbdTruncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func sabnzbdToFloat64(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		var f float64
		fmt.Sscanf(strings.TrimSpace(x), "%f", &f)
		return f
	default:
		return 0
	}
}

func sabnzbdToInt(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	case string:
		var f float64
		fmt.Sscanf(strings.TrimSpace(x), "%f", &f)
		return int(f)
	default:
		return 0
	}
}

type sabnzbdResponse struct {
	Queue *sabnzbdQueue `json:"queue"`
}

type sabnzbdQueue struct {
	Status    string        `json:"status"`
	KbPerSec  interface{}   `json:"kbpersec"`
	MbLeft    interface{}   `json:"mbleft"`
	NoOfSlots interface{}   `json:"noofslots"`
	TimeLeft  string        `json:"timeleft"`
	Slots     []sabnzbdSlot `json:"slots"`
}

type sabnzbdSlot struct {
	Filename   string      `json:"filename"`
	Percentage interface{} `json:"percentage"`
	Mb         interface{} `json:"mb"`
	TimeLeft   string      `json:"timeleft"`
}

func fallbackSabnzbd(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "SABNZBD"
	data := map[string]string{"SABNZBD": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func sabnzbdFetch(q *SabnzbdDS) (*sabnzbdQueue, error) {
	if q.URL == "" || q.Token == "" {
		return nil, fmt.Errorf("unconfigured")
	}
	// Build base URL: ensure it has mode=queue&output=json
	base := q.URL
	// Append mode/output if not present? Spec says GET <URL>/api?mode=queue&output=json&apikey=<token>
	// But we assume caller provides full URL; if not containing mode, we still just append apikey via sabnzbdURL.
	// To match spec, if URL doesn't contain "mode=", append required query.
	if !strings.Contains(base, "mode=") {
		if strings.Contains(base, "?") {
			base += "&mode=queue&output=json"
		} else {
			base += "?mode=queue&output=json"
		}
	} else if !strings.Contains(base, "output=") {
		base += "&output=json"
	}
	effective := sabnzbdURL(base, q.Token)
	body, err := apiGet(effective, "", nil)
	if err != nil {
		return nil, err
	}
	var resp sabnzbdResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Queue == nil {
		return nil, fmt.Errorf("missing queue")
	}
	return resp.Queue, nil
}

func sabnzbdBuildData(queue *sabnzbdQueue) map[string]string {
	data := map[string]string{
		"STATUS": strings.ToUpper(queue.Status),
		"SPEED":  fmt.Sprintf("%v KB/s", queue.KbPerSec),
		"LEFT":   fmt.Sprintf("%v MB", queue.MbLeft),
		"ETA":    queue.TimeLeft,
	}
	// Normalize SPEED/LEFT if they are numeric strings? Keep as raw.
	// Ensure consistent formatting: if interface is string, it already includes value; if number, format.
	// Use helper to ensure string.
	_ = sabnzbdToFloat64 // keep helpers used elsewhere
	n := len(queue.Slots)
	if n > 4 {
		n = 4
	}
	for i := 0; i < n; i++ {
		s := queue.Slots[i]
		name := sabnzbdTruncate(s.Filename, 22)
		if name == "" {
			name = fmt.Sprintf("SLOT %d", i+1)
		}
		pct := sabnzbdToInt(s.Percentage)
		// Alternative if percentage is string like "42.5", int truncates; use float.
		if pct == 0 {
			f := sabnzbdToFloat64(s.Percentage)
			pct = int(f + 0.5)
		}
		data[name] = fmt.Sprintf("%d%%", pct)
	}
	return data
}

func (s *SabnzbdDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	queue, err := sabnzbdFetch(s)
	if err != nil {
		slog.Warn("sabnzbd fetch failed, using fallback", "source", "sabnzbd", "error", err)
		return fallbackSabnzbd(width, height), nil
	}
	// Build display values with proper formatting.
	// SPEED expects "<kbpersec> KB/s"
	kbStr := fmt.Sprintf("%v", queue.KbPerSec)
	leftStr := fmt.Sprintf("%v", queue.MbLeft)
	// If they are json.Number style, fmt prints correctly.
	data := map[string]string{
		"STATUS": strings.ToUpper(queue.Status),
		"SPEED":  kbStr + " KB/s",
		"LEFT":   leftStr + " MB",
		"ETA":    queue.TimeLeft,
	}
	n := len(queue.Slots)
	if n > 4 {
		n = 4
	}
	for i := 0; i < n; i++ {
		sl := queue.Slots[i]
		name := sabnzbdTruncate(sl.Filename, 22)
		if name == "" {
			name = fmt.Sprintf("SLOT %d", i+1)
		}
		pct := sabnzbdToInt(sl.Percentage)
		if pct == 0 {
			f := sabnzbdToFloat64(sl.Percentage)
			if f != 0 {
				pct = int(f + 0.5)
			}
		}
		data[name] = fmt.Sprintf("%d%%", pct)
	}
	theme := DefaultTheme()
	theme.Title = "SABNZBD"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (s *SabnzbdDS) CurrentState(_ context.Context) (map[string]any, error) {
	queue, err := sabnzbdFetch(s)
	if err != nil {
		return map[string]any{"status": "", "kbpersec": float64(0), "mbleft": float64(0), "noofslots": 0, "timeleft": ""}, nil
	}
	return map[string]any{
		"status":    queue.Status,
		"kbpersec":  sabnzbdToFloat64(queue.KbPerSec),
		"mbleft":    sabnzbdToFloat64(queue.MbLeft),
		"noofslots": sabnzbdToInt(queue.NoOfSlots),
		"timeleft":  queue.TimeLeft,
	}, nil
}
