package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"ledit/datasource"
	"ledit/ent"
)

// cachedPlugin is a plugin row plus its parsed manifest, kept in a package-level
// cache so the feed and catalog can reach plugins without a DB handle (mirrors
// the alarm/incident singletons). Only the CRUD handlers refresh it.
type cachedPlugin struct {
	ID        int
	Name      string
	Kind      string
	Target    string
	Enabled   bool
	TimeoutMs int
	Manifest  *datasource.PluginManifest
	Config    json.RawMessage
}

var (
	pluginCacheMu sync.RWMutex
	pluginCache   []cachedPlugin
)

// StartPluginManager loads the plugin cache once at startup.
func StartPluginManager(client *ent.Client) {
	reloadPluginCache(client)
}

// reloadPluginCache re-reads all plugin rows. A malformed manifest is tolerated
// (the plugin still invokes, just without a config schema).
func reloadPluginCache(client *ent.Client) {
	if client == nil {
		return
	}
	rows, err := client.DatasourcePlugin.Query().All(context.Background())
	if err != nil {
		slog.Warn("plugin cache reload failed", "error", err)
		return
	}
	list := make([]cachedPlugin, 0, len(rows))
	for _, p := range rows {
		m, err := datasource.ParsePluginManifest(p.Manifest)
		if err != nil {
			slog.Warn("plugin manifest invalid", "plugin", p.ID, "error", err)
			m = nil
		}
		cfg := json.RawMessage(p.Config)
		if len(cfg) == 0 {
			cfg = json.RawMessage(`{}`)
		}
		list = append(list, cachedPlugin{
			ID: p.ID, Name: p.Name, Kind: string(p.Kind), Target: p.Target,
			Enabled: p.Enabled, TimeoutMs: p.TimeoutMs, Manifest: m, Config: cfg,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	pluginCacheMu.Lock()
	pluginCache = list
	pluginCacheMu.Unlock()
}

// cachedPlugins returns a copy of the cache.
func cachedPlugins() []cachedPlugin {
	pluginCacheMu.RLock()
	defer pluginCacheMu.RUnlock()
	out := make([]cachedPlugin, len(pluginCache))
	copy(out, pluginCache)
	return out
}

// pluginSource builds a renderable datasource for a cached plugin.
func pluginSource(p cachedPlugin) *datasource.PluginSource {
	info := datasource.PluginInfo{
		ID: p.ID, Name: p.Name, Kind: p.Kind, Target: p.Target,
		Enabled: p.Enabled, TimeoutMs: p.TimeoutMs,
	}
	return &datasource.PluginSource{PluginID: p.ID, Config: p.Config, Info: &info}
}

// pluginConfigObject merges manifest field defaults with stored config, used by
// the admin form when no explicit values are posted.
func pluginConfigObject(p cachedPlugin) map[string]any {
	obj := map[string]any{}
	for _, f := range pluginFields(p) {
		if f.Default != "" {
			obj[f.Key] = f.Default
		}
	}
	var stored map[string]any
	if err := json.Unmarshal(p.Config, &stored); err == nil {
		for k, v := range stored {
			obj[k] = v
		}
	}
	return obj
}

func pluginFields(p cachedPlugin) []datasource.PluginField {
	if p.Manifest == nil {
		return nil
	}
	return p.Manifest.Fields
}

var _ = fmt.Sprintf
