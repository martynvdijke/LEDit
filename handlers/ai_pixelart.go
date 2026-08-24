package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
)

var pixelartRateMu sync.Mutex
var pixelartRate = map[string][]time.Time{}

func checkPixelArtRateLimit(key string) bool {
	pixelartRateMu.Lock()
	defer pixelartRateMu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	times := pixelartRate[key]
	filtered := times[:0]
	for _, t := range times {
		if t.After(cut) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= 5 {
		pixelartRate[key] = filtered
		return false
	}
	filtered = append(filtered, now)
	pixelartRate[key] = filtered
	return true
}

// BuildPixelArtPrompt builds prompt per design D1.
func BuildPixelArtPrompt(prompt string, width, height int, paletteHint string, frameCount int, currentDraftJSON string) string {
	var sb strings.Builder
	sb.WriteString("You are a pixel art generator. Return ONLY JSON:\n")
	sb.WriteString(`{"palette":["#RRGGBB",...],"frames":[[[idx,...],...],...],"duration_ms":500}` + "\n\n")
	sb.WriteString("Constraints:\n")
	sb.WriteString("- palette: 2-16 colors, each #RRGGBB\n")
	sb.WriteString(fmt.Sprintf("- frames: %d frames, each %d rows x %d cols, each cell is palette index\n", frameCount, height, width))
	sb.WriteString("- duration_ms: 100-5000\n")
	sb.WriteString("- Use limited palette, dither sparingly, prefer solid shapes.\n\n")
	sb.WriteString("Example (2x2, 1 frame):\n")
	sb.WriteString(`Prompt: "red dot"` + "\n")
	sb.WriteString(`Response: {"palette":["#000000","#FF0000"],"frames":[[[0,0],[0,1]]],"duration_ms":500}` + "\n\n")
	if currentDraftJSON != "" {
		sb.WriteString(fmt.Sprintf("Current draft: %s — Modify it per user instruction: %q\n\n", currentDraftJSON, prompt))
	} else {
		hint := ""
		if paletteHint != "" {
			hint = fmt.Sprintf(" Palette hint: %q.", paletteHint)
		}
		sb.WriteString(fmt.Sprintf("User prompt: %q%s Width=%d Height=%d FrameCount=%d\n", prompt, hint, width, height, frameCount))
	}
	return sb.String()
}

var hexColorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type pixelArtPayload struct {
	Palette    []string  `json:"palette"`
	Frames     [][][]int `json:"frames"`
	DurationMs int       `json:"duration_ms"`
}

// ValidatePixelArtPayload validates raw LLM response.
func ValidatePixelArtPayload(rawJSON []byte, width, height, frameCount int) (pixelArtPayload, error) {
	if len(rawJSON) > 64*1024 {
		return pixelArtPayload{}, fmt.Errorf("payload too large (max 64KB)")
	}
	// strip fences
	s := strings.TrimSpace(string(rawJSON))
	if strings.Contains(s, "```") {
		// extract between fences
		// try to find first { and last }
		start := strings.Index(s, "{")
		end := strings.LastIndex(s, "}")
		if start != -1 && end != -1 && end > start {
			s = s[start : end+1]
		} else {
			// fallback strip ```
			s = strings.ReplaceAll(s, "```json", "")
			s = strings.ReplaceAll(s, "```", "")
			s = strings.TrimSpace(s)
			start = strings.Index(s, "{")
			end = strings.LastIndex(s, "}")
			if start != -1 && end != -1 && end > start {
				s = s[start : end+1]
			}
		}
	} else if !strings.HasPrefix(s, "{") {
		start := strings.Index(s, "{")
		end := strings.LastIndex(s, "}")
		if start != -1 && end != -1 && end > start {
			s = s[start : end+1]
		}
	}
	rawJSON = []byte(s)
	if len(rawJSON) > 64*1024 {
		return pixelArtPayload{}, fmt.Errorf("payload too large (max 64KB)")
	}
	// reject unknown top-level keys
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(rawJSON, &rawMap); err != nil {
		return pixelArtPayload{}, fmt.Errorf("invalid JSON: %w", err)
	}
	for k := range rawMap {
		if k != "palette" && k != "frames" && k != "duration_ms" {
			return pixelArtPayload{}, fmt.Errorf("unknown field %q", k)
		}
	}
	var p pixelArtPayload
	if err := json.Unmarshal(rawJSON, &p); err != nil {
		return pixelArtPayload{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(p.Palette) < 2 || len(p.Palette) > 16 {
		return pixelArtPayload{}, fmt.Errorf("palette size must be 2-16, got %d", len(p.Palette))
	}
	for i, c := range p.Palette {
		if !hexColorRe.MatchString(c) {
			return pixelArtPayload{}, fmt.Errorf("palette[%d] %q is not a valid #RRGGBB color", i, c)
		}
	}
	if p.DurationMs < 100 || p.DurationMs > 5000 {
		return pixelArtPayload{}, fmt.Errorf("duration_ms must be 100-5000, got %d", p.DurationMs)
	}
	if width < 8 || width > 64 || height < 8 || height > 64 {
		return pixelArtPayload{}, fmt.Errorf("width/height must be 8-64")
	}
	if frameCount < 1 || frameCount > 8 {
		return pixelArtPayload{}, fmt.Errorf("frameCount must be 1-8")
	}
	if len(p.Frames) != frameCount {
		return pixelArtPayload{}, fmt.Errorf("frame count mismatch: want %d, got %d", frameCount, len(p.Frames))
	}
	for fi, frame := range p.Frames {
		if len(frame) != height {
			return pixelArtPayload{}, fmt.Errorf("frame %d has %d rows, want %d", fi, len(frame), height)
		}
		for ri, row := range frame {
			if len(row) != width {
				return pixelArtPayload{}, fmt.Errorf("frame %d row %d has %d cols, want %d", fi, ri, len(row), width)
			}
			for ci, idx := range row {
				if idx < 0 || idx >= len(p.Palette) {
					return pixelArtPayload{}, fmt.Errorf("frame %d row %d col %d: index %d out of range for palette size %d", fi, ri, ci, idx, len(p.Palette))
				}
			}
		}
	}
	// normalize colors to uppercase
	for i, c := range p.Palette {
		p.Palette[i] = strings.ToUpper(c)
	}
	return p, nil
}

func isAIConfigured() (datasource.AIConfig, bool) {
	// helper to check AI config exists
	return datasource.AIConfig{}, false
}

func getAIConfig(ctx context.Context, db *ent.Client) (datasource.AIConfig, bool) {
	ai, err := db.AISettings.Query().Only(ctx)
	if err != nil || ai.Endpoint == "" || ai.Model == "" {
		return datasource.AIConfig{}, false
	}
	if ai.APIKey == "" && ai.Provider != "ollama" {
		return datasource.AIConfig{}, false
	}
	return datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}, true
}

// POST /api/pixelart/generate
func (s *Server) PixelArtGenerate(c *gin.Context) {
	if !checkPixelArtRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded (5/min)"})
		return
	}
	var req struct {
		Prompt      string `json:"prompt"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		PaletteHint string `json:"palette_hint"`
		FrameCount  int    `json:"frame_count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.Width == 0 {
		req.Width = 32
	}
	if req.Height == 0 {
		req.Height = 32
	}
	if req.FrameCount == 0 {
		req.FrameCount = 1
	}
	if len(req.Prompt) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt is required"})
		return
	}
	if len(req.Prompt) > 2000 {
		req.Prompt = req.Prompt[:2000]
	}
	if req.Width < 8 || req.Width > 64 || req.Height < 8 || req.Height > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "width/height must be 8-64"})
		return
	}
	if req.FrameCount < 1 || req.FrameCount > 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "frame_count must be 1-8"})
		return
	}
	cfg, ok := getAIConfig(c.Request.Context(), s.DB)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI not configured"})
		return
	}
	prompt := BuildPixelArtPrompt(req.Prompt, req.Width, req.Height, req.PaletteHint, req.FrameCount, "")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	content, err := datasource.ChatCompletions(ctx, cfg, []datasource.ChatMessage{
		{Role: "system", Content: "You are a pixel art generator. Return ONLY JSON."},
		{Role: "user", Content: prompt},
	}, 2000)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "AI request timed out"})
			return
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	payload, err := ValidatePixelArtPayload([]byte(content), req.Width, req.Height, req.FrameCount)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "details": err.Error()})
		return
	}
	// convert to storage format: render.PixelFrameDoc
	frames := make([]map[string]interface{}, 0)
	_ = frames
	// Build frames JSON for storage: use render.PixelFrameDoc -> but we store as that doc
	// We'll store palette + frames as PixelFrameDoc JSON
	type storeFrame struct {
		Duration int   `json:"duration"`
		Pixels   []int `json:"pixels"`
	}
	type storeDoc struct {
		Palette    []string     `json:"palette"`
		Frames     []storeFrame `json:"frames"`
		Background string       `json:"background,omitempty"`
	}
	doc := storeDoc{Palette: payload.Palette, Background: "#000000"}
	for _, f := range payload.Frames {
		flat := make([]int, 0, req.Width*req.Height)
		for _, row := range f {
			flat = append(flat, row...)
		}
		doc.Frames = append(doc.Frames, storeFrame{Duration: payload.DurationMs, Pixels: flat})
	}
	raw, _ := json.Marshal(doc)
	f := map[string]string{
		"name":        fmt.Sprintf("AI %s", truncate(req.Prompt, 30)),
		"grid_width":  strconv.Itoa(req.Width),
		"grid_height": strconv.Itoa(req.Height),
		"frames":      string(raw),
		"bindings":    "{}",
		"api_url":     "",
		"api_token":   "",
		"enabled":     "on",
	}
	entry := dsRegistry["pixelart"]
	obj, err := entry.CreateFields(s.DB, s.Ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create pixelart: " + err.Error()})
		return
	}
	// set is_draft=true
	var id int
	if pa, ok := obj.(*ent.PixelArt); ok {
		id = pa.ID
		s.DB.PixelArt.UpdateOneID(id).SetIsDraft(true).Exec(s.Ctx)
	}
	if gs, err := s.DB.GeneralSettings.Query().First(s.Ctx); err == nil && gs != nil {
		_ = entry.AddEdge(s.DB.GeneralSettings.UpdateOne(gs), obj).Exec(s.Ctx)
	}
	// fetch updated
	pa, _ := s.DB.PixelArt.Get(s.Ctx, id)
	c.JSON(http.StatusCreated, gin.H{"id": id, "pixelart": pa})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// POST /api/pixelart/:id/refine
