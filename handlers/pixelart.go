package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/pixelart"
	"ledit/render"
)

// validPixelBindings checks bindings parses as JSON object (empty "{}" ok).
func validPixelBindings(raw string) error {
	if raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("bindings is not valid JSON object: %w", err)
	}
	return nil
}

func pixelArtFormFields(c *gin.Context) map[string]string {
	// checkbox "enabled" value is "on" when checked, missing otherwise.
	return map[string]string{
		"name":        c.PostForm("name"),
		"grid_width":  c.PostForm("grid_width"),
		"grid_height": c.PostForm("grid_height"),
		"frames":      c.PostForm("frames"),
		"bindings":    c.PostForm("bindings"),
		"api_url":     c.PostForm("api_url"),
		"api_token":   c.PostForm("api_token"),
		"enabled":     c.PostForm("enabled"),
	}
}

func validatePixelArtForm(f map[string]string) error {
	if f["name"] == "" {
		return fmt.Errorf("name is required")
	}
	gw, err := strconv.Atoi(f["grid_width"])
	if err != nil {
		return fmt.Errorf("grid_width must be an integer")
	}
	gh, err := strconv.Atoi(f["grid_height"])
	if err != nil {
		return fmt.Errorf("grid_height must be an integer")
	}
	if gw < 1 || gw > render.MaxPixelGrid {
		return fmt.Errorf("grid_width must be between 1 and %d", render.MaxPixelGrid)
	}
	if gh < 1 || gh > render.MaxPixelGrid {
		return fmt.Errorf("grid_height must be between 1 and %d", render.MaxPixelGrid)
	}
	frames := f["frames"]
	if frames == "" {
		return fmt.Errorf("frames is required")
	}
	if _, err := render.ParsePixelFrames(frames, gw, gh); err != nil {
		return err
	}
	bindings := f["bindings"]
	if bindings == "" {
		bindings = "{}"
	}
	if err := validPixelBindings(bindings); err != nil {
		return err
	}
	return nil
}

// PixelArtList GET /admin/pixelarts
func (s *Server) PixelArtList(c *gin.Context) {
	arts, err := s.DB.PixelArt.Query().Order(ent.Desc(pixelart.FieldIsDraft), ent.Asc(pixelart.FieldID)).All(s.Ctx)
	if err != nil {
		arts = []*ent.PixelArt{}
	}
	s.renderPage(c, http.StatusOK, "pixelarts.html", gin.H{"pixelarts": arts})
}

// PixelArtNew GET /admin/pixelarts/new
func (s *Server) PixelArtNew(c *gin.Context) {
	_, aiOk := getAIConfig(c.Request.Context(), s.DB)
	s.renderPage(c, http.StatusOK, "pixelart_form.html", gin.H{"aiConfigured": aiOk})
}

// PixelArtCreate POST /admin/pixelarts/new
func (s *Server) PixelArtCreate(c *gin.Context) {
	f := pixelArtFormFields(c)
	if f["bindings"] == "" {
		f["bindings"] = "{}"
	}
	if err := validatePixelArtForm(f); err != nil {
		SetFlash(c, "danger", err.Error())
		s.renderPage(c, http.StatusOK, "pixelart_form.html", gin.H{"obj": f, "error": err.Error()})
		return
	}
	// Ensure frames/bindings defaults are set for CreateFields.
	entry := dsRegistry["pixelart"]
	obj, err := entry.CreateFields(s.DB, s.Ctx, f)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		s.renderPage(c, http.StatusOK, "pixelart_form.html", gin.H{"obj": f})
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Only(s.Ctx); err == nil && gs != nil {
		entry.AddEdge(s.DB.GeneralSettings.UpdateOne(gs), obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Pixel Art created")
	c.Redirect(http.StatusFound, "/admin/pixelarts")
}

// PixelArtEdit GET /admin/pixelarts/:id/edit
func (s *Server) PixelArtEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.PixelArt.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Pixel Art not found")
		c.Redirect(http.StatusFound, "/admin/pixelarts")
		return
	}
	_, aiOk := getAIConfig(c.Request.Context(), s.DB)
	s.renderPage(c, http.StatusOK, "pixelart_form.html", gin.H{"obj": obj, "edit": true, "aiConfigured": aiOk})
}

// PixelArtUpdate POST /admin/pixelarts/:id/edit
func (s *Server) PixelArtUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	f := pixelArtFormFields(c)
	if f["bindings"] == "" {
		f["bindings"] = "{}"
	}
	if err := validatePixelArtForm(f); err != nil {
		SetFlash(c, "danger", err.Error())
		// re-load existing to keep ID? just pass f
		s.renderPage(c, http.StatusOK, "pixelart_form.html", gin.H{"obj": f, "edit": true, "id": id, "error": err.Error()})
		return
	}
	entry := dsRegistry["pixelart"]
	if err := entry.UpdateFields(s.DB, s.Ctx, id, f); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		s.renderPage(c, http.StatusOK, "pixelart_form.html", gin.H{"obj": f, "edit": true})
		return
	}
	SetFlash(c, "success", "Pixel Art updated")
	c.Redirect(http.StatusFound, "/admin/pixelarts")
}

// PixelArtDelete POST /admin/pixelarts/:id/delete
func (s *Server) PixelArtDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	entry := dsRegistry["pixelart"]
	if err := entry.Delete(s.DB, s.Ctx, id); err != nil {
		SetFlash(c, "danger", "Failed to delete: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/pixelarts")
		return
	}
	SetFlash(c, "success", "Pixel Art deleted")
	c.Redirect(http.StatusFound, "/admin/pixelarts")
}

// PixelArtPreview POST /admin/pixelarts/preview
func (s *Server) PixelArtPreview(c *gin.Context) {
	w, _ := strconv.Atoi(c.PostForm("width"))
	h, _ := strconv.Atoi(c.PostForm("height"))
	gw, _ := strconv.Atoi(c.PostForm("grid_width"))
	gh, _ := strconv.Atoi(c.PostForm("grid_height"))
	frames := c.PostForm("frames")
	bindings := c.PostForm("bindings")
	apiURL := c.PostForm("api_url")
	apiToken := c.PostForm("api_token")

	if w <= 0 {
		w = 64
	}
	if h <= 0 {
		h = 64
	}
	if gw < 1 || gw > render.MaxPixelGrid || gh < 1 || gh > render.MaxPixelGrid {
		c.String(http.StatusBadRequest, "invalid grid dimensions")
		return
	}
	if frames == "" {
		c.String(http.StatusBadRequest, "frames required")
		return
	}
	if _, err := render.ParsePixelFrames(frames, gw, gh); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	if bindings != "" {
		if err := validPixelBindings(bindings); err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
	}
	ds := datasource.NewPixelArtDS(gw, gh, frames, bindings, apiURL, apiToken)
	img, err := ds.GetPNG(w, h)
	if err != nil || img == nil {
		c.String(http.StatusBadGateway, "render failed")
		return
	}
	c.Data(http.StatusOK, "image/png", img.Data)
}
