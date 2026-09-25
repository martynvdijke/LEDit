package handlers

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/theme"
	"ledit/render/themes"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var themeCtx = context.Background()

func newThemeTestDB(t *testing.T) *ent.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared&_fk=1"
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	return client
}

func callSave(srv *Server, idParam string, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/themes/"+idParam+"/edit", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Params = gin.Params{{Key: "id", Value: idParam}}
	srv.AdminThemeSave(c)
	return w
}
func callDel(srv *Server, idParam string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/themes/"+idParam+"/delete", nil)
	c.Params = gin.Params{{Key: "id", Value: idParam}}
	srv.AdminThemeDelete(c)
	return w
}
func callDup(srv *Server, idParam string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/themes/"+idParam+"/duplicate", nil)
	c.Params = gin.Params{{Key: "id", Value: idParam}}
	srv.AdminThemeDuplicate(c)
	return w
}
func callSetDef(srv *Server, idParam string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/themes/"+idParam+"/default", nil)
	c.Params = gin.Params{{Key: "id", Value: idParam}}
	srv.AdminThemeSetDefault(c)
	return w
}
func themeForm(name string) url.Values {
	v := url.Values{}
	v.Set("name", name)
	v.Set("bg_color", "#112233")
	v.Set("accent_color", "#445566")
	v.Set("text_color", "#778899")
	v.Set("title", "TEST")
	v.Set("font_size", "24")
	return v
}

func TestThemeValidation(t *testing.T) {
	client := newThemeTestDB(t)
	srv := &Server{DB: client, Ctx: themeCtx}

	w := callSave(srv, "new", url.Values{
		"name":         {"badhex"},
		"bg_color":     {"red"},
		"accent_color": {"#445566"},
		"text_color":   {"#778899"},
		"title":        {"T"},
		"font_size":    {"24"},
	})
	if w.Header().Get("Location") != "" {
		t.Fatalf("bad hex should re-render form, got redirect %v", w.Header())
	}
	if client.Theme.Query().Where(theme.NameEQ("badhex")).ExistX(themeCtx) {
		t.Error("bad hex theme should not be created")
	}
	w2 := callSave(srv, "new", url.Values{
		"name":         {""},
		"bg_color":     {"#112233"},
		"accent_color": {"#445566"},
		"text_color":   {"#778899"},
		"title":        {"T"},
		"font_size":    {"24"},
	})
	if w2.Header().Get("Location") != "" {
		t.Fatalf("empty name should re-render form, got redirect %d", w2.Code)
	}
	w3 := callSave(srv, "new", themeForm("uniqtheme"))
	if w3.Header().Get("Location") == "" {
		t.Fatalf("create uniq expected redirect got %d", w3.Code)
	}
	if !client.Theme.Query().Where(theme.NameEQ("uniqtheme")).ExistX(themeCtx) {
		t.Fatal("uniqtheme should exist")
	}
	w4 := callSave(srv, "new", themeForm("uniqtheme"))
	if w4.Header().Get("Location") == "" {
		t.Fatalf("dup name expected redirect got %d", w4.Code)
	}
	count, _ := client.Theme.Query().Where(theme.NameEQ("uniqtheme")).Count(themeCtx)
	if count != 1 {
		t.Errorf("duplicate name should not create second row, got %d", count)
	}
}

func TestThemeBuiltInImmutability(t *testing.T) {
	client := newThemeTestDB(t)
	srv := &Server{DB: client, Ctx: themeCtx}
	built := client.Theme.Create().SetName("cyber").SetBgColor("#282a36").SetAccentColor("#50fa7b").SetTextColor("#8be9fd").SetTitle("SYSTEM STATUS").SetFontSize(24).SetBuiltIn(true).SetIsDefault(true).SaveX(themeCtx)

	w := callSave(srv, strconv.Itoa(built.ID), url.Values{
		"name":         {"hacked"},
		"bg_color":     {"#112233"},
		"accent_color": {"#445566"},
		"text_color":   {"#778899"},
		"title":        {"T"},
		"font_size":    {"24"},
	})
	if w.Header().Get("Location") == "" {
		t.Fatalf("edit builtin expected redirect got %d", w.Code)
	}
	after := client.Theme.GetX(themeCtx, built.ID)
	if after.Name != "cyber" {
		t.Errorf("built-in edit should be refused, name got %q", after.Name)
	}
	w3 := callDel(srv, strconv.Itoa(built.ID))
	if w3.Header().Get("Location") == "" {
		t.Fatalf("delete builtin expected redirect got %d", w3.Code)
	}
	if !client.Theme.Query().Where(theme.IDEQ(built.ID)).ExistX(themeCtx) {
		t.Error("built-in should not be deleted")
	}
	// delete default refused
	client.Theme.UpdateOneID(built.ID).SetIsDefault(false).ExecX(themeCtx)
	customDef := client.Theme.Create().SetName("customDef").SetBgColor("#112233").SetAccentColor("#445566").SetTextColor("#778899").SetTitle("T").SetFontSize(24).SetIsDefault(true).SaveX(themeCtx)
	w4 := callDel(srv, strconv.Itoa(customDef.ID))
	if w4.Header().Get("Location") == "" {
		t.Fatalf("delete default expected redirect got %d", w4.Code)
	}
	if !client.Theme.Query().Where(theme.IDEQ(customDef.ID)).ExistX(themeCtx) {
		t.Error("default theme should not be deletable")
	}
}

