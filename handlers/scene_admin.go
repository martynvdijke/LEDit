package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/playlist"
	"ledit/ent/scene"
)

// ---------------------------------------------------------------------------
// Admin view models
// ---------------------------------------------------------------------------

type sceneCondView struct {
	EntityID string
	Operator string
	Value    string
	Status   string // "true" | "false" | "unknown"
	State    string
}

type sceneGroupView struct {
	Op         string
	Conditions []sceneCondView
}

type sceneListView struct {
	*ent.Scene
	Groups  []sceneGroupView
	Active  bool
	Action  string
	TTLText string
}

// sceneConditionStatus classifies one condition against the cached states.
func sceneConditionStatus(states map[string]string, cond SceneCondition) (status, state string) {
	st, present := states[cond.EntityID]
	if !present {
		return "unknown", ""
	}
	switch st {
	case "", "unknown", "unavailable":
		return "unknown", st
	}
	if EvaluateCondition(st, cond.Operator, cond.Value) {
		return "true", st
	}
	return "false", st
}

func sceneActionSummary(a SceneActions) string {
	var parts []string
	if a.SourceType != "" {
		parts = append(parts, fmt.Sprintf("pin %s:%d", a.SourceType, a.SourceID))
	}
	if a.PlaylistID != nil {
		parts = append(parts, fmt.Sprintf("playlist #%d", *a.PlaylistID))
	}
	if a.BrightnessLevel != nil {
		parts = append(parts, fmt.Sprintf("brightness %d", *a.BrightnessLevel))
	}
	if a.OverlayText != "" {
		parts = append(parts, "overlay "+strconv.Quote(a.OverlayText))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func sceneTTLText(ttl *int) string {
	if ttl == nil {
		return "—"
	}
	return strconv.Itoa(*ttl) + "s"
}

// sceneFormBase supplies the shared picker data for the form template.
func (s *Server) sceneFormBase(c *gin.Context) gin.H {
	opts := s.bindingOptions(c)
	pls, _ := s.DB.Playlist.Query().Order(ent.Asc(playlist.FieldID)).All(c.Request.Context())
	return gin.H{
		"options":      opts,
		"options_json": bindingOptionsJSON(opts),
		"playlists":    pls,
		"entity_ids":   ambientEntityIDs(),
	}
}

func sceneFormVars(name string, enabled bool, priority int, ttl, triggers, srcType string, srcID int, playlistID, brightness, overlay, controlsRaw string) gin.H {
	return gin.H{
		"fName":       name,
		"fEnabled":    enabled,
		"fPriority":   strconv.Itoa(priority),
		"fTTL":        ttl,
		"fTriggers":   triggers,
		"fSrcType":    srcType,
		"fSrcID":      strconv.Itoa(srcID),
		"fPlaylistID": playlistID,
		"fBrightness": brightness,
		"fOverlay":    overlay,
		"fControls":   controlsRaw,
	}
}

// ---------------------------------------------------------------------------
// Admin CRUD
// ---------------------------------------------------------------------------

func (s *Server) AdminSceneList(c *gin.Context) {
	rows, err := s.DB.Scene.Query().Order(ent.Asc(scene.FieldID)).All(s.Ctx)
	if err != nil {
		rows = []*ent.Scene{}
	}
	states := ambientStatesSnapshot()
	views := make([]sceneListView, 0, len(rows))
	for _, r := range rows {
		triggers, _ := parseSceneTriggers(r.Triggers)
		actions, _ := parseSceneActions(r.Actions)
		var groups []sceneGroupView
		for _, g := range triggers {
			gv := sceneGroupView{Op: g.Op}
			for _, cond := range g.Conditions {
				status, state := sceneConditionStatus(states, cond)
				gv.Conditions = append(gv.Conditions, sceneCondView{
					EntityID: cond.EntityID, Operator: cond.Operator, Value: cond.Value,
					Status: status, State: state,
				})
			}
			groups = append(groups, gv)
		}
		views = append(views, sceneListView{
			Scene:   r,
			Groups:  groups,
			Active:  globalSceneManager.IsActive(r.ID),
			Action:  sceneActionSummary(actions),
			TTLText: sceneTTLText(r.TTLSeconds),
		})
	}
	s.renderPage(c, http.StatusOK, "scenes.html", gin.H{"scenes": views, "active": "scenes"})
}

func (s *Server) AdminSceneNew(c *gin.Context) {
	vars := s.sceneFormBase(c)
	for k, v := range sceneFormVars("", true, 0, "", "[]", "", 0, "", "", "", "") {
		vars[k] = v
	}
	s.renderPage(c, http.StatusOK, "scene_form.html", vars)
}

// validateSceneForm parses and validates trigger/action/ttl form values.
func (s *Server) validateSceneForm(c *gin.Context, name, triggersRaw, ttlRaw, srcType string, srcID int, playlistRaw, brightnessRaw, overlay, controlsRaw string) ([]TriggerGroup, *int, SceneActions, string) {
	var actions SceneActions
	if strings.TrimSpace(name) == "" {
		return nil, nil, actions, "name is required"
	}
	groups, err := parseSceneTriggers(triggersRaw)
	if err != nil {
		return nil, nil, actions, err.Error()
	}
	for i, g := range groups {
		if g.Op != "all-of" && g.Op != "any-of" {
			return nil, nil, actions, fmt.Sprintf("trigger group %d: op must be all-of or any-of", i)
		}
		if len(g.Conditions) == 0 {
			return nil, nil, actions, fmt.Sprintf("trigger group %d: at least one condition required", i)
		}
		for j, cond := range g.Conditions {
			if strings.TrimSpace(cond.EntityID) == "" {
				return nil, nil, actions, fmt.Sprintf("group %d condition %d: entity_id is required", i, j)
			}
			if !validSceneOperator(cond.Operator) {
				return nil, nil, actions, fmt.Sprintf("group %d condition %d: invalid operator %q", i, j, cond.Operator)
			}
		}
	}
	var ttl *int
	if ttlRaw != "" {
		v, err := strconv.Atoi(ttlRaw)
		if err != nil || v < 0 || v > 86400 {
			return nil, nil, actions, "ttl_seconds must be between 0 and 86400"
		}
		ttl = &v
	}
	if strings.TrimSpace(srcType) != "" {
		opts := s.bindingOptions(c)
		found := false
		if list, ok := opts[srcType]; ok {
			for _, o := range list {
				if o.ID == srcID {
					found = true
					break
				}
			}
		}
		if !found {
			return nil, nil, actions, fmt.Sprintf("source %s:%d not found", srcType, srcID)
		}
		actions.SourceType = srcType
		actions.SourceID = srcID
	}
	if playlistRaw != "" {
		pid, err := strconv.Atoi(playlistRaw)
		if err != nil {
			return nil, nil, actions, "playlist_id must be an integer"
		}
		if _, err := s.DB.Playlist.Get(c.Request.Context(), pid); err != nil {
			return nil, nil, actions, fmt.Sprintf("playlist %d not found", pid)
		}
		actions.PlaylistID = &pid
	}
	if brightnessRaw != "" {
		v, err := strconv.Atoi(brightnessRaw)
		if err != nil || v < 0 || v > 100 {
			return nil, nil, actions, "brightness_level must be between 0 and 100"
		}
		actions.BrightnessLevel = &v
	}
	if len(overlay) > 200 {
		return nil, nil, actions, "overlay_text must be 200 characters or fewer"
	}
	actions.OverlayText = strings.TrimSpace(overlay)
	// controls field: JSON array of SceneControl
	controlsRaw = strings.TrimSpace(controlsRaw)
	if controlsRaw != "" {
		var controls []SceneControl
		if err := json.Unmarshal([]byte(controlsRaw), &controls); err != nil {
			return nil, nil, actions, "controls must be a JSON array"
		}
		if len(controls) > 10 {
			return nil, nil, actions, "too many controls (max 10)"
		}
		for i, ctrl := range controls {
			if strings.TrimSpace(ctrl.SourceType) == "" {
				return nil, nil, actions, fmt.Sprintf("controls[%d]: source_type is required", i)
			}
			if strings.TrimSpace(ctrl.Action) == "" {
				return nil, nil, actions, fmt.Sprintf("controls[%d]: action is required", i)
			}
			opts := s.bindingOptions(c)
			found := false
			if list, ok := opts[ctrl.SourceType]; ok {
				for _, o := range list {
					if o.ID == ctrl.SourceID {
						found = true
						break
					}
				}
			}
			if !found {
				return nil, nil, actions, fmt.Sprintf("controls[%d]: source %s:%d not found", i, ctrl.SourceType, ctrl.SourceID)
			}
		}
		actions.Controls = controls
	}
	return groups, ttl, actions, ""
}

func (s *Server) renderSceneFormError(c *gin.Context, code int, msg string, name string, enabled bool, priority int, ttl, triggers, srcType string, srcID int, playlistRaw, brightness, overlay, controlsRaw string, edit bool, id int) {
	vars := s.sceneFormBase(c)
	for k, v := range sceneFormVars(name, enabled, priority, ttl, triggers, srcType, srcID, playlistRaw, brightness, overlay, controlsRaw) {
		vars[k] = v
	}
	vars["error"] = msg
	if edit {
		vars["edit"] = true
		vars["id"] = id
	}
	s.renderPage(c, code, "scene_form.html", vars)
}

func (s *Server) AdminSceneCreate(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	enabled := c.PostForm("enabled") == "on"
	priority, _ := strconv.Atoi(c.DefaultPostForm("priority", "0"))
	ttlRaw := strings.TrimSpace(c.PostForm("ttl_seconds"))
	triggersRaw := strings.TrimSpace(c.PostForm("triggers"))
	if triggersRaw == "" {
		triggersRaw = "[]"
	}
	srcType := c.PostForm("source_type")
	srcID, _ := strconv.Atoi(c.PostForm("source_id"))
	playlistRaw := strings.TrimSpace(c.PostForm("playlist_id"))
	brightnessRaw := strings.TrimSpace(c.PostForm("brightness_level"))
	overlay := strings.TrimSpace(c.PostForm("overlay_text"))
	controlsRaw := strings.TrimSpace(c.PostForm("controls"))

	groups, ttl, actions, msg := s.validateSceneForm(c, name, triggersRaw, ttlRaw, srcType, srcID, playlistRaw, brightnessRaw, overlay, controlsRaw)
	if msg != "" {
		SetFlash(c, "danger", msg)
		s.renderSceneFormError(c, http.StatusBadRequest, msg, name, enabled, priority, ttlRaw, triggersRaw, srcType, srcID, playlistRaw, brightnessRaw, overlay, controlsRaw, false, 0)
		return
	}
	if groups == nil {
		groups = []TriggerGroup{}
	}
	triggersJSON, _ := json.Marshal(groups)
	actionsJSON, _ := json.Marshal(actions)
	obj, err := s.DB.Scene.Create().
		SetName(name).SetEnabled(enabled).SetPriority(priority).
		SetTriggers(string(triggersJSON)).SetActions(string(actionsJSON)).
		SetNillableTTLSeconds(ttl).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		s.renderSceneFormError(c, http.StatusOK, err.Error(), name, enabled, priority, ttlRaw, triggersRaw, srcType, srcID, playlistRaw, brightnessRaw, overlay, controlsRaw, false, 0)
		return
	}
	if gs, gerr := s.DB.GeneralSettings.Query().Only(s.Ctx); gerr == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddScenes(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Scene created")
	c.Redirect(http.StatusFound, "/admin/scenes")
}

func (s *Server) AdminSceneEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Scene.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Scene not found")
		c.Redirect(http.StatusFound, "/admin/scenes")
		return
	}
	triggers, _ := parseSceneTriggers(obj.Triggers)
	if triggers == nil {
		triggers = []TriggerGroup{}
	}
	triggersJSON, _ := json.Marshal(triggers)
	actions, _ := parseSceneActions(obj.Actions)
	ttl := ""
	if obj.TTLSeconds != nil {
		ttl = strconv.Itoa(*obj.TTLSeconds)
	}
	playlistRaw := ""
	if actions.PlaylistID != nil {
		playlistRaw = strconv.Itoa(*actions.PlaylistID)
	}
	brightness := ""
	if actions.BrightnessLevel != nil {
		brightness = strconv.Itoa(*actions.BrightnessLevel)
	}
	controlsRaw := ""
	if len(actions.Controls) > 0 {
		if b, err := json.Marshal(actions.Controls); err == nil {
			controlsRaw = string(b)
		}
	}
	vars := s.sceneFormBase(c)
	for k, v := range sceneFormVars(obj.Name, obj.Enabled, obj.Priority, ttl, string(triggersJSON), actions.SourceType, actions.SourceID, playlistRaw, brightness, actions.OverlayText, controlsRaw) {
		vars[k] = v
	}
	vars["obj"] = obj
	vars["edit"] = true
	vars["id"] = id
	s.renderPage(c, http.StatusOK, "scene_form.html", vars)
}

