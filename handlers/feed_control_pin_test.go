package handlers

import "testing"

func TestFeedController_PinUnpin(t *testing.T) {
	fc := &FeedController{}
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected not pinned")
	}
	fc.Pin("genericapi:1", "rule1")
	k, by, ok := fc.IsPinned()
	if !ok || k != "genericapi:1" || by != "rule1" {
		t.Fatalf("pin failed %v %v %v", k, by, ok)
	}
	s := fc.Status()
	if s["pinned_by"] != "rule1" {
		t.Fatalf("status pinned_by %v", s["pinned_by"])
	}
	fc.Unpin()
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected unpinned")
	}
	if _, ok := fc.Status()["pinned_by"]; ok {
		t.Fatal("pinned_by should be absent after unpin")
	}
}

func TestFeedController_NextClearsPin(t *testing.T) {
	fc := &FeedController{}
	fc.Pin("systemstats:0", "r")
	fc.Next()
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("Next should clear pin")
	}
	// also skip should be set
	if !fc.ShouldSkip() {
		t.Fatal("Next should set skip")
	}
}

func TestFeedController_StatusPinned(t *testing.T) {
	fc := &FeedController{}
	fc.Pin("x:1", "myrule")
	m := fc.Status()
	if m["pinned_by"] != "myrule" {
		t.Fatalf("want myrule got %v", m["pinned_by"])
	}
	if m["pinned_key"] != "x:1" {
		t.Fatalf("want x:1 got %v", m["pinned_key"])
	}
}
