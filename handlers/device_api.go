package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/playlist"
	"ledit/ent/scene"
)

// REST surface for remote-control clients (the Flutter Android app and any
// other API consumer). Reads report fleet state; the POST handlers are thin
// wrappers that converge on the shared actuation layer (handlers/actuation.go)
// so MQTT, Home Assistant, event rules and the app all share one set of
// brightness/select/message semantics.

// APIDeviceList reports every configured device with its liveness, live feed
// state, brightness and content binding.
func (s *Server) APIDeviceList(c *gin.Context) {
	devices, _ := s.DB.DeviceSettings.Query().Order(ent.Asc(devicesettings.FieldID)).All(s.Ctx)
	playlists, _ := s.DB.Playlist.Query().All(s.Ctx)
	playlistNames := map[int]string{}
	for _, pl := range playlists {
		playlistNames[pl.ID] = pl.Name
	}
	groups, _ := s.DB.DeviceGroup.Query().All(s.Ctx)
	groupNames := map[int]string{}
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}
	type deviceJSON struct {
		ID           int     `json:"id"`
		Name         string  `json:"name"`
		Enabled      bool    `json:"enabled"`
		Width        int     `json:"width"`
		Height       int     `json:"height"`
		Liveness     string  `json:"liveness"`
		Online       bool    `json:"online"`
		Live         bool    `json:"live"`
		Brightness   int     `json:"brightness"`
		ContentMode  string  `json:"content_mode"`
		PlaylistID   *int    `json:"playlist_id"`
		PlaylistName string  `json:"playlist_name"`
		GroupID      *int    `json:"group_id"`
		GroupName    string  `json:"group_name"`
		LastSeenAt   *string `json:"last_seen_at"`
	}
	out := make([]deviceJSON, 0, len(devices))
	for _, d := range devices {
		liveness := deviceLiveness(d.LastSeenAt, d.RefreshInterval)
		_, live := getDeviceFeed(d.ID)
		pn := ""
		if d.PlaylistID != nil {
			pn = playlistNames[*d.PlaylistID]
		}
		gn := ""
		if d.GroupID != nil {
			gn = groupNames[*d.GroupID]
		}
		var lastSeen *string
		if d.LastSeenAt != nil {
			ls := d.LastSeenAt.Format("2006-01-02T15:04:05Z07:00")
			lastSeen = &ls
		}
		out = append(out, deviceJSON{
			ID:           d.ID,
			Name:         d.Name,
			Enabled:      d.Enabled,
			Width:        d.Width,
			Height:       d.Height,
			Liveness:     liveness,
			Online:       liveness == "alive",
			Live:         live,
			Brightness:   s.currentDeviceBrightness(d.ID),
			ContentMode:  d.ContentMode,
			PlaylistID:   d.PlaylistID,
			PlaylistName: pn,
			GroupID:      d.GroupID,
			GroupName:    gn,
			LastSeenAt:   lastSeen,
		})
	}
	c.JSON(http.StatusOK, gin.H{"devices": out})
}

// APIPlaylistList reports playlists that a remote client can activate with
// POST /api/devices/:id/select {"target":"playlist:<id>"}.
func (s *Server) APIPlaylistList(c *gin.Context) {
	rows, _ := s.DB.Playlist.Query().Order(ent.Asc(playlist.FieldID)).All(s.Ctx)
	type playlistJSON struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		ItemCount int    `json:"item_count"`
	}
	out := make([]playlistJSON, 0, len(rows))
	for _, pl := range rows {
		count := 0
		if items, err := datasource.ParsePlaylistItems(pl.Items); err == nil {
			count = len(items)
		}
		out = append(out, playlistJSON{ID: pl.ID, Name: pl.Name, Enabled: pl.Enabled, ItemCount: count})
	}
	c.JSON(http.StatusOK, gin.H{"playlists": out})
}

// APISceneList reports ambient scenes that a remote client can activate with
// POST /api/devices/:id/select {"target":"scene:<id>"}.
func (s *Server) APISceneList(c *gin.Context) {
	rows, _ := s.DB.Scene.Query().Order(ent.Asc(scene.FieldID)).All(s.Ctx)
	type sceneJSON struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Enabled    bool   `json:"enabled"`
		Active     bool   `json:"active"`
		TTLSeconds *int   `json:"ttl_seconds"`
	}
	out := make([]sceneJSON, 0, len(rows))
	for _, sc := range rows {
		out = append(out, sceneJSON{
			ID:         sc.ID,
			Name:       sc.Name,
			Enabled:    sc.Enabled,
			Active:     globalSceneManager.IsActive(sc.ID),
			TTLSeconds: sc.TTLSeconds,
		})
	}
	c.JSON(http.StatusOK, gin.H{"scenes": out})
}

// APIImageList reports uploaded image datasources (media library).
func (s *Server) APIImageList(c *gin.Context) {
	rows, _ := s.DB.Image.Query().Order(ent.Asc("id")).All(s.Ctx)
	type imageJSON struct {
		ID   int    `json:"id"`
		Path string `json:"path"`
	}
	out := make([]imageJSON, 0, len(rows))
	for _, img := range rows {
		out = append(out, imageJSON{ID: img.ID, Path: img.Path})
	}
	c.JSON(http.StatusOK, gin.H{"images": out})
}

// apiDeviceID parses the :id path parameter, writing a 400 response when it is
// not a positive integer.
func apiDeviceID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return 0, false
	}
	return id, true
}

// apiDeviceExists reports whether the device id is known, writing a 404 when it
// is not.
func (s *Server) apiDeviceExists(c *gin.Context, id int) bool {
	if _, err := s.DB.DeviceSettings.Get(s.Ctx, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return false
	}
	return true
}

// APIDeviceBrightness sets a device brightness override (0-100).
func (s *Server) APIDeviceBrightness(c *gin.Context) {
	id, ok := apiDeviceID(c)
	if !ok {
		return
	}
	var req struct {
		Level      *int `json:"level"`
		Brightness *int `json:"brightness"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	level := req.Level
	if level == nil {
		level = req.Brightness
	}
	if level == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "level is required"})
		return
	}
	if *level < 0 || *level > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "level must be between 0 and 100"})
		return
	}
	if !s.apiDeviceExists(c, id) {
		return
	}
	s.ApplyDeviceBrightness(id, *level, "api")
	c.JSON(http.StatusOK, gin.H{"status": "ok", "device_id": id, "brightness": *level})
}

// APIDeviceSelect switches a device to a playlist or scene, or pins a rotation
// source, using the shared selection grammar.
func (s *Server) APIDeviceSelect(c *gin.Context) {
	id, ok := apiDeviceID(c)
	if !ok {
		return
	}
	var req struct {
		Target string `json:"target"`
		Select string `json:"select"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	target := req.Target
	if target == "" {
		target = req.Select
	}
	if target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target is required"})
		return
	}
	if !s.apiDeviceExists(c, id) {
		return
	}
	if err := s.ApplyDeviceSelect(id, target); err != nil {
		status := http.StatusBadRequest
		if !errors.Is(err, ErrSelectInvalid) {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "device_id": id, "target": target})
}

// APIDeviceMessage pushes an immediate priority text message to one device
// with an optional TTL in seconds.
func (s *Server) APIDeviceMessage(c *gin.Context) {
	id, ok := apiDeviceID(c)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
		TTL  int    `json:"ttl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}
	if !s.apiDeviceExists(c, id) {
		return
	}
	if err := s.ApplyDeviceMessageTTL(id, req.Text, req.TTL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "device_id": id})
}