func (s *Server) PixelArtRefine(c *gin.Context) {
	if !checkPixelArtRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded (5/min)"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	pa, err := s.DB.PixelArt.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pixelart not found"})
		return
	}
	if !pa.IsDraft {
		c.JSON(http.StatusConflict, gin.H{"error": "only drafts can be refined"})
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt is required"})
		return
	}
	if len(req.Prompt) > 2000 {
		req.Prompt = req.Prompt[:2000]
	}
	cfg, ok := getAIConfig(c.Request.Context(), s.DB)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI not configured"})
		return
	}
	// current draft JSON
	currentJSON := pa.Frames
	prompt := BuildPixelArtPrompt(req.Prompt, pa.GridWidth, pa.GridHeight, "", 1, currentJSON)
	// frameCount derive from existing doc? Use existing frames count
	frameCount := 1
	// try parse existing to get frame count
	var existing struct {
		Frames []struct {
			Pixels []int `json:"pixels"`
		} `json:"frames"`
	}
	if err := json.Unmarshal([]byte(pa.Frames), &existing); err == nil && len(existing.Frames) > 0 {
		frameCount = len(existing.Frames)
	}
	// But design says refine overwrites with same dimensions; use width/height from entity
	prompt = BuildPixelArtPrompt(req.Prompt, pa.GridWidth, pa.GridHeight, "", frameCount, currentJSON)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	content, err := datasource.ChatCompletions(ctx, cfg, []datasource.ChatMessage{
		{Role: "system", Content: "You are a pixel art generator. Return ONLY JSON."},
		{Role: "user", Content: prompt},
	}, 2000)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "AI request timed out"})
			return
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	payload, err := ValidatePixelArtPayload([]byte(content), pa.GridWidth, pa.GridHeight, frameCount)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "details": err.Error()})
		return
	}
	type storeFrame struct {
		Duration int   `json:"duration"`
		Pixels   []int `json:"pixels"`
	}
	type storeDoc struct {
		Palette    []string     `json:"palette"`
		Frames     []storeFrame `json:"frames"`
		Background string       `json:"background,omitempty"`
	}
	doc := storeDoc{Palette: payload.Palette, Background: "#000000"}
	for _, f := range payload.Frames {
		flat := make([]int, 0, pa.GridWidth*pa.GridHeight)
		for _, row := range f {
			flat = append(flat, row...)
		}
		doc.Frames = append(doc.Frames, storeFrame{Duration: payload.DurationMs, Pixels: flat})
	}
	raw, _ := json.Marshal(doc)
	err = s.DB.PixelArt.UpdateOneID(id).SetFrames(string(raw)).Exec(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update draft"})
		return
	}
	updated, _ := s.DB.PixelArt.Get(s.Ctx, id)
	c.JSON(http.StatusOK, gin.H{"id": id, "pixelart": updated})
}

// POST /api/pixelart/:id/publish
func (s *Server) PixelArtPublish(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	pa, err := s.DB.PixelArt.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pixelart not found"})
		return
	}
	if !pa.IsDraft {
		c.JSON(http.StatusOK, gin.H{"id": id, "is_draft": false})
		return
	}
	err = s.DB.PixelArt.UpdateOneID(id).SetIsDraft(false).Exec(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "is_draft": false})
}
