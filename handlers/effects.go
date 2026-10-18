package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/effectpreset"
	"ledit/render/effects"
)

// effectEditorData is the JSON blob handed to the editor script so the form can
// re-render the registry descriptors (defaults and bounds) client-side.
func effectEditorData() string {
	type bounds struct {
		Speed     [2]float64 `json:"speed"`
		Intensity [2]float64 `json:"intensity"`
		Density   [2]float64 `json:"density"`
		Direction []string   `json:"direction"`
	}
	type entry struct {
		Name      string  `json:"name"`
		Speed     float64 `json:"speed"`
		Intensity float64 `json:"intensity"`
		Density   float64 `json:"density"`
		Direction string  `json:"direction"`
		Bounds    bounds  `json:"bounds"`
	}
	list := make([]entry, 0)
	for _, d := range effects.Descriptors() {
		list = append(list, entry{
			Name:      d.Name,
			Speed:     d.Defaults.Speed,
			Intensity: d.Defaults.Intensity,
			Density:   d.Defaults.Density,
			Direction: d.Defaults.Direction,
			Bounds: bounds{
				Speed:     d.Bounds.Speed,
				Intensity: d.Bounds.Intensity,
				Density:   d.Bounds.Density,
				Direction: d.Bounds.Directions,
			},
		})
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// effectFormVars builds the editor view for a new or existing preset.
func (s *Server) effectFormVars(p *ent.EffectPreset) gin.H {
	names := effects.Names()
	params := effects.Params{}
	effectName := ""
	if p != nil {
		effectName = p.Effect
		if decoded, err := effects.UnmarshalParams([]byte(p.Params)); err == nil {
			params = decoded.StripPalette()
		}
	}
	if effectName == "" && len(names) > 0 {
		effectName = names[0]
	}
	if desc, _, ok := effects.Lookup(effectName); ok && params.Speed == 0 && params.Intensity == 0 && params.Density == 0 {
		// A brand-new editor starts from the effect's declared defaults.
		params = desc.Defaults.StripPalette()
	}
	return gin.H{
		"active":      "effects",
		"preset":      p,
		"presets":     effectPresetRows(),
		"effectNames": names,
		"editorJSON":  effectEditorData(),
		"fEffect":     effectName,
		"fName":       presetName(p),
		"fSpeed":      formatEffectNumber(params.Speed),
		"fIntensity":  formatEffectNumber(params.Intensity),
		"fDensity":    formatEffectNumber(params.Density),
		"fDirection":  params.Direction,
		"fSeed":       strconv.FormatInt(params.Seed, 10),
		"builtIn":     p != nil && p.BuiltIn,
	}
}

func presetName(p *ent.EffectPreset) string {
	if p == nil {
		return ""
	}
	return p.Name
}

// effectPresetRows lists presets for the effects page and editor sidebar.
func effectPresetRows() []cachedEffectPreset {
	return cachedEffectPresets()
}

// AdminEffectList renders the preset list.
func (s *Server) AdminEffectList(c *gin.Context) {
	items, err := s.DB.EffectPreset.Query().
		Order(ent.Asc(effectpreset.FieldBuiltIn), ent.Asc(effectpreset.FieldName)).All(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to load effect presets")
		items = nil
	}
	s.renderPage(c, 200, "effects.html", gin.H{
		"active":  "effects",
		"presets": items,
	})
}

// AdminEffectNew renders an empty editor.
func (s *Server) AdminEffectNew(c *gin.Context) {
	s.renderPage(c, 200, "effect_form.html", s.effectFormVars(nil))
}

// AdminEffectEditor renders the editor for an existing preset (or "new").
func (s *Server) AdminEffectEditor(c *gin.Context) {
	if c.Param("id") == "new" {
		s.renderPage(c, 200, "effect_form.html", s.effectFormVars(nil))
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid effect preset")
		c.Redirect(302, "/admin/effects")
		return
	}
	p, err := s.DB.EffectPreset.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Effect preset not found")
		c.Redirect(302, "/admin/effects")
		return
	}
	s.renderPage(c, 200, "effect_form.html", s.effectFormVars(p))
}

// effectFormValues reads and validates the submitted preset fields. On failure
// it returns a human-readable message and ok=false; nothing is written.
func effectFormValues(c *gin.Context) (name, effectName string, params effects.Params, msg string, ok bool) {
	name = strings.TrimSpace(c.PostForm("name"))
	effectName = strings.TrimSpace(c.PostForm("effect"))

	desc, _, found := effects.Lookup(effectName)
	if !found {
		return name, effectName, params, "Unknown effect", false
	}
	params = desc.Defaults.StripPalette()
	if v := strings.TrimSpace(c.PostForm("speed")); v != "" {
		params.Speed, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(c.PostForm("intensity")); v != "" {
		params.Intensity, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(c.PostForm("density")); v != "" {
		params.Density, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(c.PostForm("direction")); v != "" {
		params.Direction = v
	}
	if v := strings.TrimSpace(c.PostForm("seed")); v != "" {
		params.Seed, _ = strconv.ParseInt(v, 10, 64)
	}

	switch {
	case name == "":
		return name, effectName, params, "Preset name is required", false
	case len(name) > 64:
		return name, effectName, params, "Preset name must be 64 characters or fewer", false
	}
	if err := effects.Validate(effectName, params); err != nil {
		return name, effectName, params, effectValidationMessage(err), false
	}
	return name, effectName, params, "", true
}

// effectFormError re-renders the editor with the submitted values intact and the
// validation message shown, so a rejected save never loses the operator's input.
func (s *Server) effectFormError(c *gin.Context, id int, name, effectName string, params effects.Params, msg string) {
	var p *ent.EffectPreset
	if id > 0 {
		if row, err := s.DB.EffectPreset.Get(s.Ctx, id); err == nil {
			p = row
		}
	}
	vars := s.effectFormVars(p)
	vars["fName"] = name
	vars["fEffect"] = effectName
	vars["fSpeed"] = formatEffectNumber(params.Speed)
	vars["fIntensity"] = formatEffectNumber(params.Intensity)
	vars["fDensity"] = formatEffectNumber(params.Density)
	vars["fDirection"] = params.Direction
	vars["fSeed"] = strconv.FormatInt(params.Seed, 10)
	vars["error"] = msg
	SetFlash(c, "danger", msg)
	s.renderPage(c, http.StatusOK, "effect_form.html", vars)
}

// effectValidationMessage flattens field errors into one readable sentence.
func effectValidationMessage(err error) string {
	fields := effects.FieldMap(err)
	parts := make([]string, 0, len(fields))
	for _, v := range fields {
		parts = append(parts, v)
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	return err.Error()
}

// formatEffectNumber renders a parameter value without a trailing ".0".
func formatEffectNumber(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// AdminEffectSave creates or updates a preset.
func (s *Server) AdminEffectSave(c *gin.Context) {
	id := 0
	if c.Param("id") != "new" {
		id, _ = strconv.Atoi(c.Param("id"))
	}

	// Built-in presets are read-only; refuse before touching the payload.
	if id > 0 {
		existing, err := s.DB.EffectPreset.Get(s.Ctx, id)
		if err != nil {
			SetFlash(c, "danger", "Effect preset not found")
			c.Redirect(302, "/admin/effects")
			return
		}
		if existing.BuiltIn {
			SetFlash(c, "danger", "Built-in effect presets cannot be edited")
			c.Redirect(302, "/admin/effects")
			return
		}
	}

	name, effectName, params, msg, ok := effectFormValues(c)
	if !ok {
		s.effectFormError(c, id, name, effectName, params, msg)
		return
	}
	taken, err := effectPresetNameTaken(s, name, id)
	if err != nil {
		SetFlash(c, "danger", "Failed to check preset name")
		c.Redirect(302, "/admin/effects")
		return
	}
	if taken {
		s.effectFormError(c, id, name, effectName, params, "A preset with that name already exists")
		return
	}

	raw, err := effects.MarshalParams(params.StripPalette())
	if err != nil {
		s.effectFormError(c, id, name, effectName, params, "Failed to serialize parameters")
		return
	}

	if id > 0 {
		if _, err := s.DB.EffectPreset.UpdateOneID(id).
			SetName(name).SetEffect(effectName).SetParams(string(raw)).Save(s.Ctx); err != nil {
			SetFlash(c, "danger", "Failed to save preset: "+err.Error())
			c.Redirect(302, fmt.Sprintf("/admin/effects/%d/edit", id))
			return
		}
		SetFlash(c, "success", "Effect preset saved")
	} else {
		if _, err := s.DB.EffectPreset.Create().
			SetName(name).SetEffect(effectName).SetParams(string(raw)).Save(s.Ctx); err != nil {
			SetFlash(c, "danger", "Failed to create preset: "+err.Error())
			c.Redirect(302, "/admin/effects/new")
			return
		}
		SetFlash(c, "success", "Effect preset created")
	}
	reloadEffectPresetCache(s.DB)
	c.Redirect(302, "/admin/effects")
}

// effectPresetNameTaken reports whether another preset already uses the name.
func effectPresetNameTaken(s *Server, name string, exceptID int) (bool, error) {
	q := s.DB.EffectPreset.Query().Where(effectpreset.NameEqualFold(name))
	if exceptID > 0 {
		q = q.Where(effectpreset.IDNEQ(exceptID))
	}
	return q.Exist(s.Ctx)
}

// AdminEffectDuplicate copies a preset into an editable, renamed preset.
func (s *Server) AdminEffectDuplicate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid effect preset")
		c.Redirect(302, "/admin/effects")
		return
	}
	src, err := s.DB.EffectPreset.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Effect preset not found")
		c.Redirect(302, "/admin/effects")
		return
	}
	name := src.Name + " copy"
	for i := 2; ; i++ {
		exists, _ := s.DB.EffectPreset.Query().Where(effectpreset.NameEQ(name)).Exist(s.Ctx)
		if !exists {
			break
		}
		name = fmt.Sprintf("%s copy %d", src.Name, i)
	}
	created, err := s.DB.EffectPreset.Create().
		SetName(name).SetEffect(src.Effect).SetParams(src.Params).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to duplicate preset")
		c.Redirect(302, "/admin/effects")
		return
	}
	reloadEffectPresetCache(s.DB)
	SetFlash(c, "success", "Effect preset duplicated")
	c.Redirect(302, fmt.Sprintf("/admin/effects/%d/edit", created.ID))
}

// AdminEffectDelete removes a user preset. Any sources still referencing the
// preset degrade through the existing unresolved-source tolerance.
func (s *Server) AdminEffectDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid effect preset")
		c.Redirect(302, "/admin/effects")
		return
	}
	p, err := s.DB.EffectPreset.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Effect preset not found")
		c.Redirect(302, "/admin/effects")
		return
	}
	if p.BuiltIn {
		SetFlash(c, "danger", "Built-in effect presets cannot be deleted")
		c.Redirect(302, "/admin/effects")
		return
	}
	if err := s.DB.EffectPreset.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete preset")
		c.Redirect(302, "/admin/effects")
		return
	}
	reloadEffectPresetCache(s.DB)
	SetFlash(c, "success", "Effect preset deleted")
	c.Redirect(302, "/admin/effects")
}

