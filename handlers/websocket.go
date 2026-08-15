package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
	"ledit/render"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow same-origin requests (browser preview / admin panel):
		// the Origin host:port must match the request's Host header.
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, r.Host) {
			return true
		}
		// Allow configured device origins
		return allowedWSOrigin(origin)
	},
}

func allowedWSOrigin(origin string) bool {
	// Local origins are always allowed
	if origin == "http://localhost" || origin == "https://localhost" {
		return true
	}
	// Allow loopback IPs
	if origin == "http://127.0.0.1" || origin == "http://127.0.0.1:80" {
		return true
	}
	// Allow origins from device settings will be checked at runtime
	// (device IPs are loaded dynamically)
	return false
}

type sourceWithName struct {
	Name     string
	Source   datasource.Datasource
	cacheKey string // "<type>:<id>" — resolution is appended at render time
}

// feedConn carries per-connection display metadata for the feed loop. The
// cacheKeyPrefix namespaces last-known-good cache entries so that a device
// preview does not share cache state with other connections; deviceID is
// carried for logging. The zero value keeps /ws/feed and /ws/device/:token
// behavior byte-identical.
type feedConn struct {
	cacheKeyPrefix string // e.g. "device:<id>:" — "" keeps legacy cache keys
	deviceID       int
	frames         func() // optional per-frame hook (device frame counter); nil for browser/preview
}

type WSHub struct {
	Client *ent.Client
}

func NewWSHub(client *ent.Client) *WSHub {
	return &WSHub{Client: client}
}

