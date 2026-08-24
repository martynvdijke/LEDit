package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/devicegroup"
	"ledit/ent/devicesettings"
)

const (
	MaxGroups          = 32
	MaxDevicesPerGroup = 64
)

// resolveGroupContent resolves group content assignment precedence helper exported for tests.
func isGroupContentSet(g *ent.DeviceGroup) bool {
	if g.ContentMode == "global" {
		return false
	}
	if g.ContentMode == "playlist" {
		return g.PlaylistID != nil
	}
	if g.ContentMode == "scheduled" {
		var ids []int
		_ = json.Unmarshal([]byte(g.ScheduledPlaylistIds), &ids)
		return len(ids) > 0 || g.FallbackPlaylistID != nil
	}
	return false
}

func isDeviceContentExplicit(d *ent.DeviceSettings) bool {
	if d.ContentMode == "global" {
		return false
	}
	if d.ContentMode == "playlist" {
		return d.PlaylistID != nil
	}
	if d.ContentMode == "scheduled" {
		var ids []int
		_ = json.Unmarshal([]byte(d.ScheduledPlaylistIds), &ids)
		return len(ids) > 0 || d.FallbackPlaylistID != nil
	}
	return false
}

func validateGroupContent(mode string, playlistIDRaw, scheduledRaw, fallbackRaw string, ctx *gin.Context, srv *Server) (string, *int, string, *int, bool) {
	if mode == "" {
		mode = "global"
	}
	if mode != "global" && mode != "playlist" && mode != "scheduled" {
		return "", nil, "", nil, false
	}
	var pid *int
	if mode == "playlist" && playlistIDRaw != "" {
		v, err := strconv.Atoi(playlistIDRaw)
		if err != nil {
			return "", nil, "", nil, false
		}
		pid = &v
		if _, err := srv.DB.Playlist.Get(srv.Ctx, v); err != nil {
			slog.Warn("dangling group playlist id", "playlist_id", v)
		}
	}
	sched := "[]"
	var fallbackID *int
	if mode == "scheduled" {
		if scheduledRaw == "" {
			scheduledRaw = "[]"
		}
		var ids []int
		if err := json.Unmarshal([]byte(scheduledRaw), &ids); err != nil {
			return "", nil, "", nil, false
		}
		if err := ValidateScheduledCandidates(ids); err != nil {
			return "", nil, "", nil, false
		}
		sched = scheduledRaw
		if fallbackRaw != "" {
			f, err := strconv.Atoi(fallbackRaw)
			if err != nil {
				return "", nil, "", nil, false
			}
			fallbackID = &f
			if _, err := srv.DB.Playlist.Get(srv.Ctx, f); err != nil {
				slog.Warn("dangling group fallback playlist id", "fallback_id", f)
			}
		}
	}
	return mode, pid, sched, fallbackID, true
}

func (s *Server) AdminGroupList(c *gin.Context) {
	groups, _ := s.DB.DeviceGroup.Query().WithDevices().All(s.Ctx)
	// compute member counts
	type row struct {
		*ent.DeviceGroup
		MemberCount int
	}
	var rows []row
	for _, g := range groups {
		cnt := len(g.Edges.Devices)
		rows = append(rows, row{g, cnt})
	}
	s.renderPage(c, http.StatusOK, "groups.html", gin.H{"groups": rows})
}

func (s *Server) AdminGroupNew(c *gin.Context) {
	playlists, _ := s.DB.Playlist.Query().All(s.Ctx)
	s.renderPage(c, http.StatusOK, "group_form.html", gin.H{"playlists": playlists})
}

