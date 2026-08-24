package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent/playlist"
)

func (s *Server) HandlePlaylistResolve(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.Header("WWW-Authenticate", `Bearer realm="LEDit"`)
		c.Header("Cache-Control", "no-store")
		c.Header("Vary", "Cookie, Authorization")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	deviceIDStr := c.Query("device_id")
	if deviceIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id is required"})
		return
	}
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id must be an integer"})
		return
	}

	var atTime time.Time
	if atStr := c.Query("at"); atStr != "" {
		t, err := time.Parse(time.RFC3339, atStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid at parameter, must be RFC3339"})
			return
		}
		atTime = t.In(time.Local)
	} else {
		atTime = ScheduleNow()
	}

	device, err := s.DB.DeviceSettings.Get(c.Request.Context(), deviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	zone := serverZoneLabel()
	serverTimeStr := atTime.Format(time.RFC3339)

	// Non-scheduled mode
	if device.ContentMode != "scheduled" {
		c.JSON(http.StatusOK, gin.H{
			"serverTime":     serverTimeStr,
			"zone":           zone,
			"activePlaylist": nil,
			"matchedWindow":  nil,
			"nextSwitchAt":   nil,
			"fallbackReason": "device not in scheduled mode",
		})
		return
	}

	// Parse candidate ids
	var ids []int
	if err := json.Unmarshal([]byte(device.ScheduledPlaylistIds), &ids); err != nil {
		ids = []int{}
	}
	if ids == nil {
		ids = []int{}
	}

	var candidates []PlaylistSchedule
	var playlistByID map[int]*struct {
		ID              int
		Name            string
		Enabled         bool
		Items           string
		ScheduleWindows string
	}
	if len(ids) > 0 {
		pls, _ := s.DB.Playlist.Query().Where(playlist.IDIn(ids...)).All(c.Request.Context())
		m := make(map[int]*struct {
			ID              int
			Name            string
			Enabled         bool
			Items           string
			ScheduleWindows string
		})
		for _, pl := range pls {
			m[pl.ID] = &struct {
				ID              int
				Name            string
				Enabled         bool
				Items           string
				ScheduleWindows string
			}{ID: pl.ID, Name: pl.Name, Enabled: pl.Enabled, Items: pl.Items, ScheduleWindows: pl.ScheduleWindows}
		}
		playlistByID = m
		// preserve order of ids
		for idx, id := range ids {
			pl, ok := m[id]
			if !ok {
				continue
			}
			windows, _ := ParseScheduleWindows(pl.ScheduleWindows)
			if windows == nil {
				windows = []ScheduleWindow{}
			}
			candidates = append(candidates, PlaylistSchedule{
				ID:      pl.ID,
				Name:    pl.Name,
				Enabled: pl.Enabled,
				Windows: windows,
				Order:   idx,
			})
		}
	}

	active := ResolveScheduledPlaylist(atTime, candidates)

	var nextSwitchAt *string
	if len(candidates) > 0 {
		if ns := NextSwitchTime(atTime, candidates); !ns.IsZero() {
			s := ns.Format(time.RFC3339)
			nextSwitchAt = &s
		}
	}

	// Helper to build activePlaylist response
	type playlistResp struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}

	var activePlaylist interface{} = nil
	var matchedWindow interface{} = nil
	fallbackReason := ""

	if active != nil {
		// Check resolvability
		plInfo, ok := playlistByID[active.ID]
		resolvable := -1
		if ok {
			items, err := datasource.ParsePlaylistItems(plInfo.Items)
			if err != nil {
				resolvable = 0
			} else if len(items) == 0 {
				resolvable = 0
			} else {
				// Try to build source index to count resolvable
				// Load general settings minimally
				// Use empty index if settings not found
				var idx *sourceIndex
				// Try to load with edges needed for source resolution
				// We attempt to fetch general settings with common edges
				if gs, err := s.DB.GeneralSettings.Query().Only(c.Request.Context()); err == nil && gs != nil {
					idx = buildSourceIndex(gs, s.aiConfig(c.Request.Context()))
				} else {
					// fallback empty
					idx = &sourceIndex{byKey: map[string]datasource.Datasource{}, names: map[string]string{}}
				}
				cnt := 0
				for _, it := range items {
					if _, _, err := idx.Resolve(it.SourceType, it.SourceID); err == nil {
						cnt++
					}
				}
				resolvable = cnt
			}
		}
		if resolvable == 0 {
			if device.FallbackPlaylistID != nil {
				if pl, err := s.DB.Playlist.Get(c.Request.Context(), *device.FallbackPlaylistID); err == nil {
					activePlaylist = playlistResp{ID: pl.ID, Name: pl.Name, Enabled: pl.Enabled}
					fallbackReason = "fallback playlist"
				} else {
					fallbackReason = "playlist has no resolvable items"
				}
			} else {
				fallbackReason = "playlist has no resolvable items"
			}
			matchedWindow = nil
		} else {
			activePlaylist = playlistResp{ID: active.ID, Name: active.Name, Enabled: active.Enabled}
			fallbackReason = ""
			// Determine matched window: highest priority matching window
			if mw := findMatchedWindow(atTime, active); mw != nil {
				matchedWindow = mw
			}
		}
	} else {
		// No active schedule
		if device.FallbackPlaylistID != nil {
			if pl, err := s.DB.Playlist.Get(c.Request.Context(), *device.FallbackPlaylistID); err == nil {
				activePlaylist = playlistResp{ID: pl.ID, Name: pl.Name, Enabled: pl.Enabled}
				fallbackReason = "fallback playlist"
			} else {
				fallbackReason = "no matching schedule, using global"
			}
		} else {
			fallbackReason = "no matching schedule, using global"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"serverTime":     serverTimeStr,
		"zone":           zone,
		"activePlaylist": activePlaylist,
		"matchedWindow":  matchedWindow,
		"nextSwitchAt":   nextSwitchAt,
		"fallbackReason": fallbackReason,
	})
}

func findMatchedWindow(now time.Time, ps *PlaylistSchedule) *ScheduleWindow {
	if ps == nil || len(ps.Windows) == 0 {
		return nil
	}
	var best *ScheduleWindow
	bestPrio := -1 << 30
	for i := range ps.Windows {
		w := ps.Windows[i]
		if !WindowMatches(now, w) {
			continue
		}
		if best == nil || w.Priority > bestPrio {
			cp := w
			best = &cp
			bestPrio = w.Priority
		}
	}
	return best
}
