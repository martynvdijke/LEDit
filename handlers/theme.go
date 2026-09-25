package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/theme"
	"ledit/ent/themeassignment"
	"ledit/render"
	"ledit/render/themes"
)

// ponytail: fonts are selected from the bundled fonts/ dir; uploads deferred (attack surface).

// ---------------------------------------------------------------------------
// Theme resolution (Phase 8)
//
// Effective theme = per-datasource override -> global default -> built-in
// default. Resolution is intentionally uncached: a local SQLite read per
// render is cheaper than an invalidation bug.
// ---------------------------------------------------------------------------

type themeResolver struct {
	db  *ent.Client
	ctx context.Context
}

var globalThemeResolver *themeResolver

// InitThemeResolver wires the resolver used by the feed render loop.
func InitThemeResolver(client *ent.Client, ctx context.Context) {
	globalThemeResolver = &themeResolver{db: client, ctx: ctx}
}

// ResolveTheme returns the effective theme for a "<type>:<id>" source.
func ResolveTheme(targetType string, targetID int) render.Theme {
	if globalThemeResolver == nil {
		return themes.DefaultTheme
	}
	return globalThemeResolver.resolve(targetType, targetID)
}

func (r *themeResolver) resolve(targetType string, targetID int) render.Theme {
	if targetType != "" {
		a, err := r.db.ThemeAssignment.Query().
			Where(themeassignment.TargetTypeEQ(targetType), themeassignment.TargetIDEQ(targetID)).
			WithTheme().
			Only(r.ctx)
		if err == nil && a.Edges.Theme != nil {
			return ThemeToRender(a.Edges.Theme)
		}
	}
	if t, err := r.db.Theme.Query().Where(theme.IsDefaultEQ(true)).First(r.ctx); err == nil {
		return ThemeToRender(t)
	}
	return themes.DefaultTheme
}

func ThemeFontPath(name string) string {
	if name == "" {
		return ""
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return ""
	}
	for _, dir := range []string{"fonts", "../fonts"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if e.Name() == name {
				return "fonts/" + name
			}
		}
	}
	return ""
}

func listBundledFonts() []string {
	for _, dir := range []string{"fonts", "../fonts"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var out []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasSuffix(strings.ToLower(name), ".ttf") || strings.HasSuffix(strings.ToLower(name), ".otf") {
				out = append(out, name)
			}
		}
		if out != nil {
			return out
		}
	}
	return nil
}

func themeFontPath(name string) string { return ThemeFontPath(name) }

// ThemeToRender maps a persisted theme row onto the render palette.
func ThemeToRender(t *ent.Theme) render.Theme {
	bg := parseHexColorRGBA(t.BgColor)
	accent := parseHexColorRGBA(t.AccentColor)
	text := parseHexColorRGBA(t.TextColor)
	title := t.Title
	if title == "" {
		title = strings.ToUpper(t.Name)
	}
	return render.Theme{
		Name:            t.Name,
		BackgroundColor: [3]uint8{bg.R, bg.G, bg.B},
		AccentColor:     [3]uint8{accent.R, accent.G, accent.B},
		TextColor:       [3]uint8{text.R, text.G, text.B},
		Title:           title,
		FontSize:        t.FontSize,
		FontPath:        ThemeFontPath(t.FontName),
	}
}

func hexOf(c [3]uint8) string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

// ---------------------------------------------------------------------------
// Seeding + legacy import
// ---------------------------------------------------------------------------

// seedThemes creates the immutable built-in themes on first run and imports the
// legacy GeneralSettings.theme JSON once.
func seedThemes(client *ent.Client, ctx context.Context) {
	count, err := client.Theme.Query().Count(ctx)
	if err != nil {
		slog.Warn("theme seed: count failed", "error", err)
		return
	}
	if count == 0 {
		for i, t := range []render.Theme{themes.DefaultTheme, themes.F1Theme, themes.UntappdTheme} {
			_, err := client.Theme.Create().
				SetName(t.Name).
				SetBgColor(hexOf(t.BackgroundColor)).
				SetAccentColor(hexOf(t.AccentColor)).
				SetTextColor(hexOf(t.TextColor)).
				SetTitle(t.Title).
				SetFontSize(t.FontSize).
				SetBuiltIn(true).
				SetIsDefault(i == 0).
				Save(ctx)
			if err != nil {
				slog.Warn("theme seed: create failed", "name", t.Name, "error", err)
			}
		}
	}
	importLegacyTheme(client, ctx)
}