// loadSources builds the ordered list of datasources from GeneralSettings,
// optionally shuffling them when the random flag is set. Shared by the preview
// and device feeds.
func (h *WSHub) loadSources(settings *ent.GeneralSettings) []sourceWithName {
	var sources []sourceWithName

	sonarr, _ := settings.Edges.SonarrOrErr()
	for _, s := range sonarr {
		sources = append(sources, sourceWithName{Name: "Sonarr", Source: &datasource.SonarrDS{Token: s.Token, URL: s.URL}, cacheKey: fmt.Sprintf("sonarr:%d", s.ID)})
	}

	radarr, _ := settings.Edges.RadarrOrErr()
	for _, r := range radarr {
		sources = append(sources, sourceWithName{Name: "Radarr", Source: &datasource.RadarrDS{Token: r.Token, URL: r.URL}, cacheKey: fmt.Sprintf("radarr:%d", r.ID)})
	}

	f1s, _ := settings.Edges.F1OrErr()
	for _, f := range f1s {
		sources = append(sources, sourceWithName{Name: "F1", Source: &datasource.F1DS{Token: f.Token, URL: f.URL}, cacheKey: fmt.Sprintf("f1:%d", f.ID)})
	}

	weather, _ := settings.Edges.WeatherOrErr()
	for _, w := range weather {
		sources = append(sources, sourceWithName{Name: "Weather", Source: &datasource.WeatherDS{Token: w.Token, URL: w.URL}, cacheKey: fmt.Sprintf("weather:%d", w.ID)})
	}

	ha, _ := settings.Edges.HomeAssistantOrErr()
	for _, haItem := range ha {
		sources = append(sources, sourceWithName{Name: "HomeAssistant", Source: &datasource.HomeAssistantDS{Token: haItem.Token, URL: haItem.URL}, cacheKey: fmt.Sprintf("homeassistant:%d", haItem.ID)})
	}

	untappd, _ := settings.Edges.UntappdOrErr()
	for _, u := range untappd {
		sources = append(sources, sourceWithName{Name: "Untappd", Source: &datasource.UntappdDS{Token: u.Token, URL: u.URL}, cacheKey: fmt.Sprintf("untappd:%d", u.ID)})
	}

	images, _ := settings.Edges.ImagesOrErr()
	for _, img := range images {
		sources = append(sources, sourceWithName{Name: "Image", Source: &datasource.ImageDS{Path: img.Path}, cacheKey: fmt.Sprintf("images:%d", img.ID)})
	}

	videos, _ := settings.Edges.VideosOrErr()
	for _, vid := range videos {
		sources = append(sources, sourceWithName{Name: "Video", Source: &datasource.VideoDS{Path: vid.Path}, cacheKey: fmt.Sprintf("videos:%d", vid.ID)})
	}

	crypto, _ := settings.Edges.CryptoOrErr()
	for _, cr := range crypto {
		sources = append(sources, sourceWithName{Name: "Crypto", Source: &datasource.CryptoDS{Token: cr.Token, URL: cr.URL}, cacheKey: fmt.Sprintf("crypto:%d", cr.ID)})
	}

	stocks, _ := settings.Edges.StocksOrErr()
	for _, st := range stocks {
		sources = append(sources, sourceWithName{Name: "Stock", Source: &datasource.StockDS{Token: st.Token, URL: st.URL}, cacheKey: fmt.Sprintf("stock:%d", st.ID)})
	}

	// Built-in: System Stats (always available, no config)
	sources = append(sources, sourceWithName{Name: "System Stats", Source: &datasource.SystemStatsDS{}, cacheKey: "systemstats:0"})

	// Built-in: ambience modes (always available, no config)
	sources = append(sources, sourceWithName{Name: "Analog Clock", Source: &datasource.AnalogClockDS{}, cacheKey: "analog-clock:0"})
	sources = append(sources, sourceWithName{Name: "Matrix Rain", Source: &datasource.MatrixRainDS{}, cacheKey: "matrix-rain:0"})

	rssFeeds, _ := settings.Edges.RssFeedsOrErr()
	for _, rs := range rssFeeds {
		sources = append(sources, sourceWithName{Name: "RSS: " + rs.Name, Source: &datasource.RssFeedDS{URL: rs.URL, Name: rs.Name}, cacheKey: fmt.Sprintf("rssfeed:%d", rs.ID)})
	}

	calendars, _ := settings.Edges.CalendarsOrErr()
	for _, cl := range calendars {
		sources = append(sources, sourceWithName{Name: "Calendar: " + cl.Name, Source: &datasource.CalendarDS{URL: cl.URL, Name: cl.Name}, cacheKey: fmt.Sprintf("calendar:%d", cl.ID)})
	}

	textSlides, _ := settings.Edges.TextSlidesOrErr()
	for _, ts := range textSlides {
		sources = append(sources, sourceWithName{Name: "Text: " + ts.Content, Source: &datasource.TextSlideDS{Content: ts.Content, Color: ts.Color, BgColor: ts.BgColor, FontSize: ts.FontSize}, cacheKey: fmt.Sprintf("textslides:%d", ts.ID)})
	}

	gcs, _ := settings.Edges.GoogleCalendarsOrErr()
	for _, gc := range gcs {
		sources = append(sources, sourceWithName{Name: "Google Calendar: " + gc.Name, Source: &datasource.GoogleCalendarDS{URL: gc.URL, Name: gc.Name}, cacheKey: fmt.Sprintf("googlecalendar:%d", gc.ID)})
	}

	newsFeeds, _ := settings.Edges.NewsFeedsOrErr()
	for _, nf := range newsFeeds {
		sources = append(sources, sourceWithName{Name: "News: " + nf.Name, Source: &datasource.NewsDS{URL: nf.URL, Name: nf.Name}, cacheKey: fmt.Sprintf("newsfeed:%d", nf.ID)})
	}

	apis, _ := settings.Edges.GenericApisOrErr()
	for _, ga := range apis {
		label := datasource.GenericAPITitle(ga.Config)
		if label == "" {
			label = "Custom API"
		}
		sources = append(sources, sourceWithName{Name: "API: " + label, Source: &datasource.GenericAPIDS{Token: ga.Token, URL: ga.URL, Config: ga.Config}, cacheKey: fmt.Sprintf("genericapi:%d", ga.ID)})
	}

	// Enabled countdown timers stream as "Countdown: <name>".
	countdowns, _ := settings.Edges.CountdownsOrErr()
	for _, cd := range countdowns {
		if !cd.Enabled {
			continue
		}
		sources = append(sources, sourceWithName{Name: "Countdown: " + cd.Name, Source: &datasource.CountdownDS{Name: cd.Name, Label: cd.Label, Target: cd.TargetTime}, cacheKey: fmt.Sprintf("countdown:%d", cd.ID)})
	}

	// Enabled AI digests stream as "AI: <name>".
	aiCfg := h.aiConfig(context.Background())
	digests, _ := settings.Edges.AiDigestsOrErr()
	for _, d := range digests {
		if !d.Enabled {
			continue
		}
		sources = append(sources, sourceWithName{Name: "AI: " + d.Name, Source: &datasource.AIDigestDS{
			ID:       d.ID,
			Name:     d.Name,
			Prompt:   d.Prompt,
			FeedURLs: digestFeedURLs(settings, datasource.ParseDigestSources(d.Sources)),
			TTL:      time.Duration(d.TTLMinutes) * time.Minute,
			Config:   aiCfg,
		}, cacheKey: fmt.Sprintf("aidigest:%d", d.ID)})
	}

	// Enabled matrix layouts stream as a single "matrix:<name>" source.
	layouts, _ := settings.Edges.MatrixLayoutsOrErr()
	for _, ml := range layouts {
		if !ml.Enabled {
			continue
		}
		mds := h.buildMatrixDS(settings, ml, 0)
		if mds == nil {
			continue
		}
		sources = append(sources, sourceWithName{Name: "matrix:" + ml.Name, Source: mds, cacheKey: fmt.Sprintf("matrix:%d", ml.ID)})
	}

	if settings.Random {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(sources), func(i, j int) {
			sources[i], sources[j] = sources[j], sources[i]
		})
	}

	return sources
}

