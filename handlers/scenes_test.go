package handlers

import (
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func ptrInt(v int) *int { return &v }

func sceneTestClient(t *testing.T) *ent.Client {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if db := drv.DB(); db != nil {
		db.SetMaxOpenConns(1)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	return client
}

func holdAllOf(entity, value string) []TriggerGroup {
	return []TriggerGroup{{Op: "all-of", Conditions: []SceneCondition{{EntityID: entity, Operator: "==", Value: value}}}}
}

// --- EvaluateCondition -----------------------------------------------------

func TestEvaluateCondition(t *testing.T) {
	cases := []struct {
		state, op, threshold string
		want                 bool
	}{
		{"5", ">", "3", true},
		{"3", ">", "5", false},
		{"5", ">=", "5", true},
		{"2", "<", "10", true}, // numeric, not lexicographic
		{"abc", "==", "abc", true},
		{"abc", "!=", "def", true},
		{"5", "==", "5.0", true}, // numeric coercion
		{"7.5", "<", "8", true},
		{"on", "<", "off", false}, // non-numeric ordering -> false
		{"", "==", "", false},
		{"home", "in", "home,away", true},
		{"work", "in", "home,away", false},
		{"work", "not in", "home,away", true},
	}
	for _, c := range cases {
		if got := EvaluateCondition(c.state, c.op, c.threshold); got != c.want {
			t.Errorf("EvaluateCondition(%q %q %q) = %v, want %v", c.state, c.op, c.threshold, got, c.want)
		}
	}
}

func TestTriggersHold(t *testing.T) {
	states := map[string]string{
		"illuminance": "5",
		"occupancy":   "empty",
		"aqi":         "160",
		"noise":       "40",
	}
	allOf := []TriggerGroup{{Op: "all-of", Conditions: []SceneCondition{
		{EntityID: "illuminance", Operator: "<", Value: "10"},
		{EntityID: "occupancy", Operator: "==", Value: "empty"},
	}}}
	if !TriggersHold(allOf, states) {
		t.Fatal("all-of should hold")
	}
	allOfFail := []TriggerGroup{{Op: "all-of", Conditions: []SceneCondition{
		{EntityID: "illuminance", Operator: "<", Value: "10"},
		{EntityID: "occupancy", Operator: "==", Value: "present"},
	}}}
	if TriggersHold(allOfFail, states) {
		t.Fatal("all-of should not hold when one fails")
	}
	anyOf := []TriggerGroup{{Op: "any-of", Conditions: []SceneCondition{
		{EntityID: "aqi", Operator: ">", Value: "150"},
		{EntityID: "noise", Operator: ">", Value: "70"},
	}}}
	if !TriggersHold(anyOf, states) {
		t.Fatal("any-of should hold on one true")
	}
	anyOfNone := []TriggerGroup{{Op: "any-of", Conditions: []SceneCondition{
		{EntityID: "aqi", Operator: ">", Value: "200"},
		{EntityID: "noise", Operator: ">", Value: "70"},
	}}}
	if TriggersHold(anyOfNone, states) {
		t.Fatal("any-of should not hold when none true")
	}
	// Unknown entity is false.
	unknown := []TriggerGroup{{Op: "all-of", Conditions: []SceneCondition{
		{EntityID: "does_not_exist", Operator: "==", Value: "on"},
	}}}
	if TriggersHold(unknown, states) {
		t.Fatal("unknown entity should evaluate false")
	}
	// Multiple groups are ANDed.
	mixed := []TriggerGroup{allOf[0], anyOf[0]}
	if !TriggersHold(mixed, states) {
		t.Fatal("mixed groups should hold")
	}
	if TriggersHold(nil, states) {
		t.Fatal("no triggers should not hold")
	}
}

// --- ResolveActiveScene ----------------------------------------------------

func TestResolveActiveScenePriority(t *testing.T) {
	hold := holdAllOf("e", "on")
	scenes := []Scene{
		{ID: 1, Name: "low", Enabled: true, Priority: 5, Triggers: hold},
		{ID: 2, Name: "high", Enabled: true, Priority: 20, Triggers: hold},
	}
	got := ResolveActiveScene(time.Now(), scenes, map[string]string{"e": "on"}, nil)
	if got == nil || got.ID != 2 {
		t.Fatalf("expected high priority scene, got %+v", got)
	}
}

func TestResolveActiveSceneRecencyTieBreak(t *testing.T) {
	hold := holdAllOf("e", "on")
	t1 := time.Unix(1000, 0)
	t2 := time.Unix(2000, 0)
	scenes := []Scene{
		{ID: 1, Name: "A", Enabled: true, Priority: 10, ActivatedAt: t1, Triggers: hold},
		{ID: 2, Name: "B", Enabled: true, Priority: 10, ActivatedAt: t2, Triggers: hold},
	}
	got := ResolveActiveScene(time.Now(), scenes, map[string]string{"e": "on"}, nil)
	if got == nil || got.ID != 2 {
		t.Fatalf("expected more recently activated scene B, got %+v", got)
	}
	// Zero vs non-zero: the activated one wins.
	scenes[0].ActivatedAt = t2
	scenes[1].ActivatedAt = time.Time{}
	got = ResolveActiveScene(time.Now(), scenes, map[string]string{"e": "on"}, nil)
	if got == nil || got.ID != 1 {
		t.Fatalf("expected scene with activation timestamp, got %+v", got)
	}
}

func TestResolveActiveSceneDisabledAndSuppressed(t *testing.T) {
	hold := holdAllOf("e", "on")
	states := map[string]string{"e": "on"}
	scenes := []Scene{
		{ID: 1, Name: "A", Enabled: true, Priority: 10, Triggers: hold},
	}
	// Disabled never wins.
	scenes[0].Enabled = false
	if got := ResolveActiveScene(time.Now(), scenes, states, nil); got != nil {
		t.Fatalf("disabled scene should not win, got %+v", got)
	}
	scenes[0].Enabled = true
	if got := ResolveActiveScene(time.Now(), scenes, states, map[string]bool{"1": true}); got != nil {
		t.Fatalf("suppressed scene should not win, got %+v", got)
	}
}

// --- SceneManager ----------------------------------------------------------

func stubSceneResolver(s *Scene) (*sourceWithName, bool) {
	return &sourceWithName{Name: s.Name, cacheKey: "clock:0"}, true
}

func TestSceneManagerActivateAndRevert(t *testing.T) {
	m := NewSceneManager()
	m.SetScenes([]Scene{{ID: 1, Name: "Calm", Enabled: true, Priority: 1, Triggers: holdAllOf("e", "on")}})
	m.SetStates(map[string]string{"e": "on"})
	now := time.Now()
	if !m.Evaluate(now, stubSceneResolver) {
		t.Fatal("expected activation transition")
	}
	if src := m.ActiveSource(); src == nil || src.Name != "Calm" {
		t.Fatalf("expected active source Calm, got %+v", src)
	}
	if lvl := m.BrightnessLevel(now); lvl != nil {
		t.Fatalf("no brightness action expected, got %v", *lvl)
	}
	if _, ok := m.Overlay(now); ok {
		t.Fatal("no overlay action expected")
	}

	// Trigger loss reverts.
	m.SetStates(map[string]string{"e": "off"})
	if !m.Evaluate(now.Add(time.Second), stubSceneResolver) {
		t.Fatal("expected deactivation transition")
	}
	if m.ActiveSource() != nil {
		t.Fatal("expected cleared source after trigger loss")
	}
}

func TestSceneManagerTTLExpiryAndSuppression(t *testing.T) {
	m := NewSceneManager()
	ttl := 60
	m.SetScenes([]Scene{{
		ID: 1, Name: "Timed", Enabled: true, Priority: 1, TTLSeconds: &ttl,
		Triggers: holdAllOf("e", "on"),
		Actions:  SceneActions{BrightnessLevel: ptrInt(30), OverlayText: "Breathe"},
	}})
	m.SetStates(map[string]string{"e": "on"})
	t0 := time.Now()
	if !m.Evaluate(t0, stubSceneResolver) {
		t.Fatal("expected activation")
	}
	if lvl := m.BrightnessLevel(t0); lvl == nil || *lvl != 30 {
		t.Fatalf("expected scene brightness 30, got %v", lvl)
	}
	if spec, ok := m.Overlay(t0); !ok || spec.Text != "Breathe" {
		t.Fatalf("expected scene overlay Breathe, got %+v ok=%v", spec, ok)
	}
	// Just before TTL: still active.
	if !m.Evaluate(t0.Add(59*time.Second), stubSceneResolver) {
		// no transition expected
	}
	if _, ok := m.Active(); !ok {
		t.Fatal("scene should still be active before TTL")
	}
	// TTL expiry: deactivate and suppress until the next edge.
	if !m.Evaluate(t0.Add(60*time.Second), stubSceneResolver) {
		t.Fatal("expected TTL deactivation")
	}
	if _, ok := m.Active(); ok {
		t.Fatal("scene should have expired")
	}
	if m.BrightnessLevel(t0.Add(61*time.Second)) != nil {
		t.Fatal("brightness should be restored after TTL")
	}
	// Still holding: must not reactivate.
	m.Evaluate(t0.Add(61*time.Second), stubSceneResolver)
	if _, ok := m.Active(); ok {
		t.Fatal("scene should stay suppressed while triggers hold")
	}
	// Release then re-hold: rising edge reactivates.
	m.SetStates(map[string]string{"e": "off"})
	m.Evaluate(t0.Add(62*time.Second), stubSceneResolver)
	m.SetStates(map[string]string{"e": "on"})
	m.Evaluate(t0.Add(63*time.Second), stubSceneResolver)
	if _, ok := m.Active(); !ok {
		t.Fatal("scene should reactivate on the next rising edge")
	}
}

func TestSceneManagerManualSuppression(t *testing.T) {
	m := NewSceneManager()
	m.SetScenes([]Scene{{ID: 1, Name: "A", Enabled: true, Priority: 1, Triggers: holdAllOf("e", "on")}})
	m.SetStates(map[string]string{"e": "on"})
	now := time.Now()
	if !m.Evaluate(now, stubSceneResolver) {
		t.Fatal("expected activation")
	}
	m.SuppressActive()
	if m.ActiveSource() != nil {
		t.Fatal("manual suppression should clear the source")
	}
	// Still holding -> stays suppressed.
	m.Evaluate(now.Add(time.Second), stubSceneResolver)
	if m.ActiveSource() != nil {
		t.Fatal("should stay suppressed while triggers hold")
	}
	// Release then re-hold -> reactivates.
	m.SetStates(map[string]string{"e": "off"})
	m.Evaluate(now.Add(2*time.Second), stubSceneResolver)
	m.SetStates(map[string]string{"e": "on"})
	m.Evaluate(now.Add(3*time.Second), stubSceneResolver)
	if m.ActiveSource() == nil {
		t.Fatal("should reactivate after the next rising edge")
	}
}

func TestSceneManagerPreviewDoesNotPersist(t *testing.T) {
	m := NewSceneManager()
	sc := Scene{ID: 7, Name: "Preview", Enabled: true, Actions: SceneActions{SourceType: "clock", SourceID: 0}}
	now := time.Now()
	if !m.Preview(sc, stubSceneResolver, now, 15*time.Second) {
		t.Fatal("preview should resolve")
	}
	if src := m.ActiveSource(); src == nil || src.Name != "Preview" {
		t.Fatalf("expected preview source, got %+v", src)
	}
	if _, ok := m.Active(); ok {
		t.Fatal("preview must not persist as an active scene")
	}
	// Expiry clears the preview source on the next evaluation.
	m.Evaluate(now.Add(16*time.Second), stubSceneResolver)
	if m.ActiveSource() != nil {
		t.Fatal("preview source should clear after its window")
	}
}

func TestSelectTierSource(t *testing.T) {
	alarm := &sourceWithName{Name: "alarm"}
	scene := &sourceWithName{Name: "scene"}
	if got := selectTierSource(alarm, scene); got != alarm {
		t.Fatalf("alarm should outrank scene, got %+v", got)
	}
	if got := selectTierSource(nil, scene); got != scene {
		t.Fatalf("scene should win with no alarm, got %+v", got)
	}
	if got := selectTierSource(nil, nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestResolveEffectiveBrightnessWithScene(t *testing.T) {
	sched := []BrightnessWindow{{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "00:00", End: "23:59", Level: 50}}
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	sensor, override, alarm, scene := 40, 10, 20, 30
	if got := ResolveEffectiveBrightnessWithScene(now, sched, &sensor, &override, &alarm, &scene); got != 10 {
		t.Fatalf("override should win, got %d", got)
	}
	if got := ResolveEffectiveBrightnessWithScene(now, sched, &sensor, nil, &alarm, &scene); got != 20 {
		t.Fatalf("alarm should outrank scene, got %d", got)
	}
	if got := ResolveEffectiveBrightnessWithScene(now, sched, &sensor, nil, nil, &scene); got != 30 {
		t.Fatalf("scene should outrank sensor, got %d", got)
	}
	if got := ResolveEffectiveBrightnessWithScene(now, sched, &sensor, nil, nil, nil); got != 40 {
		t.Fatalf("sensor should be used with no scene, got %d", got)
	}
	if got := ResolveEffectiveBrightnessWithScene(now, sched, nil, nil, nil, nil); got != 50 {
		t.Fatalf("schedule should be used with no scene/sensor, got %d", got)
	}
}

func TestParseHAStates(t *testing.T) {
	raw := map[string]any{
		"states": []any{
			map[string]any{"entity_id": "binary_sensor.motion", "state": "on"},
			map[string]any{"entity_id": "sensor.temp", "state": "22.5"},
			map[string]any{"state": "ignored"},
		},
	}
	got := parseHAStates(raw)
	if got["binary_sensor.motion"] != "on" || got["sensor.temp"] != "22.5" {
		t.Fatalf("unexpected parsed states: %+v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 states, got %d", len(got))
	}
}