func (s *Server) AdminGroupCreate(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	desc := strings.TrimSpace(c.PostForm("description"))
	contentMode := strings.TrimSpace(c.PostForm("content_mode"))
	playlistRaw := strings.TrimSpace(c.PostForm("playlist_id"))
	schedRaw := strings.TrimSpace(c.PostForm("scheduled_playlist_ids"))
	fallbackRaw := strings.TrimSpace(c.PostForm("fallback_playlist_id"))
	if name == "" || len(name) > 64 {
		SetFlash(c, "danger", "Name must be 1-64 chars")
		c.Redirect(http.StatusFound, "/admin/groups")
		return
	}
	if len(desc) > 256 {
		SetFlash(c, "danger", "Description max 256 chars")
		c.Redirect(http.StatusFound, "/admin/groups")
		return
	}
	// uniqueness case-insensitive
	existing, _ := s.DB.DeviceGroup.Query().All(s.Ctx)
	for _, g := range existing {
		if strings.EqualFold(g.Name, name) {
			SetFlash(c, "danger", "Group name already exists")
			c.Redirect(http.StatusFound, "/admin/groups")
			return
		}
	}
	if len(existing) >= MaxGroups {
		SetFlash(c, "danger", "Group cap exceeded (32)")
		c.Redirect(http.StatusFound, "/admin/groups")
		return
	}
	mode, pid, sched, fallback, ok := validateGroupContent(contentMode, playlistRaw, schedRaw, fallbackRaw, c, s)
	if !ok {
		SetFlash(c, "danger", "Invalid group content assignment")
		c.Redirect(http.StatusFound, "/admin/groups")
		return
	}
	builder := s.DB.DeviceGroup.Create().SetName(name).SetDescription(desc).SetCreatedAt(time.Now()).SetContentMode(mode).SetScheduledPlaylistIds(sched)
	if pid != nil {
		builder.SetPlaylistID(*pid)
	}
	if fallback != nil {
		builder.SetFallbackPlaylistID(*fallback)
	}
	builder.SaveX(s.Ctx)
	SetFlash(c, "success", "Group created")
	c.Redirect(http.StatusFound, "/admin/groups")
}

func (s *Server) AdminGroupDetail(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	g, err := s.DB.DeviceGroup.Query().Where(devicegroup.ID(id)).WithDevices().Only(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Group not found")
		c.Redirect(http.StatusFound, "/admin/groups")
		return
	}
	playlists, _ := s.DB.Playlist.Query().All(s.Ctx)
	devices, _ := s.DB.DeviceSettings.Query().All(s.Ctx)
	// available devices to add (ungrouped or other group)
	s.renderPage(c, http.StatusOK, "group_detail.html", gin.H{"group": g, "members": g.Edges.Devices, "playlists": playlists, "devices": devices})
}

func (s *Server) AdminGroupUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := strings.TrimSpace(c.PostForm("name"))
	desc := strings.TrimSpace(c.PostForm("description"))
	contentMode := strings.TrimSpace(c.PostForm("content_mode"))
	playlistRaw := strings.TrimSpace(c.PostForm("playlist_id"))
	schedRaw := strings.TrimSpace(c.PostForm("scheduled_playlist_ids"))
	fallbackRaw := strings.TrimSpace(c.PostForm("fallback_playlist_id"))
	if name == "" || len(name) > 64 {
		SetFlash(c, "danger", "Name must be 1-64 chars")
		c.Redirect(http.StatusFound, "/admin/groups/"+c.Param("id"))
		return
	}
	if len(desc) > 256 {
		SetFlash(c, "danger", "Description max 256 chars")
		c.Redirect(http.StatusFound, "/admin/groups/"+c.Param("id"))
		return
	}
	all, _ := s.DB.DeviceGroup.Query().All(s.Ctx)
	for _, g := range all {
		if g.ID != id && strings.EqualFold(g.Name, name) {
			SetFlash(c, "danger", "Group name already exists")
			c.Redirect(http.StatusFound, "/admin/groups/"+c.Param("id"))
			return
		}
	}
	mode, pid, sched, fallback, ok := validateGroupContent(contentMode, playlistRaw, schedRaw, fallbackRaw, c, s)
	if !ok {
		SetFlash(c, "danger", "Invalid group content assignment")
		c.Redirect(http.StatusFound, "/admin/groups/"+c.Param("id"))
		return
	}
	upd := s.DB.DeviceGroup.UpdateOneID(id).SetName(name).SetDescription(desc).SetContentMode(mode).SetScheduledPlaylistIds(sched)
	if pid != nil {
		upd.SetPlaylistID(*pid)
	} else {
		upd.ClearPlaylistID()
	}
	if fallback != nil {
		upd.SetFallbackPlaylistID(*fallback)
	} else {
		upd.ClearFallbackPlaylistID()
	}
	upd.Exec(s.Ctx)
	SetFlash(c, "success", "Group updated")
	c.Redirect(http.StatusFound, "/admin/groups")
}

