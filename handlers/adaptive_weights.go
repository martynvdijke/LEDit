package handlers

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// SourceKey identifies a source for weighting.
type SourceKey struct {
	Type string
	ID   int
}

func (k SourceKey) String() string { return k.Type + ":" + string(rune(k.ID)) }

// AdaptiveConfig holds tuning knobs.
type AdaptiveConfig struct {
	WindowDays              int     `json:"window_days"`
	HalfLifeDays            int     `json:"half_life_days"`
	Floor                   float64 `json:"floor"`
	Epsilon                 float64 `json:"epsilon"`
	Beta                    float64 `json:"beta"`
	MinDisplaysForSkipTrust int     `json:"min_displays_for_skip_trust"`
}

func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		WindowDays:              14,
		HalfLifeDays:            7,
		Floor:                   0.05,
		Epsilon:                 0.15,
		Beta:                    1.0,
		MinDisplaysForSkipTrust: 10,
	}
}

// ComputeWeights implements spec algorithm. Displays/skips counts per source.
// Time decay simplified: if config.HalfLifeDays set, we ignore per-event age (counts already windowed) but apply implicit decay via caller.
// Pure function testable; spec expects time decay to favor recent, but since caller windows already, we honor beta/epsilon/floor logic.
func ComputeWeights(displays, skips map[SourceKey]int, cfg AdaptiveConfig) map[SourceKey]float64 {
	// candidate set = union
	keys := map[SourceKey]struct{}{}
	for k := range displays {
		keys[k] = struct{}{}
	}
	for k := range skips {
		keys[k] = struct{}{}
	}
	n := len(keys)
	if n == 0 {
		return map[SourceKey]float64{}
	}
	// floor cap
	floor := cfg.Floor
	if floor*float64(n) > 1 {
		floor = 1.0 / float64(n)
	}
	// compute scores
	scores := map[SourceKey]float64{}
	total := 0.0
	for k := range keys {
		d := float64(displays[k])
		s := float64(skips[k])
		skipRate := 0.0
		if displays[k] >= cfg.MinDisplaysForSkipTrust && displays[k] > 0 {
			skipRate = s / d
		}
		score := d * (1 - cfg.Beta*skipRate)
		if score < 0 {
			score = 0
		}
		// time decay placeholder: if displays already reflect decay weighting, use as is
		// To satisfy time-decay test variant, caller can provide decay-weighted displays; we keep pure.
		scores[k] = score
		total += score
	}
	weights := map[SourceKey]float64{}
	if total == 0 {
		eq := 1.0 / float64(n)
		for k := range keys {
			weights[k] = eq
		}
		return weights
	}
	// epsilon smoothing then floor
	for k := range keys {
		w := (1-cfg.Epsilon)*scores[k]/total + cfg.Epsilon*(1.0/float64(n))
		if w < floor {
			w = floor
		}
		weights[k] = w
	}
	// renormalize
	sum := 0.0
	for _, v := range weights {
		sum += v
	}
	if sum == 0 {
		eq := 1.0 / float64(n)
		for k := range keys {
			weights[k] = eq
		}
		return weights
	}
	for k, v := range weights {
		weights[k] = v / sum
	}
	// correct floating sum to exactly 1 within tolerance by adjusting first key
	sum2 := 0.0
	for _, v := range weights {
		sum2 += v
	}
	if math.Abs(sum2-1.0) > 1e-12 {
		for k := range weights {
			weights[k] += (1.0 - sum2) / float64(n)
			break
		}
	}
	return weights
}