// importLegacyTheme converts the old GeneralSettings.theme JSON blob into a
// regular "Imported" theme (once). It is idempotent: it skips when any
// user-created theme already exists, when the blob is empty, or when it does
// not parse. The legacy column is left untouched for rollback.
func importLegacyTheme(client *ent.Client, ctx context.Context) {
	userCount, err := client.Theme.Query().Where(theme.BuiltInEQ(false)).Count(ctx)
	if err != nil || userCount > 0 {
		return
	}
	gs, err := client.GeneralSettings.Query().First(ctx)
	if err != nil || strings.TrimSpace(gs.Theme) == "" || strings.TrimSpace(gs.Theme) == "{}" {
		return
	}
	var legacy struct {
		BgColor     string  `json:"bg_color"`
		AccentColor string  `json:"accent_color"`
		TextColor   string  `json:"text_color"`
		Title       string  `json:"title"`
		FontSize    float64 `json:"font_size"`
	}
	if err := json.Unmarshal([]byte(gs.Theme), &legacy); err != nil {
		return
	}
	if !hexColorRe.MatchString(legacy.BgColor) &&
		!hexColorRe.MatchString(legacy.AccentColor) &&
		!hexColorRe.MatchString(legacy.TextColor) {
		return // nothing usable
	}
	base := themes.DefaultTheme
	if !hexColorRe.MatchString(legacy.BgColor) {
		legacy.BgColor = hexOf(base.BackgroundColor)
	}
	if !hexColorRe.MatchString(legacy.AccentColor) {
		legacy.AccentColor = hexOf(base.AccentColor)
	}
	if !hexColorRe.MatchString(legacy.TextColor) {
		legacy.TextColor = hexOf(base.TextColor)
	}
	if legacy.Title == "" {
		legacy.Title = "IMPORTED"
	}
	if legacy.FontSize < 8 || legacy.FontSize > 100 {
		legacy.FontSize = base.FontSize
	}
	if _, err := client.Theme.Create().
		SetName("Imported").
		SetBgColor(legacy.BgColor).
		SetAccentColor(legacy.AccentColor).
		SetTextColor(legacy.TextColor).
		SetTitle(legacy.Title).
		SetFontSize(legacy.FontSize).
		SetIsDefault(true).
		Save(ctx); err != nil {
		slog.Warn("theme import failed", "error", err)
		return
	}
	// The imported theme takes over as default; demote the built-in default.
	_, _ = client.Theme.Update().Where(theme.IsDefaultEQ(true), theme.BuiltInEQ(true)).SetIsDefault(false).Save(ctx)
}

// ---------------------------------------------------------------------------
// Admin handlers
// ---------------------------------------------------------------------------

// themeTarget is an assignable/previewable source. Only sources that support
// themed rendering are listed.
type themeTarget struct {
	Type  string
	ID    int
	Label string
	Key   string
}

func (s *Server) themeTargets() []themeTarget {
	out := []themeTarget{{Type: "clock", ID: 0, Label: "Clock", Key: "clock:0"}}
	if mats, err := s.DB.MatrixLayout.Query().All(s.Ctx); err == nil {
		for _, m := range mats {
			out = append(out, themeTarget{Type: "matrix", ID: m.ID, Label: "Matrix: " + m.Name, Key: fmt.Sprintf("matrix:%d", m.ID)})
		}
	}
	if comps, err := s.DB.Composition.Query().All(s.Ctx); err == nil {
		for _, comp := range comps {
			out = append(out, themeTarget{Type: "composition", ID: comp.ID, Label: "Composition: " + comp.Name, Key: fmt.Sprintf("composition:%d", comp.ID)})
		}
	}
	return out
}

