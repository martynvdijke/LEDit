package nowplaying

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NowPlaying is the unified model.
type NowPlaying struct {
	Artist   string
	Track    string
	Album    string
	Position int    // seconds
	Duration int    // seconds
	State    string // play, pause, stop
	TempoBPM *int
	Energy   float64
}

// Poller provides current NowPlaying.
type Poller interface {
	Current() NowPlaying
	Start(ctx context.Context)
	Stop()
}

// --- Jellyfin ---

// JellyfinPoller polls Jellyfin Sessions API.
type JellyfinPoller struct {
	Host     string
	Token    string
	Username string // optional filter
	Client   *http.Client

	mu  sync.RWMutex
	cur NowPlaying
}

func (j *JellyfinPoller) Current() NowPlaying {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.cur
}

func (j *JellyfinPoller) Start(ctx context.Context) {
	go j.loop(ctx)
}
func (j *JellyfinPoller) Stop() {}

func (j *JellyfinPoller) loop(ctx context.Context) {
	j.poll()
	ticker := time.NewTicker(jitterInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.poll()
			ticker.Reset(jitterInterval())
		}
	}
}

func (j *JellyfinPoller) poll() {
	if j.Host == "" {
		j.mu.Lock()
		j.cur = NowPlaying{State: "stop"}
		j.mu.Unlock()
		return
	}
	client := j.Client
	if client == nil {
		client = http.DefaultClient
	}
	url := strings.TrimRight(j.Host, "/") + "/Sessions?activeWithinSeconds=10"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Embx-Token", j.Token)
	req.Header.Set("X-Emby-Token", j.Token)
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("jellyfin nowplaying poll failed", "error", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	np, err := ParseJellyfinSessions(body, j.Username)
	if err != nil {
		// no session -> stop
		j.mu.Lock()
		j.cur = NowPlaying{State: "stop"}
		j.mu.Unlock()
		return
	}
	j.mu.Lock()
	j.cur = *np
	j.mu.Unlock()
}

// ParseJellyfinSessions parses Jellyfin /Sessions JSON.
func ParseJellyfinSessions(body []byte, usernameFilter string) (*NowPlaying, error) {
	var sessions []struct {
		UserName       string `json:"UserName"`
		NowPlayingItem *struct {
			Name         string   `json:"Name"`
			Artists      []string `json:"Artists"`
			Album        string   `json:"Album"`
			RunTimeTicks *int64   `json:"RunTimeTicks"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			PositionTicks int64 `json:"PositionTicks"`
			IsPaused      bool  `json:"IsPaused"`
		} `json:"PlayState"`
	}
	if err := json.Unmarshal(body, &sessions); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var candidate *struct {
		UserName       string `json:"UserName"`
		NowPlayingItem *struct {
			Name         string   `json:"Name"`
			Artists      []string `json:"Artists"`
			Album        string   `json:"Album"`
			RunTimeTicks *int64   `json:"RunTimeTicks"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			PositionTicks int64 `json:"PositionTicks"`
			IsPaused      bool  `json:"IsPaused"`
		} `json:"PlayState"`
	}
	for i := range sessions {
		if sessions[i].NowPlayingItem == nil {
			continue
		}
		if usernameFilter != "" && sessions[i].UserName != usernameFilter {
			continue
		}
		candidate = &sessions[i]
		break
	}
	if candidate == nil && usernameFilter != "" {
		// fallback to any active
		for i := range sessions {
			if sessions[i].NowPlayingItem != nil {
				candidate = &sessions[i]
				break
			}
		}
	}
	if candidate == nil {
		return nil, fmt.Errorf("no active session")
	}
	np := &NowPlaying{}
	if len(candidate.NowPlayingItem.Artists) > 0 {
		np.Artist = candidate.NowPlayingItem.Artists[0]
	}
	np.Track = candidate.NowPlayingItem.Name
	np.Album = candidate.NowPlayingItem.Album
	np.Position = int(candidate.PlayState.PositionTicks / 10000000)
	if candidate.NowPlayingItem.RunTimeTicks != nil {
		np.Duration = int(*candidate.NowPlayingItem.RunTimeTicks / 10000000)
	}
	if candidate.PlayState.IsPaused {
		np.State = "pause"
	} else {
		np.State = "play"
	}
	np.Energy = 0.5
	return np, nil
}

// --- MPD ---

// MPDPoller polls MPD via TCP.
type MPDPoller struct {
	Host     string
	Port     int
	Password string

	mu   sync.RWMutex
	cur  NowPlaying
	conn net.Conn
}

func (m *MPDPoller) Current() NowPlaying {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}
func (m *MPDPoller) Start(ctx context.Context) { go m.loop(ctx) }
func (m *MPDPoller) Stop() {
	if m.conn != nil {
		m.conn.Close()
	}
}

func (m *MPDPoller) loop(ctx context.Context) {
	m.poll()
	ticker := time.NewTicker(jitterInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
			ticker.Reset(jitterInterval())
		}
	}
}

