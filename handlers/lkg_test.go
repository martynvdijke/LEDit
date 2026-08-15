package handlers

import (
	"errors"
	"testing"

	"ledit/datasource"
	"ledit/render"
)

// wrapGet adapts a GetPNG method value to the wrapper's closure signature.
func wrapGet(fn func(int, int) (*render.RenderedImage, error)) func() (*render.RenderedImage, error) {
	return func() (*render.RenderedImage, error) { return fn(64, 64) }
}

// failingDS always fails GetPNG.
type failingDS struct{}

func (failingDS) GetPNG(int, int) (*render.RenderedImage, error) {
	return nil, errors.New("boom")
}

// okDS renders a fixed frame unless disabled.
type okDS struct {
	disabled bool
}

func (d *okDS) GetPNG(int, int) (*render.RenderedImage, error) {
	if d.disabled {
		return nil, errors.New("boom")
	}
	return &render.RenderedImage{Format: "PNG", Data: []byte("frame")}, nil
}

// toggleDS flips to failing after the first successful call.
type toggleDS struct {
	count int
}

func (d *toggleDS) GetPNG(int, int) (*render.RenderedImage, error) {
	d.count++
	if d.count == 1 {
		return &render.RenderedImage{Format: "PNG", Data: []byte("frame")}, nil
	}
	return nil, errors.New("boom")
}

func TestLKGCacheStoreRetrieve(t *testing.T) {
	c := NewLKGCache(8)
	ds := &okDS{}
	img, stale, err := c.GetPNG("feed:1@64x64", "sig", wrapGet(ds.GetPNG))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stale {
		t.Fatal("first render must not be stale")
	}
	if string(img.Data) != "frame" {
		t.Fatalf("unexpected image data: %q", img.Data)
	}

	ds.disabled = true
	img, stale, err = c.GetPNG("feed:1@64x64", "sig", wrapGet(ds.GetPNG))
	if err != nil {
		t.Fatalf("stale serve must not return error: %v", err)
	}
	if !stale {
		t.Fatal("second render must be stale")
	}
	if string(img.Data) != "frame" {
		t.Fatalf("stale image must match cached frame, got %q", img.Data)
	}
	if age := c.StaleAge("feed:1@64x64"); age < 0 {
		t.Fatalf("stale age must be >= 0, got %d", age)
	}
}

func TestLKGCacheDistinctResolutions(t *testing.T) {
	c := NewLKGCache(8)
	ds := &okDS{}

	// Only the 64x64 resolution is rendered successfully; 128x128 never gets
	// stored (GetPNG for it fails immediately).
	if _, _, err := c.GetPNG("feed:1@64x64", "sig", func() (*render.RenderedImage, error) {
		return ds.GetPNG(64, 64)
	}); err != nil {
		t.Fatalf("64x64 render failed: %v", err)
	}
	if _, _, err := c.GetPNG("feed:1@128x128", "sig", func() (*render.RenderedImage, error) {
		return nil, errors.New("boom")
	}); err == nil {
		t.Fatal("128x128 render must fail")
	}

	// Failing now, but only the 64x64 resolution has a cached frame.
	ds.disabled = true
	if _, stale, err := c.GetPNG("feed:1@64x64", "sig", func() (*render.RenderedImage, error) {
		return ds.GetPNG(64, 64)
	}); err != nil || !stale {
		t.Fatalf("64x64 should serve stale, got stale=%v err=%v", stale, err)
	}
	if _, stale, err := c.GetPNG("feed:1@128x128", "sig", func() (*render.RenderedImage, error) {
		return ds.GetPNG(128, 128)
	}); err == nil || stale {
		t.Fatalf("128x128 has no cache entry, must fail fresh: stale=%v err=%v", stale, err)
	}
}

func TestLKGCacheLRUEviction(t *testing.T) {
	c := NewLKGCache(2)
	ds := &okDS{}

	keys := []string{"k", "k0", "k1", "k2"}
	for _, k := range keys {
		if _, _, err := c.GetPNG(k, "sig", wrapGet(ds.GetPNG)); err != nil {
			t.Fatalf("render %s failed: %v", k, err)
		}
	}
	// 4 keys into a 2-slot cache: only the last two remain.
	ds.disabled = true
	for _, k := range []string{"k1", "k2"} {
		if _, stale, err := c.GetPNG(k, "sig", wrapGet(ds.GetPNG)); err != nil || !stale {
			t.Fatalf("%s must remain cached, stale=%v err=%v", k, stale, err)
		}
	}
	for _, k := range []string{"k", "k0"} {
		if _, stale, err := c.GetPNG(k, "sig", wrapGet(ds.GetPNG)); err == nil || stale {
			t.Fatalf("%s must be evicted, stale=%v err=%v", k, stale, err)
		}
	}
}

func TestLKGCacheConfigSignatureInvalidation(t *testing.T) {
	c := NewLKGCache(8)
	ds := &okDS{}

	if _, _, err := c.GetPNG("feed:1@64x64", "sigA", wrapGet(ds.GetPNG)); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	ds.disabled = true
	// Config changed → cached entry is invalid, must fail fresh.
	if _, stale, err := c.GetPNG("feed:1@64x64", "sigB", wrapGet(ds.GetPNG)); err == nil || stale {
		t.Fatalf("config change must invalidate entry, stale=%v err=%v", stale, err)
	}
	// Original config still matches → stale serve.
	if _, stale, err := c.GetPNG("feed:1@64x64", "sigA", wrapGet(ds.GetPNG)); err != nil || !stale {
		t.Fatalf("sigA must still serve stale, stale=%v err=%v", stale, err)
	}
}

func TestLKGCacheStats(t *testing.T) {
	c := NewLKGCache(8)
	ds := &okDS{}

	c.GetPNG("feed:1@64x64", "sig", wrapGet(ds.GetPNG)) // live
	ds.disabled = true
	c.GetPNG("feed:1@64x64", "sig", wrapGet(ds.GetPNG)) // stale (hit + stale serve)
	c.GetPNG("feed:2@64x64", "sig", wrapGet(ds.GetPNG)) // miss

	hits, misses, stale := c.Stats()
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	if misses != 1 {
		t.Fatalf("misses = %d, want 1", misses)
	}
	if stale != 1 {
		t.Fatalf("stale serves = %d, want 1", stale)
	}
}

func TestLKGCacheNoEntryKeepsError(t *testing.T) {
	c := NewLKGCache(8)
	ds := &failingDS{}
	_, stale, err := c.GetPNG("feed:9@64x64", "sig", wrapGet(ds.GetPNG))
	if err == nil {
		t.Fatal("expected error when no cache entry exists")
	}
	if stale {
		t.Fatal("no entry → not stale")
	}
}

func TestDatasourceConfigSig(t *testing.T) {
	a := datasourceConfigSig(&datasource.TextSlideDS{Content: "x", Color: "red", BgColor: "black", FontSize: 32})
	b := datasourceConfigSig(&datasource.TextSlideDS{Content: "x", Color: "red", BgColor: "black", FontSize: 32})
	if a != b {
		t.Fatalf("identical configs must produce identical signatures: %q vs %q", a, b)
	}
	c := datasourceConfigSig(&datasource.TextSlideDS{Content: "y", Color: "red", BgColor: "black", FontSize: 32})
	if a == c {
		t.Fatal("different configs must produce different signatures")
	}
}
