package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
	"ledit/render"
)

// minRegionSize is the smallest editable region edge in pixels.
const minRegionSize = 2

// layoutDevice is the device shape the editor uses for canvas size and bezels.
type layoutDevice struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Refresh   int    `json:"refresh"`
	PanelCols int    `json:"panel_cols"`
	PanelGap  int    `json:"panel_gap"`
}

// layoutDevices lists configured devices (canvas targets) for the editor.
func (s *Server) layoutDevices() []layoutDevice {
	devs, err := s.DB.DeviceSettings.Query().Order(ent.Asc(devicesettings.FieldID)).All(s.Ctx)
	if err != nil {
		return nil
	}
	out := make([]layoutDevice, 0, len(devs))
	for _, d := range devs {
		out = append(out, layoutDevice{
			ID: d.ID, Name: d.Name, Width: d.Width, Height: d.Height,
			Refresh: d.RefreshInterval, PanelCols: d.PanelCols, PanelGap: d.PanelGap,
		})
	}
	return out
}

// devicesJSON serializes devices for the editor's inline script.
func devicesJSON(devices []layoutDevice) template.JS {
	if devices == nil {
		devices = []layoutDevice{}
	}
	b, err := json.Marshal(devices)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(b)
}

// validateRegionConfig is the editor's trust boundary: it rejects malformed
// region JSON and rectangles outside the target canvas.
func validateRegionConfig(raw string, canvasW, canvasH, rows, cols, gap, padding int) error {
	var regions []datasource.Region
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &regions); err != nil {
			return fmt.Errorf("regions must be a JSON array: %v", err)
		}
	}
	if gap < 0 || gap > 64 {
		return fmt.Errorf("gap must be between 0 and 64")
	}
	if padding < 0 || padding > 64 {
		return fmt.Errorf("padding must be between 0 and 64")
	}
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	for i, r := range regions {
		label := r.ID
		if label == "" {
			label = fmt.Sprintf("#%d", i+1)
		}
		if r.Inset < 0 || r.Inset > 64 {
			return fmt.Errorf("region %s: inset must be between 0 and 64", label)
		}
		if r.Theme != nil && !r.Theme.Validate() {
			return fmt.Errorf("region %s: invalid theme colors", label)
		}
		if r.SourceType != "" && !datasource.IsValidSourceType(r.SourceType) {
			return fmt.Errorf("region %s: unknown source type %q", label, r.SourceType)
		}
		if r.W > 0 || r.H > 0 {
			if r.W < minRegionSize || r.H < minRegionSize {
				return fmt.Errorf("region %s: width and height must be at least %dpx", label, minRegionSize)
			}
			if r.X < 0 || r.Y < 0 {
				return fmt.Errorf("region %s: position must not be negative", label)
			}
			if canvasW > 0 && canvasH > 0 && (r.X+r.W > canvasW || r.Y+r.H > canvasH) {
				return fmt.Errorf("region %s: %dx%d at %d,%d exceeds the %dx%d canvas", label, r.W, r.H, r.X, r.Y, canvasW, canvasH)
			}
			continue
		}
		if r.Row < 0 || r.Row >= rows || r.Col < 0 || r.Col >= cols {
			return fmt.Errorf("region %s: grid cell %d,%d is outside the %dx%d grid", label, r.Row, r.Col, rows, cols)
		}
		rs, cs := r.RowSpan, r.ColSpan
		if rs <= 0 {
			rs = 1
		}
		if cs <= 0 {
			cs = 1
		}
		if r.Row+rs > rows || r.Col+cs > cols {
			return fmt.Errorf("region %s: span exceeds the %dx%d grid", label, rows, cols)
		}
	}
	return nil
}

// regionsToAbsolute rewrites leftover grid regions into explicit rectangles so
// a grid-authored composition opens sanely in the absolute-only editor.
func regionsToAbsolute(regions []datasource.Region, rows, cols, gap, padding, canvasW, canvasH int) []datasource.Region {
	if canvasW < 1 || canvasH < 1 {
		return regions
	}
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	contentW, contentH := canvasW-2*padding, canvasH-2*padding
	if contentW < 1 || contentH < 1 {
		return regions
	}
	out := make([]datasource.Region, len(regions))
	copy(out, regions)
	cellW, cellH := render.CellSize(rows, cols, gap, contentW, contentH)
	for i := range out {
		r := &out[i]
		if r.W > 0 || r.H > 0 {
			continue
		}
		if r.Row < 0 || r.Row >= rows || r.Col < 0 || r.Col >= cols {
			continue
		}
		rs, cs := r.RowSpan, r.ColSpan
		if rs <= 0 {
			rs = 1
		}
		if cs <= 0 {
			cs = 1
		}
		ox, oy := render.PanelOrigin(r.Row, r.Col, gap, cellW, cellH)
		r.X = padding + ox
		r.Y = padding + oy
		r.W = cs*cellW + (cs-1)*gap
		r.H = rs*cellH + (rs-1)*gap
		r.Row, r.Col, r.RowSpan, r.ColSpan = 0, 0, 0, 0
	}
	return out
}