func (m *MPDPoller) poll() {
	if m.Host == "" {
		m.mu.Lock()
		m.cur = NowPlaying{State: "stop"}
		m.mu.Unlock()
		return
	}
	port := m.Port
	if port == 0 {
		port = 6600
	}
	addr := net.JoinHostPort(m.Host, strconv.Itoa(port))
	// reuse persistent conn
	if m.conn == nil {
		c, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			slog.Warn("mpd dial failed", "error", err)
			return
		}
		// read hello
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		br := bufio.NewReader(c)
		line, _ := br.ReadString('\n')
		if !strings.HasPrefix(line, "OK") {
			c.Close()
			return
		}
		if m.Password != "" {
			fmt.Fprintf(c, "password %s\n", m.Password)
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			resp, _ := br.ReadString('\n')
			if !strings.HasPrefix(resp, "OK") {
				c.Close()
				return
			}
		}
		m.conn = c
	}
	c := m.conn
	c.SetDeadline(time.Now().Add(3 * time.Second))
	// send status + currentsong
	fmt.Fprint(c, "status\n")
	fmt.Fprint(c, "currentsong\n")
	br := bufio.NewReader(c)
	statusLines := []string{}
	currLines := []string{}
	// Read status block until OK
	for {
		ln, err := br.ReadString('\n')
		if err != nil {
			m.conn.Close()
			m.conn = nil
			return
		}
		ln = strings.TrimSpace(ln)
		if ln == "OK" {
			break
		}
		if strings.HasPrefix(ln, "ACK") {
			break
		}
		statusLines = append(statusLines, ln)
	}
	for {
		ln, err := br.ReadString('\n')
		if err != nil {
			m.conn.Close()
			m.conn = nil
			return
		}
		ln = strings.TrimSpace(ln)
		if ln == "OK" {
			break
		}
		if strings.HasPrefix(ln, "ACK") {
			break
		}
		currLines = append(currLines, ln)
	}
	np := ParseMPD(statusLines, currLines)
	m.mu.Lock()
	m.cur = *np
	m.mu.Unlock()
}

// ParseMPD parses MPD status + currentsong lines.
func ParseMPD(statusLines, currLines []string) *NowPlaying {
	m := map[string]string{}
	for _, l := range statusLines {
		parts := strings.SplitN(l, ":", 2)
		if len(parts) == 2 {
			m[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	cm := map[string]string{}
	for _, l := range currLines {
		parts := strings.SplitN(l, ":", 2)
		if len(parts) == 2 {
			cm[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	np := &NowPlaying{Energy: 0.5}
	state := m["state"]
	switch state {
	case "play":
		np.State = "play"
	case "pause":
		np.State = "pause"
	default:
		np.State = "stop"
	}
	np.Artist = cm["artist"]
	if np.Artist == "" {
		np.Artist = cm["Artist"]
	}
	np.Track = cm["title"]
	if np.Track == "" {
		np.Track = cm["Title"]
	}
	np.Album = cm["album"]
	if t, ok := m["time"]; ok {
		// format elapsed:duration
		parts := strings.Split(t, ":")
		if len(parts) == 2 {
			if v, err := strconv.Atoi(parts[0]); err == nil {
				np.Position = v
			}
			if v, err := strconv.Atoi(parts[1]); err == nil {
				np.Duration = v
			}
		}
	} else if el, ok := m["elapsed"]; ok {
		if v, err := strconv.ParseFloat(el, 64); err == nil {
			np.Position = int(v)
		}
		if d, ok := m["duration"]; ok {
			if v, err := strconv.ParseFloat(d, 64); err == nil {
				np.Duration = int(v)
			}
		}
	}
	// currentsong time fallback
	if np.Duration == 0 {
		if tv, ok := cm["time"]; ok {
			if v, err := strconv.Atoi(tv); err == nil {
				np.Duration = v
			}
		}
	}
	if np.State != "play" && np.State != "pause" {
		np.State = "stop"
	}
	if np.Track == "" && np.Artist == "" {
		np.State = "stop"
	}
	return np
}

func jitterInterval() time.Duration {
	base := 2500 * time.Millisecond
	jitter := time.Duration(rand.Intn(600)-300) * time.Millisecond
	return base + jitter
}

// Global manager

var (
	globalMu       sync.RWMutex
	globalNP       NowPlaying
	globalProvider string
	mpdPoller      *MPDPoller
	jellyfinPoller *JellyfinPoller
	cancelPoll     context.CancelFunc
)

func SetProvider(provider string, mpdHost string, mpdPort int, mpdPass string, jellyfinHost, jellyfinToken string) {
	globalMu.Lock()
	globalProvider = provider
	globalMu.Unlock()
	// stop old
	if cancelPoll != nil {
		cancelPoll()
		cancelPoll = nil
	}
	if mpdPoller != nil {
		mpdPoller.Stop()
		mpdPoller = nil
	}
	jellyfinPoller = nil
	if provider == "disabled" || provider == "" {
		globalMu.Lock()
		globalNP = NowPlaying{State: "stop"}
		globalMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelPoll = cancel
	if provider == "mpd" {
		mpdPoller = &MPDPoller{Host: mpdHost, Port: mpdPort, Password: mpdPass}
		mpdPoller.Start(ctx)
		go relayLoop(ctx, func() NowPlaying { return mpdPoller.Current() })
	} else if provider == "jellyfin" {
		jellyfinPoller = &JellyfinPoller{Host: jellyfinHost, Token: jellyfinToken}
		jellyfinPoller.Start(ctx)
		go relayLoop(ctx, func() NowPlaying { return jellyfinPoller.Current() })
	}
}

func relayLoop(ctx context.Context, fn func() NowPlaying) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			np := fn()
			globalMu.Lock()
			globalNP = np
			globalMu.Unlock()
		}
	}
}

func CurrentNowPlaying() NowPlaying {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalNP
}

func SetNowPlayingForTest(np NowPlaying) {
	globalMu.Lock()
	globalNP = np
	globalMu.Unlock()
}
