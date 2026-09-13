package handlers

import (
	"context"
	"testing"
	"time"
)

// TestSceneEvaluatorWiring exercises the evaluator pass end to end without the
// background goroutine: an HA state change flips the active scene on the next
// tick and broadcasts the scene source to live feed controllers; trigger loss
// clears it.
func TestSceneEvaluatorWiring(t *testing.T) {
	client := sceneTestClient(t)
	ctx := context.Background()

	triggers := `[{"op":"all-of","conditions":[{"entity_id":"binary_sensor.motion","operator":"==","value":"on"}]}]`
	client.Scene.Create().
		SetName("Motion").
		SetEnabled(true).
		SetPriority(10).
		SetTriggers(triggers).
		SetActions(`{"source_type":"clock","source_id":0}`).
		SaveX(ctx)

	oldMgr := globalSceneManager
	oldResolver := SceneSourceResolver
	oldFetcher := AmbientStateFetcher
	oldAt := ambientAt
	oldCache := ambientCache
	t.Cleanup(func() {
		globalSceneManager = oldMgr
		SceneSourceResolver = oldResolver
		AmbientStateFetcher = oldFetcher
		ambientMu.Lock()
		ambientAt = oldAt
		ambientCache = oldCache
		ambientMu.Unlock()
	})

	globalSceneManager = NewSceneManager()
	globalSceneManager.SetScenes(loadSceneDefs(ctx, client))
	SceneSourceResolver = func(s *Scene) (*sourceWithName, bool) {
		return &sourceWithName{Name: "Clock", cacheKey: "clock:0"}, true
	}

	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })

	// Condition false: inert.
	SetAmbientStates(map[string]string{"binary_sensor.motion": "off"})
	if EvaluateScenesOnce(client, time.Now()) {
		t.Fatal("scene should not activate while condition is false")
	}
	if fc.GetSceneSource() != nil {
		t.Fatal("no scene source expected while condition is false")
	}

	// HA state change: next tick activates and broadcasts.
	SetAmbientStates(map[string]string{"binary_sensor.motion": "on"})
	if !EvaluateScenesOnce(client, time.Now()) {
		t.Fatal("expected activation transition")
	}
	src := fc.GetSceneSource()
	if src == nil || src.cacheKey != "clock:0" {
		t.Fatalf("expected clock scene source on the feed, got %+v", src)
	}

	// Trigger loss: clears the pin and restores rotation.
	SetAmbientStates(map[string]string{"binary_sensor.motion": "off"})
	if !EvaluateScenesOnce(client, time.Now().Add(time.Second)) {
		t.Fatal("expected deactivation transition")
	}
	if fc.GetSceneSource() != nil {
		t.Fatal("expected cleared scene source after trigger loss")
	}
}

// TestSceneEvaluatorUnknownEntityInert confirms a scene referencing an entity
// HA never reported does not activate.
func TestSceneEvaluatorUnknownEntityInert(t *testing.T) {
	client := sceneTestClient(t)
	ctx := context.Background()
	triggers := `[{"op":"all-of","conditions":[{"entity_id":"sensor.ghost","operator":">","value":"1"}]}]`
	client.Scene.Create().SetName("Ghost").SetEnabled(true).SetPriority(1).
		SetTriggers(triggers).SetActions(`{"source_type":"clock","source_id":0}`).SaveX(ctx)

	oldMgr := globalSceneManager
	oldResolver := SceneSourceResolver
	t.Cleanup(func() { globalSceneManager = oldMgr; SceneSourceResolver = oldResolver })
	globalSceneManager = NewSceneManager()
	globalSceneManager.SetScenes(loadSceneDefs(ctx, client))
	SceneSourceResolver = func(s *Scene) (*sourceWithName, bool) {
		return &sourceWithName{Name: "Clock", cacheKey: "clock:0"}, true
	}
	SetAmbientStates(map[string]string{"binary_sensor.motion": "on"})
	if EvaluateScenesOnce(client, time.Now()) {
		t.Fatal("unknown entity must not activate a scene")
	}
}