func (s *Server) themeAssignments() map[string]int {
	out := map[string]int{}
	rows, err := s.DB.ThemeAssignment.Query().WithTheme().All(s.Ctx)
	if err != nil {
		return out
	}
	for _, a := range rows {
		out[fmt.Sprintf("%s:%d", a.TargetType, a.TargetID)] = a.Edges.Theme.ID
	}
	return out
}

func (s *Server) AdminThemeList(c *gin.Context) {
	items, err := s.DB.Theme.Query().Order(ent.Asc(theme.FieldBuiltIn), ent.Asc(theme.FieldName)).All(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to load themes")
		items = nil
	}
	s.renderPage(c, 200, "themes.html", gin.H{
		"themes":      items,
		"targets":     s.themeTargets(),
		"assignments": s.themeAssignments(),
	})
}

// themeEditor loads a theme (nil for new) plus the preview/assignment targets.
func (s *Server) themeEditor(c *gin.Context, t *ent.Theme) {
	preview := themeTarget{Type: "clock", ID: 0, Label: "Clock", Key: "clock:0"}
	targets := s.themeTargets()
	if len(targets) > 0 {
		preview = targets[0]
	}
	s.renderPage(c, 200, "theme_editor.html", gin.H{
		"theme":       t,
		"targets":     targets,
		"previewType": preview.Type,
		"previewID":   preview.ID,
		"fonts":       listBundledFonts(),
	})
}

func (s *Server) AdminThemeNew(c *gin.Context) {
	s.themeEditor(c, nil)
}

func (s *Server) AdminThemeEditor(c *gin.Context) {
	if c.Param("id") == "new" {
		s.themeEditor(c, nil)
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	t, err := s.DB.Theme.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Theme not found")
		c.Redirect(302, "/admin/themes")
		return
	}
	s.themeEditor(c, t)
}

// themeFormValues validates and returns the submitted palette values.
func themeFormValues(c *gin.Context) (name, bg, accent, text, title string, fontSize float64, fontName string, ok bool) {
	name = strings.TrimSpace(c.PostForm("name"))
	bg = strings.TrimSpace(c.PostForm("bg_color"))
	accent = strings.TrimSpace(c.PostForm("accent_color"))
	text = strings.TrimSpace(c.PostForm("text_color"))
	title = strings.TrimSpace(c.PostForm("title"))
	fontSize, _ = strconv.ParseFloat(c.DefaultPostForm("font_size", "24"), 64)
	fontName = strings.TrimSpace(c.PostForm("font_name"))

	switch {
	case name == "":
		SetFlash(c, "danger", "Theme name is required")
	case !hexColorRe.MatchString(bg) || !hexColorRe.MatchString(accent) || !hexColorRe.MatchString(text):
		SetFlash(c, "danger", "Colors must be hex like #rrggbb")
	case fontSize < 8 || fontSize > 100:
		SetFlash(c, "danger", "Font size must be between 8 and 100")
	case fontName != "" && ThemeFontPath(fontName) == "":
		SetFlash(c, "danger", "Invalid font selection")
	default:
		ok = true
	}
	return
}