func (s *Server) AdminSceneUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := strings.TrimSpace(c.PostForm("name"))
	enabled := c.PostForm("enabled") == "on"
	priority, _ := strconv.Atoi(c.DefaultPostForm("priority", "0"))
	ttlRaw := strings.TrimSpace(c.PostForm("ttl_seconds"))
	triggersRaw := strings.TrimSpace(c.PostForm("triggers"))
	if triggersRaw == "" {
		triggersRaw = "[]"
	}
	srcType := c.PostForm("source_type")
	srcID, _ := strconv.Atoi(c.PostForm("source_id"))
	playlistRaw := strings.TrimSpace(c.PostForm("playlist_id"))
	brightnessRaw := strings.TrimSpace(c.PostForm("brightness_level"))
	overlay := strings.TrimSpace(c.PostForm("overlay_text"))
	controlsRaw := strings.TrimSpace(c.PostForm("controls"))

	groups, ttl, actions, msg := s.validateSceneForm(c, name, triggersRaw, ttlRaw, srcType, srcID, playlistRaw, brightnessRaw, overlay, controlsRaw)
	if msg != "" {
		SetFlash(c, "danger", msg)
		s.renderSceneFormError(c, http.StatusBadRequest, msg, name, enabled, priority, ttlRaw, triggersRaw, srcType, srcID, playlistRaw, brightnessRaw, overlay, controlsRaw, true, id)
		return
	}
	if groups == nil {
		groups = []TriggerGroup{}
	}
	triggersJSON, _ := json.Marshal(groups)
	actionsJSON, _ := json.Marshal(actions)
	upd := s.DB.Scene.UpdateOneID(id).
		SetName(name).SetEnabled(enabled).SetPriority(priority).
		SetTriggers(string(triggersJSON)).SetActions(string(actionsJSON))
	if ttl != nil {
		upd.SetTTLSeconds(*ttl)
	} else {
		upd.ClearTTLSeconds()
	}
	if err := upd.Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		s.renderSceneFormError(c, http.StatusOK, err.Error(), name, enabled, priority, ttlRaw, triggersRaw, srcType, srcID, playlistRaw, brightnessRaw, overlay, controlsRaw, true, id)
		return
	}
	SetFlash(c, "success", "Scene updated")
	c.Redirect(http.StatusFound, "/admin/scenes")
}

func (s *Server) AdminSceneDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Scene.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/scenes")
		return
	}
	SetFlash(c, "success", "Scene deleted")
	c.Redirect(http.StatusFound, "/admin/scenes")
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

// APIScenePreview applies a scene's actions transiently (15s) without touching
// activation/TTL/suppression bookkeeping.
func (s *Server) APIScenePreview(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scene id"})
		return
	}
	row, err := s.DB.Scene.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scene not found"})
		return
	}
	sc, perr := sceneFromEnt(row)
	if perr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": perr.Error()})
		return
	}
	if sc.Actions.SourceType == "" && sc.Actions.PlaylistID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scene has no previewable source or playlist action"})
		return
	}
	resolve := SceneSourceResolver
	if resolve == nil {
		resolve = func(sc *Scene) (*sourceWithName, bool) { return resolveSceneSource(s.DB, sc) }
	}
	if !PreviewScene(sc, resolve, 15*time.Second) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scene action could not be resolved"})
		return
	}
	sceneAll(globalSceneManager.ActiveSource())
	c.JSON(http.StatusOK, gin.H{"status": "ok", "preview_seconds": 15})
}
