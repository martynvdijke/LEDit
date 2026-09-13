package handlers

import (
	"context"
	"testing"

	"ledit/datasource"
	"ledit/render"
)

type inertSource struct{}

func (i *inertSource) GetPNG(_, _ int) (*render.RenderedImage, error) {
	return &render.RenderedImage{Format: "PNG"}, nil
}

var _ datasource.Datasource = &inertSource{}

func TestEvaluator_StatePathScoped(t *testing.T) {
	fakeState := map[string]any{
		"weather": map[string]any{"temp": float64(32), "wind": float64(5)},
		"cpu":     float64(1),
	}

	tests := []struct {
		name       string
		statePath  string
		condition  string
		wantPinned bool
	}{
		{"weather map temp gt", "weather", `{"path":"temp","operator":"gt","value":30}`, true},
		{"scalar wrap", "weather.temp", `{"path":"value","operator":"gt","value":30}`, true},
		{"missing path", "weather.missing", `{"path":"temp","operator":"gt","value":30}`, false},
		{"empty path backcompat cpu=1", "", `{"path":"cpu","operator":"gt","value":50}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newEventRuleTestDB(t)
			ctx := context.Background()
			fake := &testStateProvider{state: fakeState}
			orig := RuleTargetResolver
			RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
				if st == "weather" {
					return fake, true
				}
				return nil, false
			}
			t.Cleanup(func() { RuleTargetResolver = orig })
			ResetNonCapableLogged()

			fc := &FeedController{}
			joinController(fc)
			t.Cleanup(func() { leaveController(fc) })

			rule := client.DisplayRule.Create().
				SetName("sp-" + tc.name).
				SetEnabled(true).
				SetSourceType("weather").SetSourceID(0).
				SetCondition(tc.condition).SetStatePath(tc.statePath).
				SetCheckIntervalSeconds(5).SetCooldownSeconds(0).
				SaveX(ctx)

			states := map[int]*ruleState{rule.ID: {rule: rule}}
			EvaluateRulesOnce(client, states)
			_, _, pinned := fc.IsPinned()
			if pinned != tc.wantPinned {
				t.Fatalf("StatePath %q condition %s: pinned=%v want %v", tc.statePath, tc.condition, pinned, tc.wantPinned)
			}
			// cleanup pin for next sub-case isolation (also done via new DB/fc but ensure)
			unpinAll()
		})
	}
}

func TestEvaluator_InertNonStateProvider(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
		if st == "systemstats" {
			return &inertSource{}, true
		}
		return nil, false
	}
	t.Cleanup(func() { RuleTargetResolver = orig })
	ResetNonCapableLogged()

	rule := client.DisplayRule.Create().
		SetName("inert").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"gt","value":50}`).
		SetCheckIntervalSeconds(5).SetCooldownSeconds(0).
		SaveX(ctx)

	states := map[int]*ruleState{rule.ID: {rule: rule}}

	EvaluateRulesOnce(client, states)
	// feed not pinned (no controller needed but check via dummy)
	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })
	// Re-evaluate with controller joined - inert still not capable
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected NOT pinned for non-StateProvider")
	}

	nonCapableMu.Lock()
	v := nonCapableLogged["systemstats:0"]
	nonCapableMu.Unlock()
	if !v {
		t.Fatal("expected nonCapableLogged[systemstats:0] == true")
	}

	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected still NOT pinned on second call")
	}
	nonCapableMu.Lock()
	v2 := nonCapableLogged["systemstats:0"]
	nonCapableMu.Unlock()
	if !v2 {
		t.Fatal("expected nonCapableLogged still true after second call")
	}

	// rule stays enabled
	row, err := client.DisplayRule.Get(ctx, rule.ID)
	if err != nil {
		t.Fatalf("get rule: %v", err)
	}
	if !row.Enabled {
		t.Fatal("expected rule to remain Enabled")
	}
}
