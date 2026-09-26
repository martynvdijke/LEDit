package handlers

import "testing"

func TestSameSources(t *testing.T) {
	a := []sourceWithName{{cacheKey: "a:1"}, {cacheKey: "b:2"}}
	b := []sourceWithName{{cacheKey: "a:1"}, {cacheKey: "b:2"}}
	if !sameSources(a, b) {
		t.Fatal("expected true for identical")
	}
	// different order
	c := []sourceWithName{{cacheKey: "b:2"}, {cacheKey: "a:1"}}
	if sameSources(a, c) {
		t.Fatal("expected false for different order")
	}
	// different length
	d := []sourceWithName{{cacheKey: "a:1"}}
	if sameSources(a, d) {
		t.Fatal("expected false for different length")
	}
	// different cacheKey
	e := []sourceWithName{{cacheKey: "a:1"}, {cacheKey: "b:99"}}
	if sameSources(a, e) {
		t.Fatal("expected false for different cacheKey")
	}
	// nil vs nil (both empty)
	if !sameSources(nil, nil) {
		t.Fatal("nil vs nil should be true")
	}
	// Name difference alone should still be true (only cacheKey compared)
	f := []sourceWithName{{cacheKey: "a:1", Name: "foo"}, {cacheKey: "b:2", Name: "bar"}}
	g := []sourceWithName{{cacheKey: "a:1", Name: "other"}, {cacheKey: "b:2", Name: "other2"}}
	if !sameSources(f, g) {
		t.Fatal("different Name should not affect sameSources")
	}
}
