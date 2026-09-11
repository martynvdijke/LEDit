package datasource

import (
	"bytes"
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

	// Past target renders the Now! state.
	past := &CountdownDS{Name: "Past", Label: "", Target: time.Now().Add(-time.Minute)}
	img, err = past.GetPNG(64, 32)
	if err != nil {
		t.Fatalf("GetPNG past-target error: %v", err)
	}
	if len(img.Data) == 0 {
		t.Error("empty PNG data for past target")
	}

	// Non-default options are honoured: hide renders no text, a custom message
	// renders text, so the two frames differ.
	hide := &CountdownDS{Target: time.Now().Add(-time.Minute), Completion: "hide"}
	hideImg, err := hide.GetPNG(128, 64)
	if err != nil {
		t.Fatalf("GetPNG hide error: %v", err)
	}
	msg := &CountdownDS{Target: time.Now().Add(-time.Minute), Completion: "message", CompletionMessage: "Doors open"}
	msgImg, err := msg.GetPNG(128, 64)
	if err != nil {
		t.Fatalf("GetPNG message error: %v", err)
	}
	if bytes.Equal(hideImg.Data, msgImg.Data) {
		t.Error("hidden completion should not render the custom message text")
	}

	// Count-up mode renders a future target as a zero duration without error.
	up := &CountdownDS{Target: time.Now().Add(time.Hour), Direction: "up", Granularity: "minutes"}
	if _, err := up.GetPNG(64, 32); err != nil {
		t.Fatalf("GetPNG count-up error: %v", err)
	}
}
