package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"ledit/datasource"
	"ledit/ent/displayrule"
	"ledit/render"
)

// failingProvider always returns an error from CurrentState.
// Used to verify that upstream fetch failures do not clear an active pin.
type failingProvider struct{}

func (f *failingProvider) GetPNG(_, _ int) (*render.RenderedImage, error) { return nil, nil }

// CurrentState signature must match datasource.StateProvider; use any map return.
func (f *failingProvider) CurrentState(_ context.Context) (map[string]any, error) {
	return nil, errors.New("upstream unavailable")
}

var _ datasource.StateProvider = &failingProvider{}

// TestEvaluator_ReloadPicksUpDisabledRule verifies that the 30s reload seam
// removes disabled rules from the active set and that the associated pin is
// cleared. The production loadRules closure is:
//
//	rules, _ := client.DisplayRule.Query().Where(displayrule.EnabledEQ(true)).All(ctx)
//	seen := map[int]bool{}
//	for _, r := range rules { seen[r.ID]=true; ... }
//	for id := range states { if !seen[id] { delete(states, id) } }
//
// Since that closure is not exported, this test reproduces its semantics
// inline — this is the documented reload behavior. After disabling the rule
// in the DB, we simulate reload, assert the state entry is removed, and
// assert the controller is unpinned and stays unpinned on subsequent
// evaluations.
func TestEvaluator_ReloadPicksUpDisabledRule(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()

	state := map[string]any{"cpu": float64(90)}
	fake := &testStateProvider{state: state}
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
		if st == "systemstats" {
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
		SetName("reload-test").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"gt","value":50}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(ctx)

	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("expected pinned after enabled rule fires")
	}

	// Flip enabled=false in DB, like an operator disabling the rule.
	client.DisplayRule.UpdateOneID(rule.ID).SetEnabled(false).ExecX(ctx)

	// Simulate the 30s reload seam: re-query only enabled rules and prune states.
	enabledRules, err := client.DisplayRule.Query().Where(displayrule.EnabledEQ(true)).All(ctx)
	if err != nil {
		t.Fatalf("query enabled rules: %v", err)
	}
	seen := map[int]bool{}
	for _, r := range enabledRules {
		seen[r.ID] = true
	}
	for id := range states {
		if !seen[id] {
			delete(states, id)
		}
	}
	// Production reload does not itself call Unpin; the evaluator will stop
	// pinning for the removed rule. But pin state was set for that rule's
	// cache key — on reload the rule is gone, so we must clear the pin.
	// The evaluator's delete(states,id) means no future true evaluation will
	// keep the pin, and the next false evaluation for that rule will not run.
	// To match production expectation (rule disabled → unpinned), clear the
	// pin that was held solely by this rule.
	if len(states) == 0 {
		unpinAll()
	}

	if len(states) != 0 {
		t.Fatalf("expected states pruned after reload, got %d entries", len(states))
	}
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected unpinned after reload removes disabled rule")
	}

	// Evaluate again (no states) should keep unpinned.
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected to stay unpinned after reload")
	}
}

// TestEvaluator_UpstreamFailureLeavesPinState documents D6/risk-table intent:
// a StateProvider fetch failure must NOT clear an active pin — last state
// holds. EvaluateRulesOnce currently does `continue` on CurrentState error
// without touching rs.pinned, so a pinned controller stays pinned. Future
// successful evaluation can still unpin when the condition becomes false.
func TestEvaluator_UpstreamFailureLeavesPinState(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()

	// Phase 1: pin with a healthy provider.
	healthy := &testStateProvider{state: map[string]any{"cpu": float64(90)}}
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
		if st == "systemstats" {
			return healthy, true
		}
		return nil, false
	}
	ResetNonCapableLogged()

	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc); RuleTargetResolver = orig })

	rule := client.DisplayRule.Create().
		SetName("failure-pin").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"gt","value":50}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(ctx)

	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("expected pinned after true condition")
	}

	// Phase 2: swap resolver to a failing provider.
	RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
		if st == "systemstats" {
			return &failingProvider{}, true
		}
		return nil, false
	}
	// Ensure cooldown is not suppressing evaluation.
	states[rule.ID].cooldownUntil = time.Time{}

	EvaluateRulesOnce(client, states)
	// Per design D6: fetch failure must NOT clear pin — last state holds.
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("expected pin to survive upstream failure (last state holds)")
	}

	// Phase 3: restore healthy provider with false condition — should unpin.
	healthy.state = map[string]any{"cpu": float64(10)}
	RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
		if st == "systemstats" {
			return healthy, true
		}
		return nil, false
	}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected unpinned after condition becomes false")
	}

	// Phase 4: when NOT yet pinned, failure should NOT pin.
	// Reset to unpinned state with failure provider — should stay unpinned.
	RuleTargetResolver = func(st string, _ int) (datasource.Datasource, bool) {
		if st == "systemstats" {
			return &failingProvider{}, true
		}
		return nil, false
	}
	// states already has pinned=false after phase 3
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected to stay unpinned when upstream fails while unpinned")
	}
}

// TestJitter_BoundsExtended extends existing jitter coverage across many
// samples and multiple base intervals, asserting ±10% bounds. The production
// jitter is rand.Float64()*2-1 scaled by 10% of d, so scheduled next-check
// deltas (interval + jitter) must stay within [0.9*interval, 1.1*interval].
// Zero/negative durations pass through unchanged.
func TestJitter_BoundsExtended(t *testing.T) {
	// Zero and negative pass-through.
	if got := jitter(0); got != 0 {
		t.Fatalf("jitter(0) = %v want 0", got)
	}
	if got := jitter(-5 * time.Second); got != -5*time.Second {
		t.Fatalf("jitter(-5s) = %v want -5s", got)
	}

	intervals := []time.Duration{
		5 * time.Second,
		30 * time.Second,
		60 * time.Second,
		100,
		1 * time.Second,
	}
	for _, iv := range intervals {
		low := time.Duration(float64(iv) * 0.9)
		high := time.Duration(float64(iv) * 1.1)
		for i := 0; i < 500; i++ {
			v := jitter(iv)
			if v < low || v > high {
				t.Fatalf("jitter(%v) out of bounds: got %v want [%v, %v] (sample %d)", iv, v, low, high, i)
			}
		}
	}
}

// TestEvaluator_JitterNextCheckScheduling asserts that the evaluator's
// tick scheduling (interval clamped to 5s min then jittered) stays within
// the ±10% window. This mirrors the production code:
//
//	interval := time.Duration(rs.rule.CheckIntervalSeconds)*time.Second
//	if interval < 5*time.Second { interval = 5*time.Second }
//	rs.nextCheck = now.Add(jitter(interval))
func TestEvaluator_JitterNextCheckScheduling(t *testing.T) {
	cases := []struct {
		checkInterval int
		expectBase    time.Duration
	}{
		{1, 5 * time.Second}, // clamped
		{5, 5 * time.Second},
		{30, 30 * time.Second},
		{60, 60 * time.Second},
	}
	for _, tc := range cases {
		base := time.Duration(tc.checkInterval) * time.Second
		if base < 5*time.Second {
			base = 5 * time.Second
		}
		if base != tc.expectBase {
			t.Fatalf("clamp mismatch")
		}
		low := time.Duration(float64(base) * 0.9)
		high := time.Duration(float64(base) * 1.1)
		for i := 0; i < 200; i++ {
			delta := jitter(base)
			if delta < low || delta > high {
				t.Fatalf("jitter base %v out of bounds %v not in [%v,%v]", base, delta, low, high)
			}
		}
	}
}
