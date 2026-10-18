package handlers

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"ledit/datasource"
	"ledit/ent"
	"ledit/render/effects"

	// Registers the byte-identical adapters for the legacy animation code paths
	// (matrix rain, plasma, starfield, DVD) so a preset may name them.
	_ "ledit/render/effects/legacy"
)

// cachedEffectPreset is an effect_preset row kept in a package-level cache so the
// feed, pickers and preview can reach presets without a DB handle (mirrors the
// plugin and alarm singletons). Only the CRUD handlers refresh it.
type cachedEffectPreset struct {
	ID      int
	Name    string
	Effect  string
	Params  string
	BuiltIn bool
}

var (
	effectPresetCacheMu sync.RWMutex
	effectPresetCache   []cachedEffectPreset
)

// seedEffectPresets creates the built-in presets on first run. Seeding is
// skipped as soon as any preset exists, so user edits are never overwritten.
func seedEffectPresets(client *ent.Client, ctx context.Context) {
	if client == nil {
		return
	}
	count, err := client.EffectPreset.Query().Count(ctx)
	if err != nil {
		slog.Warn("effect preset seeding skipped", "error", err)
		return
	}
	if count > 0 {
		return
	}
	builtins := []struct {
		name   string
		effect string
	}{
		{"Plasma", "plasma"},
		{"Ember", "fire"},
		{"Sweep", "scan"},
		{"Static", "noise"},
	}
	for _, b := range builtins {
		desc, _, ok := effects.Lookup(b.effect)
		if !ok {
			continue
		}
		raw, err := effects.MarshalParams(desc.Defaults.StripPalette())
		if err != nil {
			slog.Warn("effect preset seeding failed", "preset", b.name, "error", err)
			continue
		}
		if _, err := client.EffectPreset.Create().
			SetName(b.name).
			SetEffect(b.effect).
			SetParams(string(raw)).
			SetBuiltIn(true).
			Save(ctx); err != nil {
			slog.Warn("effect preset seeding failed", "preset", b.name, "error", err)
		}
	}
}

// StartEffectPresetCache loads the preset cache once at startup.
func StartEffectPresetCache(client *ent.Client) {
	reloadEffectPresetCache(client)
}

// reloadEffectPresetCache re-reads every preset row.
func reloadEffectPresetCache(client *ent.Client) {
	if client == nil {
		return
	}
	rows, err := client.EffectPreset.Query().All(context.Background())
	if err != nil {
		slog.Warn("effect preset cache reload failed", "error", err)
		return
	}
	list := make([]cachedEffectPreset, 0, len(rows))
	for _, p := range rows {
		list = append(list, cachedEffectPreset{
			ID: p.ID, Name: p.Name, Effect: p.Effect, Params: p.Params, BuiltIn: p.BuiltIn,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	effectPresetCacheMu.Lock()
	effectPresetCache = list
	effectPresetCacheMu.Unlock()
}

// cachedEffectPresets returns a copy of the cache.
func cachedEffectPresets() []cachedEffectPreset {
	effectPresetCacheMu.RLock()
	defer effectPresetCacheMu.RUnlock()
	out := make([]cachedEffectPreset, len(effectPresetCache))
	copy(out, effectPresetCache)
	return out
}

// cachedEffectPresetByID returns one preset from the cache.
func cachedEffectPresetByID(id int) (cachedEffectPreset, bool) {
	effectPresetCacheMu.RLock()
	defer effectPresetCacheMu.RUnlock()
	for _, p := range effectPresetCache {
		if p.ID == id {
			return p, true
		}
	}
	return cachedEffectPreset{}, false
}

// presetParams decodes a preset's behaviour parameters (palette is ignored).
func presetParams(p cachedEffectPreset) (effects.Params, error) {
	params, err := effects.UnmarshalParams([]byte(p.Params))
	if err != nil {
		return effects.Params{}, err
	}
	return params.StripPalette(), nil
}

// effectSourceFor builds a renderable datasource for a cached preset.
func effectSourceFor(p cachedEffectPreset) *datasource.EffectDS {
	params, err := presetParams(p)
	if err != nil {
		slog.Warn("effect preset params invalid", "preset", p.ID, "error", err)
	}
	return &datasource.EffectDS{
		PresetID:   p.ID,
		PresetName: p.Name,
		Effect:     p.Effect,
		Params:     params,
	}
}
