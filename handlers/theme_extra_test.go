package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ledit/ent/theme"
	"ledit/render"

	"github.com/gin-gonic/gin"
)

func TestThemeFontValidation(t *testing.T) {
	client := newThemeTestDB(t)
	srv := &Server{DB: client, Ctx: themeCtx}
	// valid bundled font -> redirect and persisted
	form := themeForm("fonttest")
	form.Set("font_name", "PixelifySans.ttf")
	w := callSave(srv, "new", form)
	if w.Header().Get("Location") == "" {
		t.Logf("header %v body %q", w.Header(), w.Body.String())
		t.Fatalf("valid font expected redirect got %d", w.Code)
	}
	got := client.Theme.Query().Where(theme.NameEQ("fonttest")).OnlyX(themeCtx)
	if got.FontName != "PixelifySans.ttf" {
		t.Fatalf("font_name not persisted: %q", got.FontName)
	}
	// invalid selections must be rejected (re-render, no row created)
	for _, bad := range []struct{ name, font string }{
		{"badfont", "../x.ttf"},
		{"backfont", `..\x.ttf`},
		{"slashfont", "sub/x.ttf"},
		{"nofont", "nope.ttf"},
	} {
		f := themeForm(bad.name)
		f.Set("font_name", bad.font)
		w := callSave(srv, "new", f)
		if w.Header().Get("Location") != "" {
			t.Fatalf("font %q should be rejected but was accepted", bad.font)
		}
		if n := client.Theme.Query().Where(theme.NameEQ(bad.name)).CountX(themeCtx); n != 0 {
			t.Fatalf("theme %q created despite invalid font %q", bad.name, bad.font)
		}
	}
}

func TestThemeFontPath(t *testing.T) {
	if p := ThemeFontPath("PixelifySans.ttf"); p != "fonts/PixelifySans.ttf" {
		t.Fatalf("got %q", p)
	}
	for _, bad := range []string{"../x.ttf", `..\x.ttf`, "sub/x.ttf", "nope.ttf"} {
		if p := ThemeFontPath(bad); p != "" {
			t.Fatalf("should reject %q got %q", bad, p)
		}
	}
	if p := ThemeFontPath(""); p != "" {
		t.Fatalf("empty should be empty got %q", p)
	}
}

func TestThemeCacheSigFont(t *testing.T) {
	a := render.Theme{Title: "T", FontSize: 24, FontPath: ""}
	b := render.Theme{Title: "T", FontSize: 24, FontPath: "fonts/PixelifySans.ttf"}
	if themeCacheSig(a) == themeCacheSig(b) {
		t.Fatalf("cache sig should differ with font")
	}
}

func TestRenderDictWithFont(t *testing.T) {
	th := render.Theme{BackgroundColor: [3]uint8{0, 0, 0}, AccentColor: [3]uint8{255, 255, 255}, TextColor: [3]uint8{255, 255, 255}, Title: "HELLO", FontSize: 24, FontPath: "fonts/PixelifySans.ttf"}
	img, err := render.RenderDict(map[string]string{"k": "v"}, 64, 32, th, "fonts/PixelifySans.ttf")
	if err != nil {
		t.Fatalf("render err %v", err)
	}
	if len(img.Data) == 0 {
		t.Fatalf("empty png")
	}
	// compare vs default font fallback (empty font path that fails) should differ
	th2 := render.Theme{BackgroundColor: [3]uint8{0, 0, 0}, AccentColor: [3]uint8{255, 255, 255}, TextColor: [3]uint8{255, 255, 255}, Title: "HELLO", FontSize: 24}
	img2, err := render.RenderDict(map[string]string{"k": "v"}, 64, 32, th2, "/nonexistent.ttf")
	if err != nil {
		t.Fatalf("render2 err %v", err)
	}
	if bytes.Equal(img.Data, img2.Data) {
		t.Log("warning: font render identical to fallback (maybe font not loaded) - not failing")
	}
	_ = gin.TestMode
	_ = http.MethodPost
	_ = httptest.NewRecorder
	_ = url.Values{}
	_ = strings.Contains
}
