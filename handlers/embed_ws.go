package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
)

// embedSettings loads GeneralSettings with every datasource edge so the
// shared embed can reuse the exact same source set as the wall.
func (h *WSHub) embedSettings(c *gin.Context) (*ent.GeneralSettings, error) {
	return h.Client.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().
		WithImages().WithVideos().WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCompositions().WithCountdowns().WithAiDigests().WithNowPlayingSources().WithTransits().
		WithUptimes().WithPiholes().WithGithubs().WithSports().WithSunmoons().WithJellyfins().WithQrcodes().
		WithImmichs().WithQbittorrents().WithSabnzbd().WithOverseerrs().WithUptimeKumas().WithSpeedtests().WithAdguards().WithFrigates().WithZigbee2mqtts().WithTransmissions().WithProxmoxs().WithWastes().WithAirqualities().WithParcels().Only(c.Request.Context())
}

// embedSize resolves the requested frame size: an absent w/h uses def, a
// present one is clamped to the same bounds as every other preview surface.
func embedSize(c *gin.Context, defW, defH int) (int, int) {
	w := clampPreviewSize(defW)
	if q := c.Query("w"); q != "" {
		w = clampPreviewSize(mustAtoi(q))
	}
	h := clampPreviewSize(defH)
	if q := c.Query("h"); q != "" {
		h = clampPreviewSize(mustAtoi(q))
	}
	return w, h
}

// HandleEmbedWS serves the anonymous shared embed: the same wall rotation at a
// caller-chosen size. It enforces a per-IP upgrade rate limit before the
// WebSocket handshake so a refetch loop cannot exhaust connections.
func (h *WSHub) HandleEmbedWS(c *gin.Context) {
	if ok, retry := checkWindowRateLimit(embedUpgradeKeyIP+c.ClientIP(), embedUpgradeIPLimit); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	width, height := embedSize(c, 400, 400)

	settings, err := h.embedSettings(c)
	if err != nil {
		slog.Error("Failed to load settings for embed WebSocket", "error", err, "source", "embed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("Embed WebSocket upgrade error", "error", err, "source", "embed")
		return
	}
	defer conn.Close()

	sources := h.loadSources(settings)
	if len(sources) == 0 {
		msg, _ := json.Marshal(map[string]string{"error": "no datasources configured"})
		conn.WriteMessage(websocket.TextMessage, msg)
		return
	}

	timeout := time.Duration(settings.Timeout * float64(time.Second))
	// GlobalFeed keeps the embed a mirror of the wall (pin/alarm/scene state)
	// without registering a device feed or touching its health counters.
	serveFeed(conn, feedConn{}, sources, settings.Random, timeout, width, height, GlobalFeed, settings.TransitionStyle, settings.TransitionMs, nil)
}

// HandleEmbedDeviceWS serves one device's feed to an embed client authorised
// by an admin session or an embed-scoped guest token. Unlike the device feed
// it never registers a device feed, updates last_seen_at, or increments the
// frames-served counter, and it uses its own controller so embed controls
// cannot disturb the physical device.
func (h *WSHub) HandleEmbedDeviceWS(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}

	// Token-scoped limit for token clients, IP bucket for admin sessions.
	rateKey := embedUpgradeKeyIP + c.ClientIP()
	rateLimit := embedUpgradeIPLimit
	if tok := currentGuestToken(c); tok != nil {
		rateKey = "embed:ws:tok:" + strconv.Itoa(tok.ID)
		rateLimit = embedUpgradeTokRate
	}
	if ok, retry := checkWindowRateLimit(rateKey, rateLimit); !ok {
		abortGuestRateLimited(c, retry)
		return
	}

	device, err := h.Client.DeviceSettings.Query().Where(devicesettings.ID(id)).WithGroup().Only(c.Request.Context())
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

	width := device.Width
	if width <= 0 {
		width = 400
	}
	height := device.Height
	if height <= 0 {
		height = 400
	}
	width, height = embedSize(c, width, height)

	settings, err := h.embedSettings(c)
	if err != nil {
		slog.Error("Failed to load settings for device embed WebSocket", "error", err, "source", "embed", "device", device.Name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("Device embed WebSocket upgrade error", "error", err, "source", "embed", "device", device.Name)
		return
	}
	defer conn.Close()

	sources := h.composeDeviceSources(device, settings)
	if len(sources) == 0 {
		msg, _ := json.Marshal(map[string]string{"error": "no datasources configured"})
		conn.WriteMessage(websocket.TextMessage, msg)
		return
	}

	interval := device.RefreshInterval
	if interval <= 0 {
		interval = 60
	}
	timeout := time.Duration(interval) * time.Second

	playlistActive := device.ContentMode == "playlist" || device.ContentMode == "scheduled"
	randomFlag := settings.Random
	if playlistActive {
		randomFlag = false
	}

	serveFeed(conn, feedConn{cacheKeyPrefix: fmt.Sprintf("device:%d:", id), deviceID: id, overlay: overlaySpecForDeviceWithGroup(device), panelCols: device.PanelCols, panelGap: device.PanelGap}, sources, randomFlag, timeout, width, height, &FeedController{}, settings.TransitionStyle, settings.TransitionMs, nil)
}