// AdminPreviewEffect renders an unsaved preset definition without touching the
// database, mirroring AdminPreviewDatasource. The palette comes from the theme
// override supplied by the editor (or the preset's effective theme), so editing
// theme tokens previews immediately.
func (s *Server) AdminPreviewEffect(c *gin.Context) {
	effectName := strings.TrimSpace(c.PostForm("effect"))
	desc, _, ok := effects.Lookup(effectName)
	if !ok {
		c.Status(http.StatusBadRequest)
		return
	}
	params := desc.Defaults.StripPalette()
	if v := strings.TrimSpace(c.PostForm("speed")); v != "" {
		params.Speed, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(c.PostForm("intensity")); v != "" {
		params.Intensity, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(c.PostForm("density")); v != "" {
		params.Density, _ = strconv.ParseFloat(v, 64)
	}
	if v := strings.TrimSpace(c.PostForm("direction")); v != "" {
		params.Direction = v
	}
	if v := strings.TrimSpace(c.PostForm("seed")); v != "" {
		params.Seed, _ = strconv.ParseInt(v, 10, 64)
	}

	w := clampPreviewSize(mustAtoi(c.DefaultPostForm("w", "64")))
	h := clampPreviewSize(mustAtoi(c.DefaultPostForm("h", "64")))

	id, _ := strconv.Atoi(c.PostForm("id"))
	src := &datasource.EffectDS{PresetID: id, Effect: effectName, Params: params}

	theme, hasOverride := s.themeOverrideFrom(c)
	if !hasOverride {
		theme = ResolveTheme("effect", id)
	}
	img, err := datasource.RenderThemed(src, w, h, theme)
	if err != nil || img == nil {
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "no-store")
	s.writeImageResponse(c, img)
}
