package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow same-origin requests (admin panel)
		origin := r.Header.Get("Origin")
		if origin == "" {
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
	Name   string
	Source datasource.Datasource
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
		sources = append(sources, sourceWithName{Name: "Sonarr", Source: &datasource.SonarrDS{Token: s.Token, URL: s.URL}})
	}

	radarr, _ := settings.Edges.RadarrOrErr()
	for _, r := range radarr {
		sources = append(sources, sourceWithName{Name: "Radarr", Source: &datasource.RadarrDS{Token: r.Token, URL: r.URL}})
	}

	f1s, _ := settings.Edges.F1OrErr()
	for _, f := range f1s {
		sources = append(sources, sourceWithName{Name: "F1", Source: &datasource.F1DS{Token: f.Token, URL: f.URL}})
	}

	weather, _ := settings.Edges.WeatherOrErr()
	for _, w := range weather {
		sources = append(sources, sourceWithName{Name: "Weather", Source: &datasource.WeatherDS{Token: w.Token, URL: w.URL}})
	}

	ha, _ := settings.Edges.HomeAssistantOrErr()
	for _, haItem := range ha {
		sources = append(sources, sourceWithName{Name: "HomeAssistant", Source: &datasource.HomeAssistantDS{Token: haItem.Token, URL: haItem.URL}})
	}

	untappd, _ := settings.Edges.UntappdOrErr()
	for _, u := range untappd {
		sources = append(sources, sourceWithName{Name: "Untappd", Source: &datasource.UntappdDS{Token: u.Token, URL: u.URL}})
	}

	images, _ := settings.Edges.ImagesOrErr()
	for _, img := range images {
		sources = append(sources, sourceWithName{Name: "Image", Source: &datasource.ImageDS{Path: img.Path}})
	}

	videos, _ := settings.Edges.VideosOrErr()
	for _, vid := range videos {
		sources = append(sources, sourceWithName{Name: "Video", Source: &datasource.VideoDS{Path: vid.Path}})
	}

	crypto, _ := settings.Edges.CryptoOrErr()
	for _, cr := range crypto {
		sources = append(sources, sourceWithName{Name: "Crypto", Source: &datasource.CryptoDS{Token: cr.Token, URL: cr.URL}})
	}

	stocks, _ := settings.Edges.StocksOrErr()
	for _, st := range stocks {
		sources = append(sources, sourceWithName{Name: "Stock", Source: &datasource.StockDS{Token: st.Token, URL: st.URL}})
	}

	// Built-in: System Stats (always available, no config)
	sources = append(sources, sourceWithName{Name: "System Stats", Source: &datasource.SystemStatsDS{}})

	rssFeeds, _ := settings.Edges.RssFeedsOrErr()
	for _, rs := range rssFeeds {
		sources = append(sources, sourceWithName{Name: "RSS: " + rs.Name, Source: &datasource.RssFeedDS{URL: rs.URL, Name: rs.Name}})
	}

	calendars, _ := settings.Edges.CalendarsOrErr()
	for _, cl := range calendars {
		sources = append(sources, sourceWithName{Name: "Calendar: " + cl.Name, Source: &datasource.CalendarDS{URL: cl.URL, Name: cl.Name}})
	}

	textSlides, _ := settings.Edges.TextSlidesOrErr()
	for _, ts := range textSlides {
		sources = append(sources, sourceWithName{Name: "Text: " + ts.Content, Source: &datasource.TextSlideDS{Content: ts.Content, Color: ts.Color, BgColor: ts.BgColor, FontSize: ts.FontSize}})
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

	settings, err := h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().Only(c.Request.Context())
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
	serveFeed(conn, sources, settings.Random, timeout, 400, 400, GlobalFeed)
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

	settings, err := h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithRssFeeds().WithCalendars().WithStocks().WithTextSlides().Only(c.Request.Context())
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
	serveFeed(conn, sources, settings.Random, timeout, width, height, &FeedController{})
}

// serveFeed runs the source-cycle loop for a single WebSocket connection,
// rendering each datasource at the given resolution and advancing on the given
// timeout. Notifications are broadcast to every connection exactly once.
func serveFeed(conn *websocket.Conn, sources []sourceWithName, random bool, timeout time.Duration, width, height int, feed *FeedController) {
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

			img, err := sw.Source.GetPNG(width, height)
			if err != nil {
				slog.Error("Error rendering datasource for WebSocket", "source_name", sw.Name, "error", err, "source", "websocket")
				continue
			}

			msg := map[string]string{
				"format": img.Format,
				"image":  string(img.Data),
				"source": sw.Name,
				"next":   nextName,
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				slog.Warn("WebSocket write error", "error", err, "source", "websocket")
				return
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