// HandleWS serves the browser preview feed at a fixed 400x400 resolution,
// controlled by the shared GlobalFeed controller.
func (h *WSHub) HandleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("WebSocket upgrade error", "error", err, "source", "websocket")
		return
	}
	defer conn.Close()

	settings, err := h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().Only(c.Request.Context())
	if err != nil {
		slog.Error("Failed to load settings for WebSocket", "error", err, "source", "websocket")
		return
	}

	sources := h.loadSources(settings)
	if len(sources) == 0 {
		msg, _ := json.Marshal(map[string]string{"error": "no datasources configured"})
		conn.WriteMessage(websocket.TextMessage, msg)
		return
	}

	timeout := time.Duration(settings.Timeout * float64(time.Second))
	serveFeed(conn, feedConn{}, sources, settings.Random, timeout, 400, 400, GlobalFeed)
}

// HandleDeviceWS serves a device feed at the device's configured resolution
// and refresh interval, authenticated by the device token.
func (h *WSHub) HandleDeviceWS(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}

	device, err := h.Client.DeviceSettings.Query().Where(devicesettings.TokenEQ(token)).Only(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	if !device.Enabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "device disabled"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("Device WebSocket upgrade error", "error", err, "source", "websocket", "device", device.Name)
		return
	}
	defer conn.Close()

	// Mark the device as seen; clear on disconnect so status reflects
	// connectivity.
	if err := h.Client.DeviceSettings.UpdateOneID(device.ID).SetLastSeenAt(time.Now()).Exec(context.Background()); err != nil {
		slog.Warn("failed to update device last_seen_at", "device", device.Name, "error", err)
	}
	defer func() {
		if err := h.Client.DeviceSettings.UpdateOneID(device.ID).ClearLastSeenAt().Exec(context.Background()); err != nil {
			slog.Warn("failed to clear device last_seen_at", "device", device.Name, "error", err)
		}
	}()

	settings, err := h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().Only(c.Request.Context())
	if err != nil {
		slog.Error("Failed to load settings for device WebSocket", "error", err, "source", "websocket", "device", device.Name)
		return
	}

	sources := h.loadSources(settings)
	if len(sources) == 0 {
		msg, _ := json.Marshal(map[string]string{"error": "no datasources configured"})
		conn.WriteMessage(websocket.TextMessage, msg)
		return
	}

	width := device.Width
	if width <= 0 {
		width = 64
	}
	height := device.Height
	if height <= 0 {
		height = 64
	}
	interval := device.RefreshInterval
	if interval <= 0 {
		interval = 60
	}
	timeout := time.Duration(interval) * time.Second

	// Each device gets its own feed controller so pause/skip/next are
	// independent of the shared preview feed.
	serveFeed(conn, feedConn{
		deviceID: device.ID,
		frames: func() {
			if err := h.Client.DeviceSettings.UpdateOneID(device.ID).AddFramesServed(1).Exec(context.Background()); err != nil {
				slog.Warn("failed to increment device frames_served", "device", device.Name, "error", err)
			}
		},
	}, sources, settings.Random, timeout, width, height, &FeedController{})
}

