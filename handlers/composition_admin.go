package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent/generalsettings"
)

// ---------------------------------------------------------------------------
// Compositions
// ---------------------------------------------------------------------------

func (s *Server) AdminCompositionList(c *gin.Context) {
	comps, err := s.DB.Composition.Query().All(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to load compositions")
		s.renderPage(c, http.StatusOK, "compositions.html", gin.H{"compositions": []any{}})
		return
	}
	s.renderPage(c, http.StatusOK, "compositions.html", gin.H{
		"compositions": comps,
		"count":        len(comps),
	})
}

func (s *Server) AdminCompositionNew(c *gin.Context) {
	opts := s.bindingOptions(c)
	s.renderPage(c, http.StatusOK, "composition_form.html", gin.H{
		"type":              "Composition",
		"mode":              "grid",
		"rows":              2,
		"cols":              2,
		"gap":               2,
		"padding":           0,
		"background":        "#282a36",
		"enabled":           true,
		"regions":           "[]",
		"binding_opts":      opts,
		"binding_opts_json": bindingOptionsJSON(opts),
	})
}

func (s *Server) AdminCompositionCreate(c *gin.Context) {
	name, mode, rows, cols, gap, padding, background, regions, enabled, ttl := compositionFormValues(c)

	v := NewValidator().Required("Name", name).RangeInt("Rows", rows, 1, 8).RangeInt("Cols", cols, 1, 8)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/compositions/new")
		return
	}
	if !datasource.ValidRegions(regions, rows, cols) {
		SetFlash(c, "danger", "Regions are invalid or reference cells outside the grid")
		c.Redirect(http.StatusFound, "/admin/compositions/new")
		return
	}

	obj, err := s.DB.Composition.Create().
		SetName(name).SetMode(mode).SetRows(rows).SetCols(cols).SetGap(gap).
		SetPadding(padding).SetBackground(background).SetEnabled(enabled).
		SetRegions(regions).SetTTLSeconds(ttl).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create composition: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/compositions/new")
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddCompositions(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Composition created")
	c.Redirect(http.StatusFound, "/admin/compositions")
}

func (s *Server) AdminCompositionEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Composition.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Composition not found")
		c.Redirect(http.StatusFound, "/admin/compositions")
		return
	}
	opts := s.bindingOptions(c)
	s.renderPage(c, http.StatusOK, "composition_form.html", gin.H{
		"type":              "Composition",
		"obj":               obj,
		"edit":              true,
		"mode":              obj.Mode,
		"rows":              obj.Rows,
		"cols":              obj.Cols,
		"gap":               obj.Gap,
		"padding":           obj.Padding,
		"background":        obj.Background,
		"enabled":           obj.Enabled,
		"regions":           obj.Regions,
		"ttl_seconds":       obj.TTLSeconds,
		"binding_opts":      opts,
		"binding_opts_json": bindingOptionsJSON(opts),
	})
}

func (s *Server) AdminCompositionUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name, mode, rows, cols, gap, padding, background, regions, enabled, ttl := compositionFormValues(c)

	v := NewValidator().Required("Name", name).RangeInt("Rows", rows, 1, 8).RangeInt("Cols", cols, 1, 8)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/compositions/"+c.Param("id")+"/edit")
		return
	}
	if !datasource.ValidRegions(regions, rows, cols) {
		SetFlash(c, "danger", "Regions are invalid or reference cells outside the grid")
		c.Redirect(http.StatusFound, "/admin/compositions/"+c.Param("id")+"/edit")
		return
	}

	if err := s.DB.Composition.UpdateOneID(id).
		SetName(name).SetMode(mode).SetRows(rows).SetCols(cols).SetGap(gap).
		SetPadding(padding).SetBackground(background).SetEnabled(enabled).
		SetRegions(regions).SetTTLSeconds(ttl).
		Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update composition: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/compositions/"+c.Param("id")+"/edit")
		return
	}
	SetFlash(c, "success", "Composition updated")
	c.Redirect(http.StatusFound, "/admin/compositions")
}

func (s *Server) AdminCompositionDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Composition.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete composition")
	} else {
		SetFlash(c, "success", "Composition deleted")
	}
	c.Redirect(http.StatusFound, "/admin/compositions")
}

// compositionFormValues reads the shared create/update form fields.
func compositionFormValues(c *gin.Context) (name, mode string, rows, cols, gap, padding int, background, regions string, enabled bool, ttl int) {
	name = c.PostForm("name")
	mode = c.DefaultPostForm("mode", "grid")
	rows, _ = strconv.Atoi(c.DefaultPostForm("rows", "2"))
	cols, _ = strconv.Atoi(c.DefaultPostForm("cols", "2"))
	gap, _ = strconv.Atoi(c.DefaultPostForm("gap", "2"))
	padding, _ = strconv.Atoi(c.DefaultPostForm("padding", "0"))
	background = c.DefaultPostForm("background", "#282a36")
	regions = c.PostForm("regions")
	if regions == "" {
		regions = "[]"
	}
	enabled = c.PostForm("enabled") == "on"
	ttl, _ = strconv.Atoi(c.DefaultPostForm("ttl_seconds", "0"))
	return
}

// AdminCompositionPreview renders a composition from unsaved editor form values
// (live preview), resolving region sources against the configured sources.
func (s *Server) AdminCompositionPreview(c *gin.Context) {
	name, mode, rows, cols, gap, padding, background, regions, _, _ := compositionFormValues(c)
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	if rows > 8 {
		rows = 8
	}
	if cols > 8 {
		cols = 8
	}
	w := clampPreviewSize(mustAtoi(c.DefaultPostForm("w", "192")))
	h := clampPreviewSize(mustAtoi(c.DefaultPostForm("h", "192")))

	settings, err := s.loadSettingsWithAll(c)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	cds := &datasource.CompositorDS{
		Name:       name,
		Mode:       mode,
		Background: background,
		Rows:       rows,
		Cols:       cols,
		Gap:        gap,
		Padding:    padding,
		Regions:    datasource.ParseRegions(regions),
	}
	cds.Resolve = buildSourceIndex(settings, s.aiConfig(c.Request.Context())).Resolve
	img, err := cds.GetPNG(w, h)
	if err != nil || img == nil {
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", img.Data)
}
