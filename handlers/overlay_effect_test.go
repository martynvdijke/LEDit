package handlers

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ledit/ent/devicesettings"
	"ledit/render"
	"ledit/render/effects"
)

func testOverlayFramePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode frame: %v", err)
	}
	return buf.Bytes()
}

func decodePNG(t *testing.T, data []byte) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, img.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}

func stripLuma(img *image.RGBA, height int) int {
	b := img.Bounds()
	total := 0
	for y := b.Max.Y - height; y < b.Max.Y; y++ {
		for x := 0; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			total += int(c.R) + int(c.G) + int(c.B)
		}
	}
	return total
}

func TestParseOverlayEffectPresetID(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	valid := presetIDByName(t, srv, "Plasma")

	cases := []struct {
		name string
		raw  string
		want *int
	}{
		{"empty", "", nil},
		{"zero", "0", nil},
		{"not a number", "abc", nil},
		{"negative", "-3", nil},
		{"unknown preset", "999999", nil},
		{"valid", itoa(valid), &valid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gc, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest(http.MethodPost, "/admin/devices/new", strings.NewReader("overlay_effect_preset_id="+tc.raw))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			gc.Request = req
			got := parseOverlayEffectPresetID(gc)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %d", *got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("expected %d, got %v", *tc.want, got)
			}
		})
	}
}

func TestOverlaySpecResolvesEffectPreset(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	id := presetIDByName(t, srv, "Plasma")

	// A device that names a preset resolves it into the render spec.
	ds := srv.DB.DeviceSettings.Create().
		SetName("effect-strip").
		SetWidth(64).SetHeight(64).
		SetOverlayEnabled(true).
		SetOverlayText("hello").
		SetOverlayEffectPresetID(id).
		SaveX(context.Background())
	spec := overlaySpecForDeviceWithGroup(ds)
	if spec.EffectName != "plasma" {
		t.Fatalf("device overlay effect not resolved: %q", spec.EffectName)
	}
	if spec.EffectParams == "" {
		t.Fatal("resolved overlay effect is missing its parameters")
	}
	if err := render.ValidateOverlaySpec(spec, 64); err != nil {
		t.Fatalf("resolved spec invalid: %v", err)
	}

	// A deleted preset degrades to the solid strip instead of breaking.
	missing := 999999
	broken := srv.DB.DeviceSettings.Create().
		SetName("broken-strip").
		SetWidth(64).SetHeight(64).
		SetOverlayEnabled(true).
		SetOverlayText("hello").
		SetOverlayEffectPresetID(missing).
		SaveX(context.Background())
	if spec := overlaySpecForDeviceWithGroup(broken); spec.EffectName != "" {
		t.Fatalf("missing preset must fall back to the solid strip, got %q", spec.EffectName)
	}
}

func TestOverlaySpecInheritsGroupEffectPreset(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	id := presetIDByName(t, srv, "Ember")

	grp := srv.DB.DeviceGroup.Create().
		SetName("effect-group").
		SetOverlayEnabled(true).
		SetOverlayText("group text").
		SetOverlayEffectPresetID(id).
		SaveX(context.Background())

	dev := srv.DB.DeviceSettings.Create().
		SetName("grouped").
		SetWidth(64).SetHeight(64).
		SetGroupID(grp.ID).
		SaveX(context.Background())

	loaded, err := srv.DB.DeviceSettings.Query().
		Where(devicesettings.ID(dev.ID)).
		WithGroup().
		Only(context.Background())
	if err != nil {
		t.Fatalf("load device with group: %v", err)
	}
	spec := overlaySpecForDeviceWithGroup(loaded)
	if spec.EffectName != "fire" {
		t.Fatalf("group effect preset not inherited: %q", spec.EffectName)
	}
}

// TestOverlayEffectIsDimmedWithTheFrame confirms the brightness dim step runs
// after the strip effect is composited, so a dimmed device also dims its effect.
func TestOverlayEffectIsDimmedWithTheFrame(t *testing.T) {
	data := testOverlayFramePNG(t, 64, 64)
	desc, _, ok := effects.Lookup("plasma")
	if !ok {
		t.Fatal("plasma missing from the effects registry")
	}
	raw, err := effects.MarshalParams(desc.Defaults.StripPalette())
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	spec := render.OverlaySpec{
		Enabled: true, Position: "bottom", Height: 8, Text: "HI",
		Background: "#000000", Foreground: "#ffffff",
		EffectName: "plasma", EffectParams: string(raw),
	}
	composited := frameOverlay(data, spec, effects.DefaultPalette())
	if bytes.Equal(composited, data) {
		t.Fatal("effect overlay did not change the frame")
	}
	dimmed := dimPNGBytes(composited, 40)
	if dimmed == nil {
		t.Fatal("dim produced no frame")
	}
	before := stripLuma(decodePNG(t, composited), 8)
	after := stripLuma(decodePNG(t, dimmed), 8)
	if after >= before {
		t.Fatalf("dim did not darken the effect strip: %d -> %d", before, after)
	}
}

// TestAdminDeviceCreatePersistsOverlayEffect drives the create form and asserts
// the effect preset reference round-trips through the device columns.
func TestAdminDeviceCreatePersistsOverlayEffect(t *testing.T) {
	srv := newGroupTestServer(t)
	cookie := loginGroupTest(t, srv)
	id := presetIDByName(t, srv, "Plasma")

	form := "name=EffectDev&ip=127.0.0.1&port=6270&width=64&height=64&refresh_interval=1&enabled=on" +
		"&overlay_enabled=on&overlay_text=OVL&overlay_position=top&overlay_height=10" +
		"&overlay_speed_px=25&overlay_bg=%23ff0000&overlay_fg=%23ffffff" +
		"&overlay_effect_preset_id=" + itoa(id)
	req := httptest.NewRequest(http.MethodPost, "/admin/devices/new", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("create device: %d %s", w.Code, w.Body.String())
	}
	d, err := srv.DB.DeviceSettings.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("query device: %v", err)
	}
	if d.OverlayEffectPresetID == nil || *d.OverlayEffectPresetID != id {
		t.Fatalf("overlay effect preset not persisted: %v", d.OverlayEffectPresetID)
	}
	if spec := overlaySpecForDeviceWithGroup(d); spec.EffectName != "plasma" {
		t.Fatalf("persisted reference does not resolve: %q", spec.EffectName)
	}

	// Submitting "0" clears the reference again.
	form = strings.Replace(form, "&overlay_effect_preset_id="+itoa(id), "&overlay_effect_preset_id=0", 1)
	req = httptest.NewRequest(http.MethodPost, "/admin/devices/"+itoa(d.ID)+"/edit", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("update device: %d %s", w.Code, w.Body.String())
	}
	updated, err := srv.DB.DeviceSettings.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if updated.OverlayEffectPresetID != nil {
		t.Fatalf("clearing the select must persist nil, got %v", *updated.OverlayEffectPresetID)
	}
}