// HandleDevicePreviewWS serves an admin-authenticated, device-accurate preview
// feed at the device's configured resolution and refresh interval. Unlike
// HandleDeviceWS it is keyed by device id (admin session, no token) and never
// writes last_seen_at, so a browser preview cannot fake device liveness.
func (h *WSHub) HandleDevicePreviewWS(c *gin.Context) {
	// The route param is named ":token" (see server.go) so it coexists with
	// /ws/device/:token in gin's tree; semantically it is the device id.
	id, err := strconv.Atoi(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}

	device, err := h.Client.DeviceSettings.Query().Where(devicesettings.ID(id)).Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load device"})
		}
		return
	}
	if !device.Enabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "device disabled"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("Device preview WebSocket upgrade error", "error", err, "source", "websocket", "device", device.Name)
		return
	}
	defer conn.Close()

	settings, err := h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().Only(c.Request.Context())
	if err != nil {
		slog.Error("Failed to load settings for device preview WebSocket", "error", err, "source", "websocket", "device", device.Name)
		return
	}

	sources := h.loadSources(settings)
	if len(sources) == 0 {
		msg, _ := json.Marshal(map[string]string{"error": "no datasources configured"})
		conn.WriteMessage(websocket.TextMessage, msg)
		return
	}

	width := device.Width
	if width <= 0 {
		width = 64
	}
	height := device.Height
	if height <= 0 {
		height = 64
	}
	interval := device.RefreshInterval
	if interval <= 0 {
		interval = 60
	}
	timeout := time.Duration(interval) * time.Second

	// Each preview gets its own feed controller: pause/skip/next in the
	// preview tab only affects that tab, never the physical device.
	serveFeed(conn, feedConn{cacheKeyPrefix: fmt.Sprintf("device:%d:", id), deviceID: id}, sources, settings.Random, timeout, width, height, &FeedController{})
}

// serveFeed runs the source-cycle loop for a single WebSocket connection,
// rendering each datasource at the given resolution and advancing on the given
// timeout. Notifications are broadcast to every connection exactly once.
func serveFeed(conn *websocket.Conn, fc feedConn, sources []sourceWithName, random bool, timeout time.Duration, width, height int, feed *FeedController) {
	cursor := CurrentNotifSeq()

	// Read control messages in a goroutine
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var cmd map[string]string
			if err := json.Unmarshal(msg, &cmd); err != nil {
				continue
			}
			switch cmd["action"] {
			case "next":
				feed.Next()
			case "pause":
				feed.Pause()
			case "resume":
				feed.Resume()
			}
		}
	}()

	for {
		for i, sw := range sources {
			// Broadcast any new notifications to this connection.
			if notifs := NotificationsAfter(cursor); len(notifs) > 0 {
				for _, n := range notifs {
					msg := map[string]string{
						"format":  "PNG",
						"source":  "NOTIFICATION",
						"title":   n.Title,
						"message": n.Message,
					}
					data, _ := json.Marshal(msg)
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return
					}
					cursor = n.ID
				}
				time.Sleep(timeout)
				continue
			}

			// Compute next source name
			nextName := ""
			if random {
				nextName = sources[rand.Intn(len(sources))].Name
			} else {
				nextIdx := (i + 1) % len(sources)
				nextName = sources[nextIdx].Name
			}

			feed.SetCurrent(sw.Name, nextName)

			// Wait if paused
			for feed.IsPaused() {
				time.Sleep(100 * time.Millisecond)
			}

			// Render through the last-known-good cache: successful renders are
			// cached, failures serve the cached frame marked stale. Health is
			// recorded per source (and per device for device feeds).
			cacheKey := lkgCacheKey(fc.cacheKeyPrefix+sw.cacheKey, width, height)
			img, stale, err := defaultLKG.GetPNG(cacheKey, datasourceConfigSig(sw.Source), func() (*render.RenderedImage, error) {
				start := time.Now()
				img, err := sw.Source.GetPNG(width, height)
				dur := time.Since(start)
				if err != nil {
					Health.RecordFailure(sw.cacheKey, err, dur)
					if fc.deviceID > 0 {
						Health.RecordFailure(fmt.Sprintf("device:%d", fc.deviceID), err, dur)
					}
				} else {
					Health.RecordSuccess(sw.cacheKey, dur)
				}
				return img, err
			})
			if err != nil {
				slog.Error("Error rendering datasource for WebSocket", "source_name", sw.Name, "error", err, "source", "websocket")
				continue
			}

			msg := map[string]any{
				"format": img.Format,
				"image":  string(img.Data),
				"source": sw.Name,
				"next":   nextName,
			}
			if stale {
				msg["stale"] = true
				msg["stale_age"] = defaultLKG.StaleAge(cacheKey)
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				slog.Warn("WebSocket write error", "error", err, "source", "websocket")
				if fc.deviceID > 0 {
					Health.RecordFailure(fmt.Sprintf("device:%d", fc.deviceID), err, 0)
				}
				return
			}
			if fc.deviceID > 0 {
				Health.RecordSuccess(fmt.Sprintf("device:%d", fc.deviceID), 0)
				if fc.frames != nil {
					fc.frames()
				}
			}
			TrackDisplay(sw.Name, timeout.Seconds())

			// Wait for timeout or skip signal
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				if feed.ShouldSkip() {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}
