package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
	"ledit/ent/scene"
	"ledit/render"
)

// ---------------------------------------------------------------------------
// Scene model (plain, resolution-time view)
// ---------------------------------------------------------------------------

// SceneCondition is one sensor comparison: HA entity_id, operator, threshold.
type SceneCondition struct {
	EntityID string `json:"entity_id"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// TriggerGroup is an all-of / any-of group of conditions.
type TriggerGroup struct {
	Op         string           `json:"op"`
	Conditions []SceneCondition `json:"conditions"`
}

type SceneControl struct {
	SourceType string            `json:"source_type"`
	SourceID   int               `json:"source_id"`
	Action     string            `json:"action"`
	Params     map[string]string `json:"params,omitempty"`
}

// SceneActions is the bundled effect applied atomically on activation.
type SceneActions struct {
	SourceType      string         `json:"source_type,omitempty"`
	SourceID        int            `json:"source_id,omitempty"`
	PlaylistID      *int           `json:"playlist_id,omitempty"`
	BrightnessLevel *int           `json:"brightness_level,omitempty"`
	OverlayText     string         `json:"overlay_text,omitempty"`
	Controls        []SceneControl `json:"controls,omitempty"`
}

// Scene is the plain value type used by the pure resolver and manager. The ent
// row is converted into this shape so evaluation needs no DB types.
type Scene struct {
	ID          int
	Name        string
	Enabled     bool
	Priority    int
	TTLSeconds  *int
	Triggers    []TriggerGroup
	Actions     SceneActions
	ActivatedAt time.Time
}

var sceneOperators = []string{"==", "!=", ">", ">=", "<", "<=", "in", "not in"}

func validSceneOperator(op string) bool {
	for _, o := range sceneOperators {
		if o == op {
			return true
		}
	}
	return false
}

// parseSceneTriggers decodes the triggers JSON column. Empty/null is no groups.
func parseSceneTriggers(s string) ([]TriggerGroup, error) {
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "null" {
		return nil, nil
	}
	var out []TriggerGroup
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("invalid triggers JSON: %w", err)
	}
	return out, nil
}

// parseSceneActions decodes the actions JSON column.
func parseSceneActions(s string) (SceneActions, error) {
	var a SceneActions
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "null" {
		return a, nil
	}
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return a, fmt.Errorf("invalid actions JSON: %w", err)
	}
	return a, nil
}

func sceneFromEnt(r *ent.Scene) (Scene, error) {
	triggers, err := parseSceneTriggers(r.Triggers)
	if err != nil {
		return Scene{}, err
	}
	actions, err := parseSceneActions(r.Actions)
	if err != nil {
		return Scene{}, err
	}
	return Scene{
		ID:         r.ID,
		Name:       r.Name,
		Enabled:    r.Enabled,
		Priority:   r.Priority,
		TTLSeconds: r.TTLSeconds,
		Triggers:   triggers,
		Actions:    actions,
	}, nil
}

// ---------------------------------------------------------------------------
// Pure evaluators
// ---------------------------------------------------------------------------

// EvaluateCondition compares a sensor state to a threshold. Operators are
// ==, !=, >, >=, <, <=, in, not in. Numeric coercion is attempted first for
// ordering/comparison; if either side is non-numeric, ==/!= fall back to string
// equality and ordering operators are false. Unknown/missing states must be
// filtered by the caller (see conditionHolds).
func EvaluateCondition(entityState, operator, threshold string) bool {
	switch operator {
	case "in":
		return stringInList(entityState, threshold)
	case "not in":
		return !stringInList(entityState, threshold)
	}
	if entityState == "" {
		return false
	}
	ef, eok := parseSceneNumber(entityState)
	tf, tok := parseSceneNumber(threshold)
	switch operator {
	case "==":
		if eok && tok {
			return ef == tf
		}
		return entityState == threshold
	case "!=":
		if eok && tok {
			return ef != tf
		}
		return entityState != threshold
	case ">":
		return eok && tok && ef > tf
	case ">=":
		return eok && tok && ef >= tf
	case "<":
		return eok && tok && ef < tf
	case "<=":
		return eok && tok && ef <= tf
	default:
		return false
	}
}

func parseSceneNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// stringInList reports whether state matches any comma-separated value in list.
func stringInList(state, list string) bool {
	for _, v := range strings.Split(list, ",") {
		if strings.TrimSpace(v) == state {
			return true
		}
	}
	return false
}

// conditionHolds resolves one condition against the states snapshot. A missing,
// empty, unknown, or unavailable entity evaluates false and is reported as
// unknown by the admin view.
func conditionHolds(states map[string]string, c SceneCondition) bool {
	state, present := states[c.EntityID]
	if !present {
		return false
	}
	switch state {
	case "", "unknown", "unavailable":
		return false
	}
	return EvaluateCondition(state, c.Operator, c.Value)
}

// TriggersHold reports whether every group holds: all-of requires all
// conditions, any-of requires at least one. Groups are ANDed. A scene with no
// triggers (or an empty group) never holds.
func TriggersHold(triggers []TriggerGroup, states map[string]string) bool {
	if len(triggers) == 0 {
		return false
	}
	for _, g := range triggers {
		if len(g.Conditions) == 0 {
			return false
		}
		if g.Op == "any-of" {
			ok := false
			for _, c := range g.Conditions {
				if conditionHolds(states, c) {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
			continue
		}
		// Default / all-of.
		for _, c := range g.Conditions {
			if !conditionHolds(states, c) {
				return false
			}
		}
	}
	return true
}

// ResolveActiveScene returns the enabled, holding scene with the highest
// Priority. Equal priorities are broken by the most recent ActivatedAt (a zero
// timestamp loses); an exact tie falls back to the lowest ID for determinism.
// Suppressed scenes are skipped. now is accepted for signature parity with the
// evaluator tick.
func ResolveActiveScene(now time.Time, scenes []Scene, states map[string]string, suppressed map[string]bool) *Scene {
	_ = now
	var best *Scene
	for i := range scenes {
		s := &scenes[i]
		if !s.Enabled {
			continue
		}
		if suppressed != nil && (suppressed[strconv.Itoa(s.ID)] || suppressed[s.Name]) {
			continue
		}
		if !TriggersHold(s.Triggers, states) {
			continue
		}
		if best == nil {
			best = s
			continue
		}
		if s.Priority > best.Priority {
			best = s
			continue
		}
		if s.Priority == best.Priority {
			if s.ActivatedAt.After(best.ActivatedAt) {
				best = s
			} else if s.ActivatedAt.Equal(best.ActivatedAt) && s.ID < best.ID {
				best = s
			}
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

// ---------------------------------------------------------------------------
// Ambient state cache (Home Assistant entity states)
// ---------------------------------------------------------------------------

const ambientRefreshInterval = 5 * time.Second

var (
	ambientMu    sync.RWMutex
	ambientCache = map[string]string{}
	ambientAt    time.Time
	// AmbientStateFetcher is overridable in tests. Nil falls back to the
	// configured Home Assistant datasource.
	AmbientStateFetcher func(ctx context.Context) (map[string]string, error)
)

// refreshAmbientStates bulk-loads HA entity states at most every
// ambientRefreshInterval so the 1s evaluator tick does not hammer HA. Failures
// leave the previous snapshot intact (last-known-good).
func refreshAmbientStates(ctx context.Context, client *ent.Client) {
	ambientMu.RLock()
	fresh := !ambientAt.IsZero() && time.Since(ambientAt) < ambientRefreshInterval
	ambientMu.RUnlock()
	if fresh {
		return
	}
	fetch := AmbientStateFetcher
	if fetch == nil {
		fetch = func(ctx context.Context) (map[string]string, error) {
			return fetchHAStates(ctx, client)
		}
	}
	states, err := fetch(ctx)
	if err != nil {
		slog.Debug("ambient state fetch failed", "error", err)
		// Stamp the attempt so a persistently down HA is not retried every tick.
		ambientMu.Lock()
		ambientAt = time.Now()
		ambientMu.Unlock()
		return
	}
	ambientMu.Lock()
	ambientCache = states
	ambientAt = time.Now()
	ambientMu.Unlock()
}

// GetEntityState returns the cached state for an HA entity id. Entities that are
// absent, empty, unknown, or unavailable report not-found (false).
func GetEntityState(entityID string) (string, bool) {
	ambientMu.RLock()
	defer ambientMu.RUnlock()
	v, ok := ambientCache[entityID]
	if !ok || v == "" || v == "unknown" || v == "unavailable" {
		return "", false
	}
	return v, true
}

// ambientStatesSnapshot copies the cached states for pure evaluation.
func ambientStatesSnapshot() map[string]string {
	ambientMu.RLock()
	defer ambientMu.RUnlock()
	out := make(map[string]string, len(ambientCache))
	for k, v := range ambientCache {
		out[k] = v
	}
	return out
}

// SetAmbientStates injects a states snapshot (tests and admin preview).
func SetAmbientStates(states map[string]string) {
	ambientMu.Lock()
	ambientCache = states
	ambientAt = time.Now()
	ambientMu.Unlock()
}

// ambientEntityIDs returns the sorted known entity ids (admin picker hints).
func ambientEntityIDs() []string {
	ambientMu.RLock()
	defer ambientMu.RUnlock()
	out := make([]string, 0, len(ambientCache))
	for k := range ambientCache {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func fetchHAStates(ctx context.Context, client *ent.Client) (map[string]string, error) {
	if client == nil {
		return nil, fmt.Errorf("no database client")
	}
	ha, err := client.HomeAssistant.Query().First(ctx)
	if err != nil || ha == nil {
		return nil, fmt.Errorf("homeassistant not configured")
	}
	ds := &datasource.HomeAssistantDS{Token: ha.Token, URL: ha.URL}
	raw, err := ds.CurrentState(ctx)
	if err != nil {
		return nil, err
	}
	return parseHAStates(raw), nil
}

func parseHAStates(raw map[string]any) map[string]string {
	out := map[string]string{}
	arr, ok := raw["states"].([]any)
	if !ok {
		return out
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["entity_id"].(string)
		st, _ := m["state"].(string)
		if id == "" {
			continue
		}
		out[id] = st
	}
	return out
}

// ---------------------------------------------------------------------------
// SceneManager
// ---------------------------------------------------------------------------

// SceneManager is the in-memory source of truth for the active scene, its
// pending suppression set, preview window, and cached definitions. All state is
// reversible: Clear/unpin restores the prior feed behavior.
type SceneManager struct {
	mu             sync.Mutex
	scenes         []Scene
	states         map[string]string
	active         *Scene
	activatedAt    time.Time
	activeSource   *sourceWithName
	suppressed     map[string]bool
	activatedAtMap map[int]time.Time
	preview        *Scene
	previewUntil   time.Time
	previewSource  *sourceWithName
}

// NewSceneManager creates an empty manager.
func NewSceneManager() *SceneManager {
	return &SceneManager{
		states:         map[string]string{},
		suppressed:     map[string]bool{},
		activatedAtMap: map[int]time.Time{},
	}
}

// SetScenes replaces the cached scene definitions.
func (m *SceneManager) SetScenes(scenes []Scene) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scenes = scenes
}

// SetStates replaces the ambient states snapshot used for evaluation.
func (m *SceneManager) SetStates(states map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if states == nil {
		states = map[string]string{}
	}
	m.states = states
}

// Scenes returns a copy of the cached definitions.
func (m *SceneManager) Scenes() []Scene {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Scene, len(m.scenes))
	copy(out, m.scenes)
	return out
}

// ActiveScene returns the active (non-preview) scene. Copy under lock.
func (m *SceneManager) ActiveScene() (Scene, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return Scene{}, false
	}
	return *m.active, true
}

// Active returns the active (non-preview) scene, if any.
func (m *SceneManager) Active() (*Scene, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, false
	}
	cp := *m.active
	return &cp, true
}

// IsActive reports whether the given scene id is currently active.
func (m *SceneManager) IsActive(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil && m.active.ID == id
}

func (m *SceneManager) effectiveSceneLocked(now time.Time) *Scene {
	if m.preview != nil {
		if now.Before(m.previewUntil) {
			return m.preview
		}
		m.preview = nil
		m.previewSource = nil
	}
	return m.active
}

// ActiveSource returns the source to pin for the current scene tier (preview
// first), or nil when no scene is holding.
func (m *SceneManager) ActiveSource() *sourceWithName {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.effectiveSceneLocked(time.Now()); s != nil {
		if m.preview != nil && s == m.preview {
			return m.previewSource
		}
		return m.activeSource
	}
	return nil
}

// BrightnessLevel returns the scene brightness action, or nil.
func (m *SceneManager) BrightnessLevel(now time.Time) *int {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.effectiveSceneLocked(now)
	if s == nil || s.Actions.BrightnessLevel == nil {
		return nil
	}
	v := *s.Actions.BrightnessLevel
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return &v
}

// Overlay returns the scene overlay spec, or false when no overlay text is set.
func (m *SceneManager) Overlay(now time.Time) (render.OverlaySpec, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.effectiveSceneLocked(now)
	if s == nil || strings.TrimSpace(s.Actions.OverlayText) == "" {
		return render.OverlaySpec{}, false
	}
	spec := render.DefaultOverlaySpec()
	spec.Enabled = true
	spec.Text = s.Actions.OverlayText
	return spec, true
}

// SuppressActive clears the active/preview scene and suppresses re-activation
// of that scene until its triggers next transition false -> true.
func (m *SceneManager) SuppressActive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.preview != nil {
		m.preview = nil
		m.previewSource = nil
	}
	if m.active == nil {
		return
	}
	m.suppressed[strconv.Itoa(m.active.ID)] = true
	m.active = nil
	m.activeSource = nil
}

// Preview applies a scene transiently for d without touching activation/TTL/
// suppression bookkeeping. It returns false when the scene has no resolvable
// action.
func (m *SceneManager) Preview(sc Scene, resolve func(*Scene) (*sourceWithName, bool), now time.Time, d time.Duration) bool {
	if resolve == nil {
		return false
	}
	src, ok := resolve(&sc)
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := sc
	m.preview = &cp
	m.previewUntil = now.Add(d)
	m.previewSource = src
	return true
}

// Evaluate recomputes the active scene at now. resolve is invoked only on a
// transition to a new winner so the source catalog is not hit every tick. It
// returns true when the published scene source changed (activated, cleared,
// preempted, or a preview expired).
func (m *SceneManager) Evaluate(now time.Time, resolve func(*Scene) (*sourceWithName, bool)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := false
	if m.preview != nil && !now.Before(m.previewUntil) {
		m.preview = nil
		m.previewSource = nil
		changed = true
	}

	states := m.states
	if states == nil {
		states = map[string]string{}
	}

	cands := make([]Scene, len(m.scenes))
	copy(cands, m.scenes)
	for i := range cands {
		if t, ok := m.activatedAtMap[cands[i].ID]; ok {
			cands[i].ActivatedAt = t
		}
	}

	// Rising/falling edges: a suppressed scene is released once its triggers
	// stop holding, so it can win again on the next rising edge.
	holds := map[int]bool{}
	for i := range cands {
		holds[cands[i].ID] = TriggersHold(cands[i].Triggers, states)
	}
	for key := range m.suppressed {
		id, err := strconv.Atoi(key)
		if err == nil && !holds[id] {
			delete(m.suppressed, key)
		}
	}

	winner := ResolveActiveScene(now, cands, states, m.suppressed)

	// TTL expiry: suppress until the next rising edge so a still-holding scene
	// does not immediately reactivate.
	if m.active != nil {
		a := m.active
		if a.TTLSeconds != nil && !now.Before(m.activatedAt.Add(time.Duration(*a.TTLSeconds)*time.Second)) {
			m.suppressed[strconv.Itoa(a.ID)] = true
			winner = ResolveActiveScene(now, cands, states, m.suppressed)
		}
	}

	if m.active != nil && winner != nil && winner.ID == m.active.ID {
		return changed
	}
	if m.active == nil && winner == nil {
		return changed
	}
	if winner == nil {
		m.active = nil
		m.activeSource = nil
		return true
	}
	if resolve == nil {
		return changed
	}
	src, ok := resolve(winner)
	if !ok {
		// Unresolvable action: never hold a half-applied scene.
		if m.active != nil {
			m.active = nil
			m.activeSource = nil
			return true
		}
		return changed
	}
	cp := *winner
	m.active = &cp
	m.activatedAt = now
	m.activatedAtMap[cp.ID] = now
	m.activeSource = src
	return true
}

// selectTierSource picks the special-tier source for a feed: an active wake
// alarm outranks an active scene. Notifications and incidents are handled
// earlier in serveFeed and therefore outrank both.
func selectTierSource(alarmSrc, sceneSrc *sourceWithName) *sourceWithName {
	if alarmSrc != nil {
		return alarmSrc
	}
	return sceneSrc
}

// globalSceneManager is the process-wide scene state read by the feed path and
// written by the event-rule evaluator.
var globalSceneManager = NewSceneManager()

// ActiveSceneSource returns the current scene-tier source, or nil.
func ActiveSceneSource() *sourceWithName { return globalSceneManager.ActiveSource() }

// ActiveSceneBrightnessLevel returns the active scene's brightness action, or nil.
func ActiveSceneBrightnessLevel(now time.Time) *int {
	return globalSceneManager.BrightnessLevel(now)
}

// ActiveSceneOverlay returns the active scene's overlay spec, or false.
func ActiveSceneOverlay(now time.Time) (render.OverlaySpec, bool) {
	return globalSceneManager.Overlay(now)
}

// SuppressActiveScene clears the active scene and blocks re-activation until
// the next rising edge, then broadcasts the cleared pin to live feeds.
func SuppressActiveScene() {
	globalSceneManager.SuppressActive()
	sceneAll(nil)
}

// PreviewScene applies a scene transiently (default window) via the manager.
func PreviewScene(sc Scene, resolve func(*Scene) (*sourceWithName, bool), d time.Duration) bool {
	return globalSceneManager.Preview(sc, resolve, time.Now(), d)
}

// ---------------------------------------------------------------------------
// Definition loading and source resolution
// ---------------------------------------------------------------------------

// SceneSourceResolver resolves a scene's action into a feed source; overridable
// in tests.
var SceneSourceResolver func(s *Scene) (*sourceWithName, bool)

func loadSceneDefs(ctx context.Context, client *ent.Client) []Scene {
	if client == nil {
		return nil
	}
	rows, err := client.Scene.Query().Where(scene.EnabledEQ(true)).Order(ent.Asc(scene.FieldID)).All(ctx)
	if err != nil {
		slog.Error("event evaluator: failed to load scenes", "error", err)
		return nil
	}
	out := make([]Scene, 0, len(rows))
	for _, r := range rows {
		s, perr := sceneFromEnt(r)
		if perr != nil {
			slog.Warn("scene parse error, skipping", "scene", r.Name, "error", perr)
			continue
		}
		out = append(out, s)
	}
	return out
}

// sceneSourceIndex loads GeneralSettings plus AI config to build a source index,
// mirroring the alarm resolver.
func sceneSourceIndex(client *ent.Client) (*sourceIndex, bool) {
	if client == nil {
		return nil, false
	}
	ctx := context.Background()
	gs, err := client.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithGenericApis().WithHomeAssistant().WithWeather().WithSonarr().WithRadarr().WithF1().
		WithUntappd().WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		WithGoogleCalendars().WithNewsFeeds().WithMatrixLayouts().WithCountdowns().WithAiDigests().
		WithImages().WithVideos().WithTransits().WithUptimes().WithPiholes().WithGithubs().
		WithSports().WithSunmoons().WithJellyfins().WithQrcodes().WithNowPlayingSources().WithImmichs().WithQbittorrents().WithSabnzbd().WithOverseerrs().WithUptimeKumas().WithSpeedtests().WithAdguards().WithFrigates().WithZigbee2mqtts().WithTransmissions().WithProxmoxs().WithWastes().WithAirqualities().WithParcels().Only(ctx)
	if err != nil || gs == nil {
		return nil, false
	}
	aiCfg := datasource.AIConfig{}
	if ai, aerr := client.AISettings.Query().Only(ctx); aerr == nil && ai != nil {
		aiCfg = datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
	}
	return buildSourceIndex(gs, aiCfg), true
}

// resolveSceneSource resolves a scene's action to a single pinned source.
// source_ref wins over playlist_id. A playlist-only scene pins its first
// resolvable item. Returns false when nothing resolves, so the manager never
// activates a half-applied scene.
//
// ponytail: playlist actions pin one item instead of rotating the playlist;
// full in-slot playlist rotation needs a multi-source feed tier.
func resolveSceneSource(client *ent.Client, s *Scene) (*sourceWithName, bool) {
	idx, ok := sceneSourceIndex(client)
	if !ok {
		return nil, false
	}
	resolveItem := func(srcType string, srcID int) (*sourceWithName, bool) {
		src, name, err := idx.Resolve(srcType, srcID)
		if err != nil {
			slog.Warn("scene source not resolvable", "scene", s.Name, "source", fmt.Sprintf("%s:%d", srcType, srcID), "error", err)
			return nil, false
		}
		return &sourceWithName{Name: name, Source: src, cacheKey: fmt.Sprintf("%s:%d", srcType, srcID)}, true
	}
	if s.Actions.SourceType != "" {
		return resolveItem(s.Actions.SourceType, s.Actions.SourceID)
	}
	if s.Actions.PlaylistID != nil {
		pl, err := client.Playlist.Get(context.Background(), *s.Actions.PlaylistID)
		if err != nil || !pl.Enabled {
			slog.Warn("scene playlist not resolvable", "scene", s.Name, "playlist_id", *s.Actions.PlaylistID)
			return nil, false
		}
		items, err := datasource.ParsePlaylistItems(pl.Items)
		if err != nil || len(items) == 0 {
			slog.Warn("scene playlist has no items", "scene", s.Name, "playlist_id", pl.ID)
			return nil, false
		}
		return resolveItem(items[0].SourceType, items[0].SourceID)
	}
	return nil, false
}

func runSceneControls(ctx context.Context, client *ent.Client, sc Scene) {
	controls := sc.Actions.Controls
	if len(controls) > 10 {
		controls = controls[:10]
	}
	for _, ctrl := range controls {
		idx, ok := sceneSourceIndex(client)
		if !ok {
			slog.Warn("scene control: no source index", "scene", sc.Name, "source_type", ctrl.SourceType, "source_id", ctrl.SourceID, "action", ctrl.Action)
			continue
		}
		ds, _, err := idx.Resolve(ctrl.SourceType, ctrl.SourceID)
		if err != nil {
			slog.Warn("scene control: source not resolvable", "scene", sc.Name, "source_type", ctrl.SourceType, "source_id", ctrl.SourceID, "action", ctrl.Action, "error", err)
			continue
		}
		act, ok := ds.(datasource.Actuator)
		if !ok {
			slog.Warn("scene control: not actuatable", "scene", sc.Name, "source_type", ctrl.SourceType, "source_id", ctrl.SourceID, "action", ctrl.Action)
			continue
		}
		actCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = act.Actuate(actCtx, ctrl.Action, ctrl.Params)
		cancel()
		if err != nil {
			slog.Warn("scene control: actuate failed", "scene", sc.Name, "source_type", ctrl.SourceType, "source_id", ctrl.SourceID, "action", ctrl.Action, "error", err)
		}
	}
}

// evaluateScenesTick runs the scene pass for one evaluator tick: refresh the
// ambient snapshot, resolve the active scene, and broadcast on transition.
func evaluateScenesTick(ctx context.Context, client *ent.Client, now time.Time) bool {
	refreshAmbientStates(ctx, client)
	globalSceneManager.SetStates(ambientStatesSnapshot())
	changed := globalSceneManager.Evaluate(now, SceneSourceResolver)
	if changed {
		sceneAll(globalSceneManager.ActiveSource())
		// ponytail: rule-triggered scene previews and manual previews skip controls; only real scene transitions fire controls.
		if sc, ok := globalSceneManager.ActiveScene(); ok && len(sc.Actions.Controls) > 0 {
			runSceneControls(ctx, client, sc)
		}
	}
	return changed
}

// EvaluateScenesOnce is exported for tests: one synchronous scene evaluation.
func EvaluateScenesOnce(client *ent.Client, now time.Time) bool {
	return evaluateScenesTick(context.Background(), client, now)
}