func TestThemeCRUD(t *testing.T) {
	client := newThemeTestDB(t)
	srv := &Server{DB: client, Ctx: themeCtx}
	def := client.Theme.Create().SetName("cyber").SetBgColor("#282a36").SetAccentColor("#50fa7b").SetTextColor("#8be9fd").SetTitle("SYSTEM STATUS").SetFontSize(24).SetBuiltIn(true).SetIsDefault(true).SaveX(themeCtx)

	w := callSave(srv, "new", themeForm("mytheme"))
	if w.Header().Get("Location") == "" {
		t.Fatalf("create expected redirect got %d", w.Code)
	}
	my := client.Theme.Query().Where(theme.NameEQ("mytheme")).OnlyX(themeCtx)

	w2 := callDup(srv, strconv.Itoa(my.ID))
	if w2.Header().Get("Location") == "" {
		t.Fatalf("duplicate expected redirect got %d", w2.Code)
	}
	dup := client.Theme.Query().Where(theme.NameEQ("mytheme copy")).OnlyX(themeCtx)
	if dup.BuiltIn {
		t.Error("duplicate should not be built_in")
	}
	if dup.IsDefault {
		t.Error("duplicate should not be default")
	}

	w3 := callSetDef(srv, strconv.Itoa(my.ID))
	if w3.Header().Get("Location") == "" {
		t.Fatalf("set-default expected redirect got %d", w3.Code)
	}
	count, _ := client.Theme.Query().Where(theme.IsDefaultEQ(true)).Count(themeCtx)
	if count != 1 {
		t.Errorf("exactly one default expected, got %d", count)
	}
	if !client.Theme.GetX(themeCtx, my.ID).IsDefault {
		t.Error("mytheme should be default")
	}
	if client.Theme.GetX(themeCtx, def.ID).IsDefault {
		t.Error("previous default should be cleared")
	}

	other := client.Theme.Create().SetName("todelete").SetBgColor("#112233").SetAccentColor("#445566").SetTextColor("#778899").SetTitle("T").SetFontSize(24).SaveX(themeCtx)
	client.ThemeAssignment.Create().SetTargetType("matrix").SetTargetID(1).SetThemeID(other.ID).ExecX(themeCtx)
	w4 := callDel(srv, strconv.Itoa(other.ID))
	if w4.Header().Get("Location") == "" {
		t.Fatalf("delete expected redirect got %d", w4.Code)
	}
	if client.Theme.Query().Where(theme.IDEQ(other.ID)).ExistX(themeCtx) {
		t.Error("theme should be deleted")
	}
	c, _ := client.ThemeAssignment.Query().Count(themeCtx)
	if c != 0 {
		t.Errorf("assignments should be cleared, got %d", c)
	}
}

