package handlers

import (
	"container/list"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"ledit/datasource"
	"ledit/render"
)

// DefaultLKGCapacity bounds the number of cached frames. Worst case is a few
// MB at 400x400 (tens of KB per frame), well within reason.
const DefaultLKGCapacity = 256

// lkgEntry is one cached rendered frame.
type lkgEntry struct {
	key       string
	configSig string
	img       *render.RenderedImage
	rendered  time.Time
}

// LKGCache is a bounded last-known-good cache of rendered frames, keyed by
// "<type>:<id>@<width>x<height>". LRU eviction keeps memory bounded; a
// config-signature mismatch invalidates an entry until the source renders
// successfully with the new configuration.
type LKGCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]*list.Element
	lru     *list.List
	hits    uint64
	misses  uint64
	stale   uint64
}

// NewLKGCache creates a cache bounded to max entries (default 256 when <= 0).
func NewLKGCache(max int) *LKGCache {
	if max <= 0 {
		max = DefaultLKGCapacity
	}
	return &LKGCache{
		max:     max,
		entries: make(map[string]*list.Element),
		lru:     list.New(),
	}
}

// GetPNG returns a rendered frame for key. When get succeeds the frame is
// stored and returned live (stale=false). When get fails and a cached frame
// exists for the same key and config signature, the cached frame is returned
// marked stale (stale=true). When get fails with no usable cache entry, the
// error is returned unchanged so callers keep today's skip/error behavior.
func (c *LKGCache) GetPNG(key, configSig string, get func() (*render.RenderedImage, error)) (*render.RenderedImage, bool, error) {
	img, err := get()
	if err == nil {
		c.store(key, configSig, img)
		return img, false, nil
	}

	// Failure path: try the cache.
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false, err
	}
	e := el.Value.(*lkgEntry)
	if e.configSig != configSig {
		// Config changed: treat as absent until a success with the new config.
		c.misses++
		return nil, false, err
	}
	c.hits++
	c.stale++
	c.lru.MoveToFront(el)
	return e.img, true, nil
}

// StaleAge returns the age in seconds of the cached frame for key, or 0 when
// no entry exists.
func (c *LKGCache) StaleAge(key string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return 0
	}
	return int64(time.Since(el.Value.(*lkgEntry).rendered).Seconds())
}

// Stats returns cumulative hit, miss, and stale-serve counts.
func (c *LKGCache) Stats() (hits, misses, staleServes uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses, c.stale
}

func (c *LKGCache) store(key, configSig string, img *render.RenderedImage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		e := el.Value.(*lkgEntry)
		e.configSig = configSig
		e.img = img
		e.rendered = time.Now()
		c.lru.MoveToFront(el)
		return
	}
	e := &lkgEntry{key: key, configSig: configSig, img: img, rendered: time.Now()}
	el := c.lru.PushFront(e)
	c.entries[key] = el
	if c.lru.Len() > c.max {
		oldest := c.lru.Back()
		if oldest != nil {
			oe := oldest.Value.(*lkgEntry)
			slog.Debug("lkg cache eviction", "key", oe.key)
			c.lru.Remove(oldest)
			delete(c.entries, oe.key)
		}
	}
}

// cfgSig builds a cheap config signature from a source's config fields.
// A separator is embedded so field boundaries are unambiguous.
func cfgSig(parts ...string) string {
	return strings.Join(parts, "\x00")
}

// datasourceConfigSig derives the config signature of a datasource from its
// config fields. Fields that are runtime-only (Resolve closures, Depth) are
// excluded. Unknown datasource types yield "" (cache entry never invalidated
// by config — acceptable fallback).
func datasourceConfigSig(d datasource.Datasource) string {
	switch v := d.(type) {
	case *datasource.SonarrDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.RadarrDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.F1DS:
		return cfgSig(v.Token, v.URL)
	case *datasource.WeatherDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.HomeAssistantDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.UntappdDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.ImageDS:
		return cfgSig(v.Path)
	case *datasource.VideoDS:
		return cfgSig(v.Path)
	case *datasource.CryptoDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.StockDS:
		return cfgSig(v.Token, v.URL)
	case *datasource.SystemStatsDS:
		return cfgSig()
	case *datasource.RssFeedDS:
		return cfgSig(v.URL, v.Name)
	case *datasource.CalendarDS:
		return cfgSig(v.URL, v.Name)
	case *datasource.TextSlideDS:
		return cfgSig(v.Content, v.Color, v.BgColor, strconv.Itoa(v.FontSize))
	case *datasource.GoogleCalendarDS:
		return cfgSig(v.URL, v.Name)
	case *datasource.NewsDS:
		return cfgSig(v.URL, v.Name)
	case *datasource.GenericAPIDS:
		return cfgSig(v.Token, v.URL, v.Config)
	case *datasource.MatrixDS:
		return cfgSig(v.Name, strconv.Itoa(v.Rows), strconv.Itoa(v.Cols), strconv.Itoa(v.Gap), v.Background, datasource.BindingsJSON(v.Bindings))
	case *datasource.AnalogClockDS, *datasource.MatrixRainDS:
		return cfgSig()
	case *datasource.CountdownDS:
		return cfgSig(v.Name, v.Label, v.Target.Format(time.RFC3339))
	case *datasource.AIDigestDS:
		return cfgSig(v.Name, v.Prompt, strings.Join(v.FeedURLs, ","), v.TTL.String())
	default:
		return ""
	}
}

// defaultLKG is the shared last-known-good cache used by all feed connections
// and preview endpoints.
var defaultLKG = NewLKGCache(DefaultLKGCapacity)

// lkgCacheKey builds a cache key for a source at a resolution.
func lkgCacheKey(prefix string, width, height int) string {
	return fmt.Sprintf("%s@%dx%d", prefix, width, height)
}
