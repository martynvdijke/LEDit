package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"ledit/render"
)

var _ StateProvider = (*QBittorrentDS)(nil)

// QBittorrentDS fetches qBittorrent transfer and torrent info.
//
// DEVIATION from house convention: qBittorrent authenticates via POST
// /api/v2/auth/login with form fields username/password and a SID cookie,
// not the standard X-API-Key header. A local http.Client with a cookiejar is
// used for login and subsequent GETs.
type QBittorrentDS struct {
	Token string
	URL   string
}

func qBittorrentHumanSpeed(v int64) string {
	if v < 1024 {
		return fmt.Sprintf("%d B/s", v)
	}
	f := float64(v)
	units := []string{"KB/s", "MB/s", "GB/s", "TB/s"}
	for _, u := range units {
		f /= 1024
		if f < 1024 || u == "TB/s" {
			if f < 10 {
				return fmt.Sprintf("%.1f %s", f, u)
			}
			return fmt.Sprintf("%.0f %s", f, u)
		}
	}
	return fmt.Sprintf("%.0f TB/s", f)
}

func qBittorrentTruncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func qBittorrentIsActive(t qBittorrentTorrent) bool {
	if t.DlSpeed > 0 {
		return true
	}
	s := strings.ToLower(t.State)
	switch s {
	case "pauseddl", "pausedup", "paused", "stopped", "queueddl", "queuedup", "queued":
		return false
	}
	return true
}

type qBittorrentTransferInfo struct {
	DlInfoSpeed      int64  `json:"dl_info_speed"`
	UpInfoSpeed      int64  `json:"up_info_speed"`
	DlInfoData       int64  `json:"dl_info_data"`
	UpInfoData       int64  `json:"up_info_data"`
	ConnectionStatus string `json:"connection_status"`
}

type qBittorrentTorrent struct {
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	State    string  `json:"state"`
	DlSpeed  int64   `json:"dlspeed"`
}

func fallbackQBittorrent(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "QBITTORRENT"
	data := map[string]string{"QBITTORRENT": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func qBittorrentLoginAndFetch(q *QBittorrentDS) (*qBittorrentTransferInfo, []qBittorrentTorrent, error) {
	if q.URL == "" || q.Token == "" {
		return nil, nil, fmt.Errorf("unconfigured")
	}
	if !strings.Contains(q.Token, ":") {
		return nil, nil, fmt.Errorf("malformed token")
	}
	parts := strings.SplitN(q.Token, ":", 2)
	username := parts[0]
	password := ""
	if len(parts) == 2 {
		password = parts[1]
	}
	base := strings.TrimRight(q.URL, "/")

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}

	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	loginURL := base + "/api/v2/auth/login"
	req, err := http.NewRequest("POST", loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("login status %d body %s", resp.StatusCode, string(body))
	}
	// Check SID cookie.
	hasSID := false
	u, _ := url.Parse(loginURL)
	for _, c := range jar.Cookies(u) {
		if c.Name == "SID" && c.Value != "" {
			hasSID = true
			break
		}
	}
	if !hasSID {
		// Also check response cookies directly.
		for _, c := range resp.Cookies() {
			if c.Name == "SID" && c.Value != "" {
				hasSID = true
				break
			}
		}
	}
	if !hasSID {
		return nil, nil, fmt.Errorf("missing SID cookie body %s", string(body))
	}

	// GET transfer info
	transferURL := base + "/api/v2/transfer/info"
	req2, err := http.NewRequest("GET", transferURL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, nil, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("transfer status %d", resp2.StatusCode)
	}
	tBody, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil, nil, err
	}
	var tInfo qBittorrentTransferInfo
	if err := json.Unmarshal(tBody, &tInfo); err != nil {
		return nil, nil, err
	}

	// GET torrents info
	torrentsURL := base + "/api/v2/torrents/info"
	req3, err := http.NewRequest("GET", torrentsURL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp3, err := client.Do(req3)
	if err != nil {
		return nil, nil, err
	}
	defer resp3.Body.Close()
	if resp3.StatusCode < 200 || resp3.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("torrents status %d", resp3.StatusCode)
	}
	toBody, err := io.ReadAll(resp3.Body)
	if err != nil {
		return nil, nil, err
	}
	var torrents []qBittorrentTorrent
	if err := json.Unmarshal(toBody, &torrents); err != nil {
		return nil, nil, err
	}
	return &tInfo, torrents, nil
}

func qBittorrentBuildData(tInfo *qBittorrentTransferInfo, torrents []qBittorrentTorrent) map[string]string {
	data := map[string]string{
		"DOWN": qBittorrentHumanSpeed(tInfo.DlInfoSpeed),
		"UP":   qBittorrentHumanSpeed(tInfo.UpInfoSpeed),
	}
	active := 0
	for _, t := range torrents {
		if qBittorrentIsActive(t) {
			active++
		}
	}
	data["ACTIVE"] = fmt.Sprintf("%d", active)
	data["TOTAL"] = fmt.Sprintf("%d", len(torrents))
	n := len(torrents)
	if n > 4 {
		n = 4
	}
	for i := 0; i < n; i++ {
		t := torrents[i]
		name := qBittorrentTruncate(t.Name, 22)
		if name == "" {
			name = fmt.Sprintf("TORRENT %d", i+1)
		}
		pct := int(t.Progress*100 + 0.5)
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		data[name] = fmt.Sprintf("%d%%", pct)
	}
	return data
}

func (q *QBittorrentDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	tInfo, torrents, err := qBittorrentLoginAndFetch(q)
	if err != nil {
		slog.Warn("qbittorrent fetch failed, using fallback", "source", "qbittorrent", "error", err)
		return fallbackQBittorrent(width, height), nil
	}
	data := qBittorrentBuildData(tInfo, torrents)
	theme := DefaultTheme()
	theme.Title = "QBITTORRENT"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (q *QBittorrentDS) CurrentState(_ context.Context) (map[string]any, error) {
	tInfo, torrents, err := qBittorrentLoginAndFetch(q)
	if err != nil {
		return map[string]any{"dl_speed": 0, "up_speed": 0, "active": 0, "total": 0, "progress": float64(0)}, nil
	}
	active := 0
	for _, t := range torrents {
		if qBittorrentIsActive(t) {
			active++
		}
	}
	progress := float64(0)
	if len(torrents) > 0 {
		// most-active torrent: highest dlspeed, or first active
		best := torrents[0]
		for _, t := range torrents {
			if t.DlSpeed > best.DlSpeed {
				best = t
			}
		}
		progress = best.Progress
	}
	return map[string]any{
		"dl_speed": int(tInfo.DlInfoSpeed),
		"up_speed": int(tInfo.UpInfoSpeed),
		"active":   active,
		"total":    len(torrents),
		"progress": progress,
	}, nil
}