func TestThemePrecedence(t *testing.T) {
	client := newThemeTestDB(t)
	InitThemeResolver(client, themeCtx)
	got := ResolveTheme("matrix", 1)
	if got != themes.DefaultTheme {
		t.Errorf("with no default row, expected DefaultTheme, got %+v", got)
	}
	def := client.Theme.Create().SetName("defTheme").SetBgColor("#112233").SetAccentColor("#445566").SetTextColor("#778899").SetTitle("DEF").SetFontSize(24).SetIsDefault(true).SaveX(themeCtx)
	over := client.Theme.Create().SetName("overTheme").SetBgColor("#ff0000").SetAccentColor("#00ff00").SetTextColor("#0000ff").SetTitle("OVER").SetFontSize(30).SaveX(themeCtx)
	client.ThemeAssignment.Create().SetTargetType("matrix").SetTargetID(1).SetThemeID(over.ID).ExecX(themeCtx)
	InitThemeResolver(client, themeCtx)
	gotOver := ResolveTheme("matrix", 1)
	if gotOver.Title != "OVER" {
		t.Errorf("override expected OVER got %q", gotOver.Title)
	}
	gotDef := ResolveTheme("matrix", 2)
	if gotDef.Title != "DEF" {
		t.Errorf("default expected DEF got %q", gotDef.Title)
	}
	_ = def
	client.Theme.Update().Where(theme.IsDefaultEQ(true)).SetIsDefault(false).ExecX(themeCtx)
	gotFallback := ResolveTheme("matrix", 2)
	if gotFallback != themes.DefaultTheme {
		t.Errorf("no default row should return DefaultTheme, got %+v", gotFallback)
	}
}

func TestThemePreviewUnsavedTokens(t *testing.T) {
	client := newThemeTestDB(t)
	client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(themeCtx)
	srv := &Server{DB: client, Ctx: themeCtx, Router: gin.New()}
	srv.Router.GET("/admin/preview", srv.AdminPreview)
	req := httptest.NewRequest(http.MethodGet, "/admin/preview?type=clock&id=0&w=64&h=64&theme_accent=%23ff0000", nil)
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("preview expected 200 got %d body %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected image/png got %q", ct)
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatalf("png decode failed: %v", err)
	}
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/?theme_accent=%23ff0000", nil)
	th, ok := srv.themeOverrideFrom(c2)
	if !ok {
		t.Fatal("themeOverrideFrom should return overridden")
	}
	if th.AccentColor != [3]uint8{255, 0, 0} {
		t.Errorf("accent override expected ff0000 got %v", th.AccentColor)
	}
}

func TestLegacyImport(t *testing.T) {
	client := newThemeTestDB(t)
	client.Theme.Create().SetName("cyber").SetBgColor("#282a36").SetAccentColor("#50fa7b").SetTextColor("#8be9fd").SetTitle("SYSTEM STATUS").SetFontSize(24).SetBuiltIn(true).SetIsDefault(true).SaveX(themeCtx)
	client.Theme.Create().SetName("f1").SetBgColor("#111111").SetAccentColor("#222222").SetTextColor("#333333").SetTitle("F1").SetFontSize(24).SetBuiltIn(true).SaveX(themeCtx)
	gs := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).SetTheme(`{"bg_color":"#010101","accent_color":"#020202","text_color":"#030303","title":"LEGACY","font_size":20}`).SaveX(themeCtx)
	importLegacyTheme(client, themeCtx)
	imp := client.Theme.Query().Where(theme.NameEQ("Imported")).OnlyX(themeCtx)
	if imp == nil {
		t.Fatal("Imported theme not created")
	}
	if !imp.IsDefault {
		t.Error("Imported should be default")
	}
	if client.Theme.Query().Where(theme.NameEQ("cyber")).OnlyX(themeCtx).IsDefault {
		t.Error("cyber should be demoted")
	}
	importLegacyTheme(client, themeCtx)
	count, _ := client.Theme.Query().Where(theme.NameEQ("Imported")).Count(themeCtx)
	if count != 1 {
		t.Errorf("idempotent: expected 1 Imported, got %d", count)
	}
	gs2 := client.GeneralSettings.GetX(themeCtx, gs.ID)
	if gs2.Theme != gs.Theme {
		t.Error("GeneralSettings.Theme should be unchanged")
	}
	// empty / {} / unparseable ignored
	for i, val := range []string{"", "{}", "notjson"} {
		dsn := "file:legacy_" + strconv.Itoa(i) + "_" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared&_fk=1"
		drv, _ := sql.Open(dialect.SQLite, dsn)
		drv.DB().SetMaxOpenConns(1)
		c2 := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
		c2.Theme.Create().SetName("cyber").SetBgColor("#282a36").SetAccentColor("#50fa7b").SetTextColor("#8be9fd").SetTitle("SYSTEM STATUS").SetFontSize(24).SetBuiltIn(true).SetIsDefault(true).SaveX(themeCtx)
		c2.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).SetTheme(val).SaveX(themeCtx)
		importLegacyTheme(c2, themeCtx)
		if c2.Theme.Query().Where(theme.NameEQ("Imported")).ExistX(themeCtx) {
			t.Errorf("value %q should not create Imported", val)
		}
		c2.Close()
	}
}