func (s *Server) AdminThemeSave(c *gin.Context) {
	id := 0
	if c.Param("id") != "new" {
		id, _ = strconv.Atoi(c.Param("id"))
	}
	name, bg, accent, text, title, fontSize, fontName, ok := themeFormValues(c)
	if !ok {
		themeSaveRedirect(c, id, false)
		return
	}

	if id > 0 {
		existing, err := s.DB.Theme.Get(s.Ctx, id)
		if err != nil {
			SetFlash(c, "danger", "Theme not found")
			c.Redirect(302, "/admin/themes")
			return
		}
		if existing.BuiltIn {
			SetFlash(c, "danger", "Built-in themes cannot be edited")
			c.Redirect(302, "/admin/themes")
			return
		}
		_, err = s.DB.Theme.UpdateOneID(id).
			SetName(name).SetBgColor(bg).SetAccentColor(accent).SetTextColor(text).
			SetTitle(title).SetFontSize(fontSize).SetFontName(fontName).Save(s.Ctx)
		if err != nil {
			SetFlash(c, "danger", "Failed to save theme: "+err.Error())
			c.Redirect(302, themeBackURL(c, id))
			return
		}
		SetFlash(c, "success", "Theme saved")
	} else {
		create := s.DB.Theme.Create().
			SetName(name).SetBgColor(bg).SetAccentColor(accent).SetTextColor(text).
			SetTitle(title).SetFontSize(fontSize).SetFontName(fontName)
		if c.PostForm("is_default") != "" {
			_, _ = s.DB.Theme.Update().Where(theme.IsDefaultEQ(true)).SetIsDefault(false).Save(s.Ctx)
			create.SetIsDefault(true)
		}
		if _, err := create.Save(s.Ctx); err != nil {
			SetFlash(c, "danger", "Failed to create theme: "+err.Error())
			c.Redirect(302, "/admin/themes/new")
			return
		}
		SetFlash(c, "success", "Theme created")
	}
	c.Redirect(302, "/admin/themes")
}

func themeBackURL(c *gin.Context, id int) string {
	if id > 0 {
		return fmt.Sprintf("/admin/themes/%d/edit", id)
	}
	return "/admin/themes/new"
}

// themeSaveRedirect returns the post-save redirect target. On validation
// failure it re-renders the form (200) instead of redirecting, so callers can
// detect rejection via the absence of a Location header.
func themeSaveRedirect(c *gin.Context, id int, ok bool) {
	if !ok {
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(302, themeBackURL(c, id))
}

func (s *Server) AdminThemeDuplicate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	src, err := s.DB.Theme.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Theme not found")
		c.Redirect(302, "/admin/themes")
		return
	}
	name := src.Name + " copy"
	for i := 2; ; i++ {
		exists, _ := s.DB.Theme.Query().Where(theme.NameEQ(name)).Exist(s.Ctx)
		if !exists {
			break
		}
		name = fmt.Sprintf("%s copy %d", src.Name, i)
	}
	created, err := s.DB.Theme.Create().
		SetName(name).SetBgColor(src.BgColor).SetAccentColor(src.AccentColor).
		SetTextColor(src.TextColor).SetTitle(src.Title).SetFontSize(src.FontSize).SetFontName(src.FontName).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to duplicate theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	SetFlash(c, "success", "Theme duplicated")
	c.Redirect(302, fmt.Sprintf("/admin/themes/%d/edit", created.ID))
}

func (s *Server) AdminThemeDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	t, err := s.DB.Theme.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Theme not found")
		c.Redirect(302, "/admin/themes")
		return
	}
	if t.BuiltIn {
		SetFlash(c, "danger", "Built-in themes cannot be deleted")
		c.Redirect(302, "/admin/themes")
		return
	}
	if t.IsDefault {
		SetFlash(c, "danger", "Set another theme as default before deleting this one")
		c.Redirect(302, "/admin/themes")
		return
	}
	if _, err := s.DB.ThemeAssignment.Delete().Where(themeassignment.HasThemeWith(theme.ID(id))).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to clear theme assignments")
		c.Redirect(302, "/admin/themes")
		return
	}
	if err := s.DB.Theme.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	SetFlash(c, "success", "Theme deleted")
	c.Redirect(302, "/admin/themes")
}

func (s *Server) AdminThemeSetDefault(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	if _, err := s.DB.Theme.Get(s.Ctx, id); err != nil {
		SetFlash(c, "danger", "Theme not found")
		c.Redirect(302, "/admin/themes")
		return
	}
	if _, err := s.DB.Theme.Update().Where(theme.IsDefaultEQ(true)).SetIsDefault(false).Save(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update default theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	if err := s.DB.Theme.UpdateOneID(id).SetIsDefault(true).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to set default theme")
		c.Redirect(302, "/admin/themes")
		return
	}
	SetFlash(c, "success", "Default theme updated")
	c.Redirect(302, "/admin/themes")
}

