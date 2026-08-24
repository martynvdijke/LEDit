package datasource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"ledit/render"
)

// UptimeDS probes configured HTTP targets directly (no external API).
type UptimeDS struct {
	URL    string
	Config string
}

// UptimeTarget is a single monitored endpoint.
type UptimeTarget struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// ParseUptimeTargets is tolerant: invalid JSON → nil; non-array → nil;
// entries missing name or url skipped; timeout_seconds clamped 1..30 default 2.
func ParseUptimeTargets(config string) []UptimeTarget {
	if strings.TrimSpace(config) == "" {
		return nil
	}
	trim := strings.TrimSpace(config)
	if !strings.HasPrefix(trim, "[") {
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(config), &raw); err != nil {
		return nil
	}
	var out []UptimeTarget
	for _, m := range raw {
		var entry struct {
			Name           string `json:"name"`
			URL            string `json:"url"`
			TimeoutSeconds *int   `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(m, &entry); err != nil {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		u := strings.TrimSpace(entry.URL)
		if name == "" || u == "" {
			continue
		}
		timeout := 2
		if entry.TimeoutSeconds != nil {
			timeout = *entry.TimeoutSeconds
			if timeout < 1 {
				timeout = 1
			}
			if timeout > 30 {
				timeout = 30
			}
		}
		out = append(out, UptimeTarget{Name: name, URL: u, TimeoutSeconds: timeout})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildUptimeRows builds up to 4 rows from probe results. Injectable probe seam for tests.
func BuildUptimeRows(targets []UptimeTarget, probe func(UptimeTarget) (bool, int)) [][2]string {
	var rows [][2]string
	for _, t := range targets {
		if len(rows) >= 4 {
			break
		}
		name := t.Name
		if len(name) > 28 {
			name = name[:28]
		}
		up, ms := probe(t)
		status := "DOWN"
		if up {
			status = fmt.Sprintf("UP %dms", ms)
		}
		rows = append(rows, [2]string{name, status})
	}
	if rows == nil {
		rows = [][2]string{}
	}
	return rows
}

// probeUptimeTarget is the real probe: HEAD first; on 405 or method-unsupported transport error fall back to GET.
var probeUptimeTarget = func(target UptimeTarget) (bool, int) {
	timeout := time.Duration(target.TimeoutSeconds) * time.Second
	client := &http.Client{Timeout: timeout}
	doReq := func(method string) (bool, int, bool) {
		start := time.Now()
		req, err := http.NewRequest(method, target.URL, nil)
		if err != nil {
			return false, 0, false
		}
		resp, err := client.Do(req)
		elapsed := int(time.Since(start).Milliseconds())
		if err != nil {
			// transport error indicating method unsupported -> signal fallback
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "method") || strings.Contains(msg, "unsupported") {
				return false, elapsed, true
			}
			return false, elapsed, false
		}
		defer resp.Body.Close()
		// any HTTP response counts as UP, even 4xx/5xx
		if resp.StatusCode == http.StatusMethodNotAllowed {
			return false, elapsed, true
		}
		return true, elapsed, false
	}
	up, ms, needFallback := doReq(http.MethodHead)
	if needFallback {
		up2, ms2, _ := doReq(http.MethodGet)
		return up2, ms2
	}
	return up, ms
}

// Uptime cache: keyed by sha256 hex of config JSON, 30s TTL.
var (
	uptimeCacheTTL = 30 * time.Second
	uptimeCache    = struct {
		sync.Mutex
		m map[string]uptimeCacheEntry
	}{m: map[string]uptimeCacheEntry{}}
)

type uptimeCacheEntry struct {
	rows [][2]string
	at   time.Time
}

func uptimeCacheKey(config string) string {
	h := sha256.Sum256([]byte(config))
	return fmt.Sprintf("%x", h)
}

// clearUptimeCache resets the cache; exported for tests.
func clearUptimeCache() {
	uptimeCache.Lock()
	uptimeCache.m = map[string]uptimeCacheEntry{}
	uptimeCache.Unlock()
}

// resetUptimeProbeForTest restores default probe; exported for tests if needed.
func resetUptimeProbeForTest() {
	// no-op reassignment hook; probeUptimeTarget is default
}

// fallbackUptime renders unavailable.
func fallbackUptime(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "UPTIME"
	data := map[string]string{"UPTIME": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (u *UptimeDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	targets := ParseUptimeTargets(u.Config)
	if len(targets) == 0 {
		slog.Warn("uptime no targets, using fallback", "source", "uptime")
		return fallbackUptime(width, height), nil
	}
	if len(targets) > 4 {
		targets = targets[:4]
	}
	key := uptimeCacheKey(u.Config)
	uptimeCache.Lock()
	if e, ok := uptimeCache.m[key]; ok && time.Since(e.at) < uptimeCacheTTL {
		rows := e.rows
		uptimeCache.Unlock()
		// check if all down -> fallback
		allDown := len(rows) > 0
		for _, r := range rows {
			if strings.HasPrefix(r[1], "UP") {
				allDown = false
				break
			}
		}
		if allDown {
			return fallbackUptime(width, height), nil
		}
		// if cache hit but rows empty (shouldn't happen) fallback
		if len(rows) == 0 {
			return fallbackUptime(width, height), nil
		}
		theme := DefaultTheme()
		theme.Title = "UPTIME"
		data := map[string]string{}
		for _, r := range rows {
			data[r[0]] = r[1]
		}
		img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
		if err != nil {
			return nil, err
		}
		return img, nil
	}
	uptimeCache.Unlock()

	rows := BuildUptimeRows(targets, probeUptimeTarget)

	// Cache the rows
	uptimeCache.Lock()
	uptimeCache.m[key] = uptimeCacheEntry{rows: rows, at: time.Now()}
	uptimeCache.Unlock()

	// zero targets already handled; check all down
	allDown := true
	for _, r := range rows {
		if strings.HasPrefix(r[1], "UP") {
			allDown = false
			break
		}
	}
	if allDown {
		slog.Warn("uptime all targets down, using fallback", "source", "uptime")
		return fallbackUptime(width, height), nil
	}
	theme := DefaultTheme()
	theme.Title = "UPTIME"
	data := map[string]string{}
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}