// APIGroupCreate handles JSON creation for tests / API
func (s *Server) APIGroupCreate(c *gin.Context) {
	var req struct {
		Name                 string `json:"name"`
		Description          string `json:"description"`
		ContentMode          string `json:"content_mode"`
		PlaylistID           *int   `json:"playlist_id"`
		ScheduledPlaylistIds []int  `json:"scheduled_playlist_ids"`
		FallbackPlaylistID   *int   `json:"fallback_playlist_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// fallback to form
		req.Name = strings.TrimSpace(c.PostForm("name"))
		req.Description = strings.TrimSpace(c.PostForm("description"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-64 chars"})
		return
	}
	if len(req.Description) > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description max 256"})
		return
	}
	existing, _ := s.DB.DeviceGroup.Query().All(s.Ctx)
	for _, g := range existing {
		if strings.EqualFold(g.Name, name) {
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate name"})
			return
		}
	}
	if len(existing) >= MaxGroups {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group cap exceeded"})
		return
	}
	mode := req.ContentMode
	if mode == "" {
		mode = "global"
	}
	if mode != "global" && mode != "playlist" && mode != "scheduled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content_mode"})
		return
	}
	schedJSON := "[]"
	if req.ScheduledPlaylistIds != nil {
		if err := ValidateScheduledCandidates(req.ScheduledPlaylistIds); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		b, _ := json.Marshal(req.ScheduledPlaylistIds)
		schedJSON = string(b)
	}
	builder := s.DB.DeviceGroup.Create().SetName(name).SetDescription(req.Description).SetCreatedAt(time.Now()).SetContentMode(mode).SetScheduledPlaylistIds(schedJSON)
	if req.PlaylistID != nil && mode == "playlist" {
		builder.SetPlaylistID(*req.PlaylistID)
	}
	if req.FallbackPlaylistID != nil && mode == "scheduled" {
		builder.SetFallbackPlaylistID(*req.FallbackPlaylistID)
	}
	g := builder.SaveX(s.Ctx)
	c.JSON(http.StatusCreated, g)
}

func (s *Server) APIGroupList(c *gin.Context) {
	groups, _ := s.DB.DeviceGroup.Query().WithDevices().All(s.Ctx)
	c.JSON(http.StatusOK, groups)
}

func (s *Server) APIGroupDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	g, err := s.DB.DeviceGroup.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// SetNull handled by ent via FK, but also explicitly clear
	members, _ := s.DB.DeviceSettings.Query().Where(devicesettings.GroupIDEQ(g.ID)).All(s.Ctx)
	for _, m := range members {
		s.DB.DeviceSettings.UpdateOneID(m.ID).ClearGroup().Exec(s.Ctx)
	}
	s.DB.DeviceGroup.DeleteOneID(id).Exec(s.Ctx)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) APIGroupAddMember(c *gin.Context) {
	gid, _ := strconv.Atoi(c.Param("id"))
	if _, err := s.DB.DeviceGroup.Get(s.Ctx, gid); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	var req struct {
		DeviceID int `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceID == 0 {
		// try form
		if v := c.PostForm("device_id"); v != "" {
			req.DeviceID, _ = strconv.Atoi(v)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "device_id required"})
			return
		}
	}
	dev, err := s.DB.DeviceSettings.Get(s.Ctx, req.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	// cap check
	cnt, _ := s.DB.DeviceSettings.Query().Where(devicesettings.GroupIDEQ(gid)).Count(s.Ctx)
	if cnt >= MaxDevicesPerGroup && (dev.GroupID == nil || *dev.GroupID != gid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group member cap exceeded"})
		return
	}
	s.DB.DeviceSettings.UpdateOneID(dev.ID).SetGroupID(gid).Exec(s.Ctx)
	c.JSON(http.StatusOK, gin.H{"status": "added"})
}

func (s *Server) APIGroupRemoveMember(c *gin.Context) {
	gid, _ := strconv.Atoi(c.Param("id"))
	did, _ := strconv.Atoi(c.Param("deviceId"))
	dev, err := s.DB.DeviceSettings.Get(s.Ctx, did)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	if dev.GroupID == nil || *dev.GroupID != gid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device not in group"})
		return
	}
	s.DB.DeviceSettings.UpdateOneID(did).ClearGroup().Exec(s.Ctx)
	c.JSON(http.StatusOK, gin.H{"status": "removed"})
}