// layoutEditorData assembles the template context for the editor shell.
func (s *Server) layoutEditorData(c *gin.Context, obj *ent.Composition) gin.H {
	opts := s.bindingOptions(c)
	devices := s.layoutDevices()
	canvasW, canvasH := 64, 64
	defaultDeviceID := 0
	if len(devices) > 0 {
		canvasW, canvasH = devices[0].Width, devices[0].Height
		defaultDeviceID = devices[0].ID
	}
	data := gin.H{
		"mode":              "absolute",
		"rows":              2,
		"cols":              2,
		"gap":               2,
		"padding":           0,
		"background":        "#282a36",
		"enabled":           true,
		"ttl_seconds":       0,
		"regions":           "[]",
		"canvas_w":          canvasW,
		"canvas_h":          canvasH,
		"min_region":        minRegionSize,
		"default_device_id": defaultDeviceID,
		"binding_opts":      opts,
		"binding_opts_json": bindingOptionsJSON(opts),
		"devices":           devices,
		"devices_json":      devicesJSON(devices),
		"active":            "layouts",
	}
	if obj != nil {
		data["obj"] = obj
		data["edit"] = true
		data["name"] = obj.Name
		data["rows"] = obj.Rows
		data["cols"] = obj.Cols
		data["gap"] = obj.Gap
		data["padding"] = obj.Padding
		data["background"] = obj.Background
		data["enabled"] = obj.Enabled
		data["ttl_seconds"] = obj.TTLSeconds
		data["regions"] = datasource.RegionsJSON(regionsToAbsolute(
			datasource.ParseRegions(obj.Regions), obj.Rows, obj.Cols, obj.Gap, obj.Padding, canvasW, canvasH))
	}
	return data
}

// ---------------------------------------------------------------------------
// Visual layout editor
// ---------------------------------------------------------------------------

func (s *Server) AdminLayoutList(c *gin.Context) {
	layouts, err := s.DB.Composition.Query().All(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to load layouts")
		s.renderPage(c, http.StatusOK, "layouts.html", gin.H{"layouts": []any{}, "count": 0, "active": "layouts"})
		return
	}
	s.renderPage(c, http.StatusOK, "layouts.html", gin.H{
		"layouts": layouts,
		"count":   len(layouts),
		"active":  "layouts",
	})
}

func (s *Server) AdminLayoutNew(c *gin.Context) {
	data := s.layoutEditorData(c, nil)
	data["form_action"] = "/admin/layouts/new"
	s.renderPage(c, http.StatusOK, "layout_editor.html", data)
}

func (s *Server) AdminLayoutCreate(c *gin.Context) {
	name, _, rows, cols, gap, padding, background, regions, enabled, ttl := compositionFormValues(c)
	canvasW, canvasH := mustAtoi(c.PostForm("canvas_w")), mustAtoi(c.PostForm("canvas_h"))

	v := NewValidator().Required("Name", name).RangeInt("Rows", rows, 1, 8).RangeInt("Cols", cols, 1, 8)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/layouts/new")
		return
	}
	if err := validateRegionConfig(regions, canvasW, canvasH, rows, cols, gap, padding); err != nil {
		SetFlash(c, "danger", err.Error())
		c.Redirect(http.StatusFound, "/admin/layouts/new")
		return
	}

	obj, err := s.DB.Composition.Create().
		SetName(name).SetMode("absolute").SetRows(rows).SetCols(cols).SetGap(gap).
		SetPadding(padding).SetBackground(background).SetEnabled(enabled).
		SetRegions(regions).SetTTLSeconds(ttl).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create layout: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/layouts/new")
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddCompositions(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Layout created")
	c.Redirect(http.StatusFound, fmt.Sprintf("/admin/layouts/%d/edit", obj.ID))
}

func (s *Server) AdminLayoutEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Composition.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Layout not found")
		c.Redirect(http.StatusFound, "/admin/layouts")
		return
	}
	data := s.layoutEditorData(c, obj)
	data["form_action"] = fmt.Sprintf("/admin/layouts/%d/edit", obj.ID)
	s.renderPage(c, http.StatusOK, "layout_editor.html", data)
}

func (s *Server) AdminLayoutUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name, _, rows, cols, gap, padding, background, regions, enabled, ttl := compositionFormValues(c)
	canvasW, canvasH := mustAtoi(c.PostForm("canvas_w")), mustAtoi(c.PostForm("canvas_h"))
	back := fmt.Sprintf("/admin/layouts/%d/edit", id)

	v := NewValidator().Required("Name", name).RangeInt("Rows", rows, 1, 8).RangeInt("Cols", cols, 1, 8)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, back)
		return
	}
	if err := validateRegionConfig(regions, canvasW, canvasH, rows, cols, gap, padding); err != nil {
		SetFlash(c, "danger", err.Error())
		c.Redirect(http.StatusFound, back)
		return
	}

	if err := s.DB.Composition.UpdateOneID(id).
		SetName(name).SetMode("absolute").SetRows(rows).SetCols(cols).SetGap(gap).
		SetPadding(padding).SetBackground(background).SetEnabled(enabled).
		SetRegions(regions).SetTTLSeconds(ttl).
		Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update layout: "+err.Error())
		c.Redirect(http.StatusFound, back)
		return
	}
	SetFlash(c, "success", "Layout saved")
	c.Redirect(http.StatusFound, "/admin/layouts")
}

func (s *Server) AdminLayoutDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Composition.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete layout")
	} else {
		SetFlash(c, "success", "Layout deleted")
	}
	c.Redirect(http.StatusFound, "/admin/layouts")
}

// AdminLayoutPreview renders unsaved editor form values with canvas-bound
// validation, returning 400 for a malformed or out-of-bounds config.
func (s *Server) AdminLayoutPreview(c *gin.Context) {
	name, _, rows, cols, gap, padding, background, regions, _, _ := compositionFormValues(c)
	canvasW, canvasH := mustAtoi(c.PostForm("canvas_w")), mustAtoi(c.PostForm("canvas_h"))
	if err := validateRegionConfig(regions, canvasW, canvasH, rows, cols, gap, padding); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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
		Mode:       "absolute",
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
