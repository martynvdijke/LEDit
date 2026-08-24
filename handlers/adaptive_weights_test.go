package handlers

import (
	"math"
	"testing"
)

func TestComputeWeightsColdStart(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	w := ComputeWeights(map[SourceKey]int{}, map[SourceKey]int{}, cfg)
	if len(w) != 0 {
		t.Fatalf("expected empty")
	}
	// with three sources zero displays
	displays := map[SourceKey]int{{Type: "a"}: 0, {Type: "b"}: 0, {Type: "c"}: 0}
	w = ComputeWeights(displays, map[SourceKey]int{}, cfg)
	for _, v := range w {
		if math.Abs(v-1.0/3) > 1e-9 {
			t.Fatalf("cold start not equal %v", w)
		}
	}
}

func TestComputeWeightsSkipPenalty(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	a := SourceKey{Type: "a"}
	b := SourceKey{Type: "b"}
	displays := map[SourceKey]int{a: 20, b: 20}
	skips := map[SourceKey]int{a: 10, b: 0}
	w := ComputeWeights(displays, skips, cfg)
	if w[a] >= w[b] {
		t.Fatalf("expected b > a, got %v", w)
	}
	sum := 0.0
	for _, v := range w {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Fatalf("sum %v", sum)
	}
}

func TestComputeWeightsTrustThreshold(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	a := SourceKey{Type: "a"}
	b := SourceKey{Type: "b"}
	displays := map[SourceKey]int{a: 3, b: 20}
	skips := map[SourceKey]int{a: 2, b: 0}
	w1 := ComputeWeights(displays, skips, cfg)
	// a's skip should be ignored, so weight proportional mainly to displays
	if w1[a] == 0 {
		t.Fatalf("a weight zero")
	}
}

func TestComputeWeightsFloorAndSum(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	cfg.Floor = 0.05
	displays := map[SourceKey]int{{Type: "a"}: 100, {Type: "b"}: 1, {Type: "c"}: 1, {Type: "d"}: 1}
	w := ComputeWeights(displays, map[SourceKey]int{}, cfg)
	for _, v := range w {
		if v < 0.04 {
			t.Fatalf("floor violated %v", w)
		}
	}
	sum := 0.0
	for _, v := range w {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Fatalf("sum %v", sum)
	}
}

func TestComputeWeightsFloorCap(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	cfg.Floor = 0.6
	displays := map[SourceKey]int{{Type: "a"}: 10, {Type: "b"}: 10}
	w := ComputeWeights(displays, map[SourceKey]int{}, cfg)
	if math.Abs(w[SourceKey{Type: "a"}]-0.5) > 1e-9 {
		t.Fatalf("floor cap not applied %v", w)
	}
}

func TestSkipAttribution(t *testing.T) {
	// reset
	analyticsMu.Lock()
	skipEvents = nil
	unattributedSkips = 0
	analyticsMu.Unlock()
	GlobalFeed.CurrentName = "news:3"
	GlobalFeed.Next()
	if GetUnattributedSkips() != 0 {
		t.Fatalf("should not be unattributed")
	}
	analyticsMu.Lock()
	if len(skipEvents) != 1 || skipEvents[0].SourceLabel != "news:3" {
		t.Fatalf("skip not recorded %v", skipEvents)
	}
	analyticsMu.Unlock()
	// second skip with different source
	GlobalFeed.CurrentName = "weather:1"
	GlobalFeed.Next()
	analyticsMu.Lock()
	if len(skipEvents) != 2 || skipEvents[1].SourceLabel != "weather:1" {
		t.Fatalf("second skip failed %v", skipEvents)
	}
	analyticsMu.Unlock()
	// unattributed
	GlobalFeed.CurrentName = ""
	GlobalFeed.Next()
	if GetUnattributedSkips() != 1 {
		t.Fatalf("unattributed not counted")
	}
}
