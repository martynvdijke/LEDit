package datasource

import (
	"bytes"
	"testing"
	"time"
)

func TestVisualizerBinsSetFreshAndClear(t *testing.T) {
	SetVisualizerBins(nil)
	if _, ok := FreshVisualizerBins(time.Second); ok {
		t.Fatal("no bins must not be fresh")
	}

	SetVisualizerBins([]int{1, 2, 3})
	bins, ok := FreshVisualizerBins(time.Second)
	if !ok || len(bins) != 3 || bins[0] != 1 {
		t.Fatalf("expected fresh 3 bins, got %v ok=%v", bins, ok)
	}

	// Stored slice is a copy: mutating the caller's slice must not leak in.
	src := []int{9, 9, 9}
	SetVisualizerBins(src)
	src[0] = 0
	bins, _ = FreshVisualizerBins(time.Second)
	if bins[0] != 9 {
		t.Fatalf("expected stored copy, got %v", bins)
	}

	SetVisualizerBins(nil)
	if _, ok := FreshVisualizerBins(time.Second); ok {
		t.Fatal("cleared bins must not be fresh")
	}
}

func TestVisualizerBinsExpire(t *testing.T) {
	SetVisualizerBins([]int{1, 2, 3})
	time.Sleep(5 * time.Millisecond)
	if _, ok := FreshVisualizerBins(time.Millisecond); ok {
		t.Fatal("expected bins to be stale after maxAge")
	}
	SetVisualizerBins(nil)
}

func TestVisualizerDSRendersFromBinsVsSynthetic(t *testing.T) {
	oldTTL := visualizerBinTTL
	visualizerBinTTL = time.Second
	defer func() {
		visualizerBinTTL = oldTTL
		SetVisualizerBins(nil)
	}()

	ds := &VisualizerDS{Mode: "bars"}
	SetVisualizerBins(nil)
	synthetic, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("synthetic render: %v", err)
	}
	if len(synthetic.Data) == 0 {
		t.Fatal("synthetic render produced no data")
	}

	bins := make([]int, 16)
	for i := range bins {
		bins[i] = 255
	}
	SetVisualizerBins(bins)
	fromBins, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("bins render: %v", err)
	}
	if bytes.Equal(synthetic.Data, fromBins.Data) {
		t.Fatal("bins render should differ from the synthetic tempo model")
	}
}

func TestVisualizerDSFallsBackAfterTimeout(t *testing.T) {
	oldTTL := visualizerBinTTL
	visualizerBinTTL = 5 * time.Millisecond
	defer func() {
		visualizerBinTTL = oldTTL
		SetVisualizerBins(nil)
	}()

	ds := &VisualizerDS{Mode: "bars"}
	SetVisualizerBins(make([]int, 16))
	time.Sleep(20 * time.Millisecond)

	// Stale bins must not be consulted...
	if _, ok := FreshVisualizerBins(visualizerBinTTL); ok {
		t.Fatal("bins should be stale")
	}
	// ...and rendering must still succeed (synthetic fallback, never stalls).
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback render: %v", err)
	}
	if len(img.Data) == 0 {
		t.Fatal("fallback render produced no data")
	}
}
