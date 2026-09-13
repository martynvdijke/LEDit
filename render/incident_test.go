package render

import (
	"bytes"
	"testing"
	"time"
)

func incidentScene() IncidentScene {
	return IncidentScene{
		Title:    "Database down",
		Message:  "Primary postgres is unreachable from the app tier",
		Severity: "critical",
		Since:    time.Date(2026, 9, 13, 14, 5, 0, 0, time.Local),
		More:     2,
	}
}

func TestIncidentPNGDimensionsAndDeterminism(t *testing.T) {
	now := time.Date(2026, 9, 13, 14, 5, 30, 0, time.Local)
	for _, sev := range []string{"critical", "warning", "info"} {
		sc := incidentScene()
		sc.Severity = sev
		data, err := IncidentPNG(64, 32, sc, now)
		if err != nil {
			t.Fatalf("severity %s: %v", sev, err)
		}
		img := decodeRGBA(t, data)
		if got := img.Bounds(); got.Dx() != 64 || got.Dy() != 32 {
			t.Fatalf("severity %s: bounds = %v, want 64x32", sev, got)
		}
		again, err := IncidentPNG(64, 32, sc, now)
		if err != nil {
			t.Fatalf("severity %s: %v", sev, err)
		}
		if !bytes.Equal(data, again) {
			t.Fatalf("severity %s: render is not deterministic", sev)
		}
	}
}

func TestIncidentPNGSeverityPaletteDiffers(t *testing.T) {
	now := time.Date(2026, 9, 13, 14, 5, 30, 0, time.Local)
	sc := incidentScene()
	pixels := map[string][3]uint8{}
	for _, sev := range []string{"critical", "warning", "info"} {
		sc.Severity = sev
		data, err := IncidentPNG(64, 32, sc, now)
		if err != nil {
			t.Fatal(err)
		}
		// Interior pixel away from the border and text should be background.
		r, g, b, _ := decodeRGBA(t, data).At(30, 20).RGBA()
		pixels[sev] = [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
	}
	if pixels["critical"] == pixels["warning"] || pixels["warning"] == pixels["info"] || pixels["critical"] == pixels["info"] {
		t.Fatalf("severity backgrounds not distinct: %+v", pixels)
	}
}

func TestIncidentPNGFooterAndMoreAffectOutput(t *testing.T) {
	now := time.Date(2026, 9, 13, 14, 5, 30, 0, time.Local)
	sc := IncidentScene{Title: "T", Message: "M", Severity: "warning", Since: now}
	base, err := IncidentPNG(160, 96, sc, now)
	if err != nil {
		t.Fatal(err)
	}
	sc.More = 2
	withMore, err := IncidentPNG(160, 96, sc, now)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(base, withMore) {
		t.Fatal("More count did not change the rendered frame")
	}
}

func TestIncidentPNGInvalidCanvas(t *testing.T) {
	if _, err := IncidentPNG(0, 32, incidentScene(), time.Now()); err == nil {
		t.Fatal("expected error for zero width")
	}
	if _, err := IncidentPNG(32, 0, incidentScene(), time.Now()); err == nil {
		t.Fatal("expected error for zero height")
	}
}
