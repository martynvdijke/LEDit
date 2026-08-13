package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent/generalsettings"
)

// bindingOptionsJSON serializes the per-cell source options for the matrix
// editor's client-side selectors.
func bindingOptionsJSON(opts map[string][]bindingOption) template.JS {
	raw, err := json.Marshal(opts)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(raw)
}

// ---------------------------------------------------------------------------
// Matrix layouts
// ---------------------------------------------------------------------------

func (s *Server) AdminMatrixLayoutList(c *gin.Context) {
	layouts, err := s.DB.MatrixLayout.Query().All(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to load matrix layouts")
		s.renderPage(c, http.StatusOK, "matrix_layouts.html", gin.H{"layouts": []any{}})
		return
	}
	s.renderPage(c, http.StatusOK, "matrix_layouts.html", gin.H{
		"layouts": layouts,
		"count":   len(layouts),
	})
}

func (s *Server) AdminMatrixLayoutNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "matrix_layout_form.html", gin.H{
		"type":              "Matrix Layout",
		"rows":              2,
		"cols":              2,
		"gap":               2,
		"background":        "#282a36",
		"enabled":           true,
		"bindings":          "[]",
		"binding_opts":      s.bindingOptions(c),
		"binding_opts_json": bindingOptionsJSON(s.bindingOptions(c)),
	})
}

func (s *Server) AdminMatrixLayoutCreate(c *gin.Context) {
	name := c.PostForm("name")
	rows, _ := strconv.Atoi(c.DefaultPostForm("rows", "2"))
	cols, _ := strconv.Atoi(c.DefaultPostForm("cols", "2"))
	gap, _ := strconv.Atoi(c.DefaultPostForm("gap", "2"))
	background := c.DefaultPostForm("background", "#282a36")
	enabled := c.PostForm("enabled") == "on"
	bindings := c.PostForm("bindings")
	if bindings == "" {
		bindings = "[]"
	}

	v := NewValidator().Required("Name", name).RangeInt("Rows", rows, 1, 8).RangeInt("Cols", cols, 1, 8)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/matrixlayouts/new")
		return
	}
	if !datasource.ValidBindings(bindings, rows, cols) {
		SetFlash(c, "danger", "Bindings are invalid or reference cells outside the grid")
		c.Redirect(http.StatusFound, "/admin/matrixlayouts/new")
		return
	}

	obj, err := s.DB.MatrixLayout.Create().
		SetName(name).SetRows(rows).SetCols(cols).SetGap(gap).
		SetBackground(background).SetEnabled(enabled).SetBindings(bindings).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create layout: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/matrixlayouts/new")
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddMatrixLayouts(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Matrix layout created")
	c.Redirect(http.StatusFound, "/admin/matrixlayouts")
}

func (s *Server) AdminMatrixLayoutEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.MatrixLayout.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Matrix layout not found")
		c.Redirect(http.StatusFound, "/admin/matrixlayouts")
		return
	}
	opts := s.bindingOptions(c)
	s.renderPage(c, http.StatusOK, "matrix_layout_form.html", gin.H{
		"type":              "Matrix Layout",
		"obj":               obj,
		"edit":              true,
		"rows":              obj.Rows,
		"cols":              obj.Cols,
		"gap":               obj.Gap,
		"background":        obj.Background,
		"enabled":           obj.Enabled,
		"bindings":          obj.Bindings,
		"binding_opts":      opts,
		"binding_opts_json": bindingOptionsJSON(opts),
	})
}

func (s *Server) AdminMatrixLayoutUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	rows, _ := strconv.Atoi(c.DefaultPostForm("rows", "2"))
	cols, _ := strconv.Atoi(c.DefaultPostForm("cols", "2"))
	gap, _ := strconv.Atoi(c.DefaultPostForm("gap", "2"))
	background := c.DefaultPostForm("background", "#282a36")
	enabled := c.PostForm("enabled") == "on"
	bindings := c.PostForm("bindings")
	if bindings == "" {
		bindings = "[]"
	}

	v := NewValidator().Required("Name", name).RangeInt("Rows", rows, 1, 8).RangeInt("Cols", cols, 1, 8)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/matrixlayouts/"+c.Param("id")+"/edit")
		return
	}
	if !datasource.ValidBindings(bindings, rows, cols) {
		SetFlash(c, "danger", "Bindings are invalid or reference cells outside the grid")
		c.Redirect(http.StatusFound, "/admin/matrixlayouts/"+c.Param("id")+"/edit")
		return
	}

	if err := s.DB.MatrixLayout.UpdateOneID(id).
		SetName(name).SetRows(rows).SetCols(cols).SetGap(gap).
		SetBackground(background).SetEnabled(enabled).SetBindings(bindings).
		Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update layout: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/matrixlayouts/"+c.Param("id")+"/edit")
		return
	}
	SetFlash(c, "success", "Matrix layout updated")
	c.Redirect(http.StatusFound, "/admin/matrixlayouts")
}

func (s *Server) AdminMatrixLayoutDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.MatrixLayout.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete layout")
	}
	SetFlash(c, "success", "Matrix layout deleted")
	c.Redirect(http.StatusFound, "/admin/matrixlayouts")
}
