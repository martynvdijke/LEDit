package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ledit/datasource"
	"ledit/ent/sports"
)

const sportsLiveESPN = `{"events":[{"id":"1","date":"2025-01-01T18:00:00Z","competitions":[{"competitors":[{"homeAway":"home","team":{"displayName":"Eagles","abbreviation":"PHI"},"score":"21"},{"homeAway":"away","team":{"displayName":"Cowboys","abbreviation":"DAL"},"score":"14"}],"status":{"period":3,"displayClock":"8:23","type":{"state":"in","shortDetail":"Q3 08:23"}}}]}]}`

// TestResolveTarget_SportsLivePin verifies the documented event-rule
// integration: a sports source resolves to a StateProvider and a DisplayRule on
// {"path":"live"} pins the wall while a game is live.
func TestResolveTarget_SportsLivePin(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sportsLiveESPN))
	}))
	defer srv.Close()

	sp := client.Sports.Create().
		SetProvider(sports.ProviderEspn).
		SetURL(srv.URL + "/%s/scoreboard").
		SetConfig(`{"leagues":["nfl"]}`).
		SaveX(ctx)
	gs := client.GeneralSettings.GetX(ctx, 1)
	client.GeneralSettings.UpdateOneID(gs.ID).AddSports(sp).ExecX(ctx)

	ds, ok := resolveTarget("sports", sp.ID, client)
	if !ok {
		t.Fatal("sports target not resolved")
	}
	provider, ok := ds.(datasource.StateProvider)
	if !ok {
		t.Fatal("SportsDS must implement StateProvider")
	}
	state, err := provider.CurrentState(ctx)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if state["live"] != true {
		t.Fatalf("expected live true, got %+v", state)
	}

	ResetNonCapableLogged()
	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })
	rule := client.DisplayRule.Create().
		SetName("live").
		SetEnabled(true).
		SetSourceType("sports").
		SetSourceID(sp.ID).
		SetCondition(`{"path":"live","operator":"eq","value":true}`).
		SetCheckIntervalSeconds(5).
		SetCooldownSeconds(0).
		SaveX(ctx)
	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("expected wall pinned during a live game")
	}
}