// AdminThemeAssign sets or clears the per-source theme override. An empty
// theme_id clears the assignment.
func (s *Server) AdminThemeAssign(c *gin.Context) {
	targetType := strings.TrimSpace(c.PostForm("target_type"))
	targetID, _ := strconv.Atoi(c.PostForm("target_id"))
	themeID, _ := strconv.Atoi(c.PostForm("theme_id"))
	back := c.PostForm("redirect")
	if back == "" {
		back = "/admin/themes"
	}
	if targetType == "" {
		SetFlash(c, "danger", "Invalid assignment target")
		c.Redirect(302, back)
		return
	}
	if _, err := s.DB.ThemeAssignment.Delete().
		Where(themeassignment.TargetTypeEQ(targetType), themeassignment.TargetIDEQ(targetID)).
		Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update assignment")
		c.Redirect(302, back)
		return
	}
	if themeID > 0 {
		if _, err := s.DB.Theme.Get(s.Ctx, themeID); err != nil {
			SetFlash(c, "danger", "Theme not found")
			c.Redirect(302, back)
			return
		}
		if err := s.DB.ThemeAssignment.Create().SetTargetType(targetType).SetTargetID(targetID).SetThemeID(themeID).Exec(s.Ctx); err != nil {
			SetFlash(c, "danger", "Failed to update assignment")
			c.Redirect(302, back)
			return
		}
	}
	SetFlash(c, "success", "Theme assignment updated")
	c.Redirect(302, back)
}

// AdminThemeLegacyRedirect keeps the old /admin/theme URL working.
func (s *Server) AdminThemeLegacyRedirect(c *gin.Context) {
	c.Redirect(302, "/admin/themes")
}

// ---------------------------------------------------------------------------
// Preview theme override
// ---------------------------------------------------------------------------

// themeOverrideFrom reads optional theme tokens from the request (form or
// query): theme_id selects a saved theme, theme_* tokens build an unsaved one.
// ok=false means "use the source's effective theme".
func (s *Server) themeOverrideFrom(c *gin.Context) (render.Theme, bool) {
	get := func(key string) string {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			return v
		}
		return strings.TrimSpace(c.Query(key))
	}
	if idStr := get("theme_id"); idStr != "" {
		if id, err := strconv.Atoi(idStr); err == nil && id > 0 {
			if t, err := s.DB.Theme.Get(s.Ctx, id); err == nil {
				return ThemeToRender(t), true
			}
		}
	}
	base := themes.DefaultTheme
	overridden := false
	if v := get("theme_bg"); v != "" {
		base.BackgroundColor = rgb3(parseHexColorRGBA(v), base.BackgroundColor)
		overridden = true
	}
	if v := get("theme_accent"); v != "" {
		base.AccentColor = rgb3(parseHexColorRGBA(v), base.AccentColor)
		overridden = true
	}
	if v := get("theme_text"); v != "" {
		base.TextColor = rgb3(parseHexColorRGBA(v), base.TextColor)
		overridden = true
	}
	if v := get("theme_title"); v != "" {
		base.Title = v
		overridden = true
	}
	if v := get("theme_font_size"); v != "" {
		if fs, err := strconv.ParseFloat(v, 64); err == nil {
			base.FontSize = fs
			overridden = true
		}
	}
	return base, overridden
}

func rgb3(c color.RGBA, fallback [3]uint8) [3]uint8 {
	if c.A == 0 && c.R == 0 && c.G == 0 && c.B == 0 {
		return fallback
	}
	return [3]uint8{c.R, c.G, c.B}
}

// themeCacheSig fingerprints a palette so frames cached for one theme are not
// served for another.
func themeCacheSig(t render.Theme) string {
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x|%s|%.1f|%s",
		t.BackgroundColor[0], t.BackgroundColor[1], t.BackgroundColor[2],
		t.AccentColor[0], t.AccentColor[1], t.AccentColor[2],
		t.TextColor[0], t.TextColor[1], t.TextColor[2],
		t.Title, t.FontSize, t.FontPath)
}
