package datasource

import (
	"testing"
	"time"
)

func TestAnalogClockDS(t *testing.T) {
	ds := &AnalogClockDS{}
	for _, size := range []int{32, 64} {
		img, err := ds.GetPNG(size, size)
		if err != nil {
			t.Fatalf("GetPNG(%d) error: %v", size, err)
		}
		if img.Format != "PNG" {
			t.Errorf("format = %q, want PNG", img.Format)
		}
		if len(img.Data) == 0 {
			t.Error("empty PNG data")
		}
	}
}

func TestMatrixRainDS(t *testing.T) {
	ds := &MatrixRainDS{}
	for _, size := range []int{32, 64} {
		img, err := ds.GetPNG(size, size)
		if err != nil {
			t.Fatalf("GetPNG(%d) error: %v", size, err)
		}
		if img.Format != "PNG" {
			t.Errorf("format = %q, want PNG", img.Format)
		}
		if len(img.Data) == 0 {
			t.Error("empty PNG data")
		}
	}
}

func TestCountdownDS(t *testing.T) {
	ds := &CountdownDS{Name: "Deploy", Label: "Launch", Target: time.Now().Add(2 * time.Hour)}
	img, err := ds.GetPNG(64, 32)
	if err != nil {
		t.Fatalf("GetPNG error: %v", err)
	}
	if img.Format != "PNG" {
		t.Errorf("format = %q, want PNG", img.Format)
	}
	if len(img.Data) == 0 {
		t.Error("empty PNG data")
	}

	// Past target still renders (DONE state).
	past := &CountdownDS{Name: "Past", Label: "", Target: time.Now().Add(-time.Minute)}
	img, err = past.GetPNG(64, 32)
	if err != nil {
		t.Fatalf("GetPNG past-target error: %v", err)
	}
	if len(img.Data) == 0 {
		t.Error("empty PNG data for past target")
	}
}