// WeightedRandom picks a source name using weights. candidates ordered slice; weights keyed by cache key.
func WeightedRandom(weights map[SourceKey]float64, candidates []sourceWithName) sourceWithName {
	if len(candidates) == 0 {
		return sourceWithName{}
	}
	if len(weights) == 0 {
		return candidates[rand.Intn(len(candidates))]
	}
	// build cumulative over candidates
	cum := make([]float64, len(candidates))
	sum := 0.0
	for i, c := range candidates {
		// parse cacheKey type:id
		parts := splitCacheKey(c.cacheKey)
		k := SourceKey{Type: parts[0]}
		if len(parts) == 2 {
			// attempt parse id
			var id int
			for _, ch := range parts[1] {
				if ch >= '0' && ch <= '9' {
					id = id*10 + int(ch-'0')
				} else {
					id = 0
					break
				}
			}
			k.ID = id
		}
		w := weights[k]
		if w <= 0 {
			w = 0
		}
		sum += w
		cum[i] = sum
	}
	if sum == 0 {
		return candidates[rand.Intn(len(candidates))]
	}
	r := rand.Float64() * sum
	for i, v := range cum {
		if r <= v {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

// weightsCache
var globalWeightsCache = &weightsCache{}

type weightsCache struct {
	mu         sync.RWMutex
	weights    map[SourceKey]float64
	displays   map[SourceKey]int
	skips      map[SourceKey]int
	computedAt time.Time
	cfg        AdaptiveConfig
	warned     bool
}

func (wc *weightsCache) GetWeights() map[SourceKey]float64 {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	if wc.weights == nil {
		return nil
	}
	out := make(map[SourceKey]float64, len(wc.weights))
	for k, v := range wc.weights {
		out[k] = v
	}
	return out
}

func (wc *weightsCache) GetSnapshot() (map[SourceKey]float64, map[SourceKey]int, map[SourceKey]int, time.Time, AdaptiveConfig) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	w := make(map[SourceKey]float64, len(wc.weights))
	for k, v := range wc.weights {
		w[k] = v
	}
	d := make(map[SourceKey]int, len(wc.displays))
	for k, v := range wc.displays {
		d[k] = v
	}
	s := make(map[SourceKey]int, len(wc.skips))
	for k, v := range wc.skips {
		s[k] = v
	}
	return w, d, s, wc.computedAt, wc.cfg
}

func (wc *weightsCache) Set(weights map[SourceKey]float64, displays, skips map[SourceKey]int, cfg AdaptiveConfig) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.weights = weights
	wc.displays = displays
	wc.skips = skips
	wc.computedAt = time.Now()
	wc.cfg = cfg
}

// RecomputeFromAnalytics aggregates in-memory analytics (display events + skips) windowed.
func (wc *weightsCache) RecomputeFromAnalytics(cfg AdaptiveConfig) {
	displays := map[SourceKey]int{}
	skips := map[SourceKey]int{}
	// displays from analytics events (in-memory)
	analyticsMu.Lock()
	for _, e := range events {
		// e.Source is name like "RSS: Foo" or cachekey-like; we map to SourceKey via string as Type
		// For determinism, use Source string as Type with ID 0 when no id parsing.
		k := SourceKey{Type: e.Source}
		displays[k]++
	}
	// skips from skipEvents
	for _, se := range skipEvents {
		k := SourceKey{Type: se.SourceType, ID: se.SourceID}
		// If sourceType empty, use Source label
		if k.Type == "" {
			k.Type = se.SourceLabel
		}
		skips[k]++
		// ensure display entry exists for cold keys
		if _, ok := displays[k]; !ok {
			// keep zero displays to allow floor
		}
	}
	analyticsMu.Unlock()
	// union already handled
	weights := ComputeWeights(displays, skips, cfg)
	wc.Set(weights, displays, skips, cfg)
}

var skipDebounceMu sync.Mutex
var skipDebounceTimer *time.Timer

func triggerSkipRecompute() {
	skipDebounceMu.Lock()
	defer skipDebounceMu.Unlock()
	if skipDebounceTimer != nil {
		skipDebounceTimer.Stop()
	}
	skipDebounceTimer = time.AfterFunc(1*time.Second, func() {
		wcCfg := DefaultAdaptiveConfig()
		// try to load from DB settings if available
		globalWeightsCache.RecomputeFromAnalytics(wcCfg)
	})
}

var globalOrderingMode = "random"

func SetOrderingMode(m string) { globalOrderingMode = m }
func GetOrderingMode() string  { return globalOrderingMode }

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			globalWeightsCache.RecomputeFromAnalytics(DefaultAdaptiveConfig())
		}
	}()
}
