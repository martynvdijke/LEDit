package handlers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ledit/datasource"
	"ledit/ent"
	"ledit/render"
)

// countingStateProvider records how many times the evaluator fetched state so
// the "one CurrentState fetch per rule per check" contract can be asserted.
type countingStateProvider struct {
	mu    sync.Mutex
	calls int
	state map[string]any
	err   error
}

func (c *countingStateProvider) GetPNG(width, height int) (*render.RenderedImage, error) {
	return nil, nil
}

func (c *countingStateProvider) CurrentState(ctx context.Context) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.state, c.err
}

func (c *countingStateProvider) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *countingStateProvider) set(state map[string]any, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
	c.err = err
}

var _ datasource.Datasource = &countingStateProvider{}
var _ datasource.StateProvider = &countingStateProvider{}

func TestSyncRuleStatesReload(t *testing.T) {
	now := time.Now()
	rule := &ent.DisplayRule{
		ID:                   7,
		Name:                 "reload-rule",
		SourceType:           "genericapi",
		SourceID:             1,
		Condition:            `{"path":"a","operator":"eq","value":1}`,
		ForSeconds:           60,
		TriggerKind:          triggerKindCondition,
		CheckIntervalSeconds: 30,
		CooldownSeconds:      120,
	}

	states := map[int]*ruleState{}
	syncRuleStates(states, []*ent.DisplayRule{rule}, now)
	rs := states[7]
	if rs == nil {
		t.Fatal("expected rule state to be created")
	}
	if rs.triggerSig != triggerSignature(rule) {
		t.Fatalf("trigger signature not stored: %q", rs.triggerSig)
	}
	// nextCheck is jittered by +/-10% of the check interval.
	if rs.nextCheck.Before(now.Add(27*time.Second)) || rs.nextCheck.After(now.Add(33*time.Second)) {
		t.Fatalf("nextCheck %v outside the jitter window", rs.nextCheck.Sub(now))
	}

	// Simulate an in-flight trigger: timers running and the rule pinned.
	rs.conditionSince = now.Add(-30 * time.Second)
	rs.seqArmed = true
	rs.held = true
	rs.seqStep = 2
	rs.stepSince = now.Add(-5 * time.Second)
	rs.armedAt = now.Add(-time.Minute)
	rs.pinned = true
	rs.cooldownUntil = now.Add(5 * time.Minute)

	// A reload with identical trigger values must not disturb the timers.
	same := *rule
	syncRuleStates(states, []*ent.DisplayRule{&same}, now.Add(time.Second))
	if states[7] != rs {
		t.Fatal("reload must reuse the existing rule state")
	}
	if rs.conditionSince.IsZero() || !rs.seqArmed || !rs.held || rs.seqStep != 2 {
		t.Fatalf("timers reset without a trigger change: %+v", rs)
	}
	if rs.stepSince.IsZero() || rs.armedAt.IsZero() {
		t.Fatal("sequence timers reset without a trigger change")
	}
	if !rs.pinned || rs.cooldownUntil.IsZero() {
		t.Fatal("pin bookkeeping must survive an unchanged reload")
	}

	// Changing the trigger definition restarts its timers but keeps the pin
	// and cooldown contract intact.
	changed := *rule
	changed.ForSeconds = 120
	syncRuleStates(states, []*ent.DisplayRule{&changed}, now.Add(2*time.Second))
	if rs.triggerSig != triggerSignature(&changed) {
		t.Fatal("trigger signature must follow the changed rule")
	}
	if !rs.conditionSince.IsZero() || rs.seqArmed || rs.held || rs.seqStep != 0 {
		t.Fatalf("timers must reset when the trigger changes: %+v", rs)
	}
	if !rs.stepSince.IsZero() || !rs.armedAt.IsZero() {
		t.Fatal("sequence timers must reset when the trigger changes")
	}
	if !rs.pinned || rs.cooldownUntil.IsZero() {
		t.Fatal("pin and cooldown must survive a trigger change")
	}

	// Disabled/deleted rules are pruned from the state map.
	syncRuleStates(states, nil, now)
	if len(states) != 0 {
		t.Fatalf("expected all states pruned, got %d", len(states))
	}
}

func TestAdvanceConditionTriggerHold(t *testing.T) {
	composed := &ent.DisplayRule{
		ID:                   1,
		Name:                 "door-warm",
		Condition:            `{"op":"and","conditions":[{"path":"door","operator":"eq","value":"open"},{"path":"temp","operator":"lt","value":20}]}`,
		ForSeconds:           60,
		CheckIntervalSeconds: 5,
	}
	rs := &ruleState{rule: composed}
	t0 := time.Now()
	open := map[string]any{"door": "open", "temp": float64(10)}

	if fire, release := advanceTrigger(rs, t0, open); fire || release {
		t.Fatalf("fired before the hold elapsed: fire=%v release=%v", fire, release)
	}
	if rs.conditionSince.IsZero() {
		t.Fatal("expected the hold clock to start")
	}
	if fire, _ := advanceTrigger(rs, t0.Add(30*time.Second), open); fire {
		t.Fatal("fired before the full hold")
	}
	if !rs.conditionSince.Equal(t0) {
		t.Fatalf("hold clock moved without a false tick: %v", rs.conditionSince.Sub(t0))
	}
	if fire, _ := advanceTrigger(rs, t0.Add(60*time.Second), open); !fire {
		t.Fatal("expected fire after the condition held for 60s")
	}

	// A false tick resets the hold and releases the pin.
	rs.pinned = true
	broken := map[string]any{"door": "open", "temp": float64(30)}
	fire, release := advanceTrigger(rs, t0.Add(70*time.Second), broken)
	if fire || !release {
		t.Fatalf("expected release on a false tick: fire=%v release=%v", fire, release)
	}
	if !rs.conditionSince.IsZero() {
		t.Fatal("hold clock must reset on a false tick")
	}

	// A failed state fetch arrives as an empty map: same reset + release.
	rs.pinned = true
	rs.conditionSince = t0.Add(80 * time.Second)
	fire, release = advanceTrigger(rs, t0.Add(90*time.Second), map[string]any{})
	if fire || !release {
		t.Fatalf("failed fetch must reset and release: fire=%v release=%v", fire, release)
	}
	if !rs.conditionSince.IsZero() {
		t.Fatal("hold clock must reset when the state fetch fails")
	}

	// for_seconds = 0 keeps the legacy behaviour: fire on the first true tick.
	legacy := &ent.DisplayRule{ID: 2, Name: "legacy", Condition: `{"path":"door","operator":"eq","value":"open"}`}
	lrs := &ruleState{rule: legacy}
	if fire, _ := advanceTrigger(lrs, t0, open); !fire {
		t.Fatal("for_seconds=0 must fire on the first matching tick")
	}
	// An already pinned rule refreshes its cooldown instead of firing again.
	lrs.pinned = true
	if fire, release := advanceTrigger(lrs, t0.Add(time.Second), open); fire || release {
		t.Fatalf("already pinned rule must not fire again: fire=%v release=%v", fire, release)
	}

	// Unusable trigger definitions are ignored instead of panicking.
	bad := &ruleState{rule: &ent.DisplayRule{ID: 3, Condition: `{}`}}
	if fire, release := advanceTrigger(bad, t0, open); fire || release {
		t.Fatalf("invalid condition must not fire: fire=%v release=%v", fire, release)
	}
	if fire, release := advanceTrigger(nil, t0, open); fire || release {
		t.Fatal("nil state must not fire")
	}
}

func TestAdvanceConditionTriggerCooldownRefresh(t *testing.T) {
	rule := &ent.DisplayRule{
		ID:              4,
		Name:            "still-true",
		Condition:       `{"path":"v","operator":"gt","value":5}`,
		CooldownSeconds: 120,
	}
	rs := &ruleState{rule: rule, pinned: true}
	now := time.Now()
	advanceTrigger(rs, now, map[string]any{"v": float64(10)})
	if rs.cooldownUntil.Before(now.Add(119 * time.Second)) {
		t.Fatalf("pinned rule must refresh its cooldown while still true: %v", rs.cooldownUntil.Sub(now))
	}
}

func newSequenceRule() *ent.DisplayRule {
	return &ent.DisplayRule{
		ID:                   10,
		Name:                 "door-then-motion",
		TriggerKind:          triggerKindSequence,
		CheckIntervalSeconds: 5,
		Sequence:             `{"window_seconds":3600,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":10},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":20}]}`,
	}
}

func TestAdvanceSequenceTriggerRun(t *testing.T) {
	rs := &ruleState{rule: newSequenceRule()}
	t0 := time.Now()
	aOnly := map[string]any{"door": "open", "motion": false}
	both := map[string]any{"door": "open", "motion": true}

	if fire, _ := advanceTrigger(rs, t0, aOnly); fire {
		t.Fatal("first step must not fire")
	}
	if rs.stepSince.IsZero() {
		t.Fatal("expected the first step hold clock to start")
	}
	if fire, _ := advanceTrigger(rs, t0.Add(9*time.Second), aOnly); fire {
		t.Fatal("fired before the first step held for its duration")
	}
	if fire, _ := advanceTrigger(rs, t0.Add(10*time.Second), aOnly); fire {
		t.Fatal("arming the run must not fire")
	}
	if !rs.seqArmed || rs.seqStep != 1 || !rs.armedAt.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("expected run armed at step 1: armed=%v step=%d armedAt=%v", rs.seqArmed, rs.seqStep, rs.armedAt.Sub(t0))
	}

	if fire, _ := advanceTrigger(rs, t0.Add(11*time.Second), both); fire {
		t.Fatal("final step must hold for its own duration")
	}
	if fire, _ := advanceTrigger(rs, t0.Add(30*time.Second), both); fire {
		t.Fatal("fired before the final step hold elapsed")
	}
	if fire, _ := advanceTrigger(rs, t0.Add(31*time.Second), both); !fire {
		t.Fatal("expected fire once the final step completed inside the window")
	}
	if !rs.held || rs.seqArmed || rs.seqStep != 0 {
		t.Fatalf("expected the run to enter its held state: held=%v armed=%v step=%d", rs.held, rs.seqArmed, rs.seqStep)
	}

	// The run fires exactly once; the pin is held while the final step matches.
	rs.pinned = true
	if fire, release := advanceTrigger(rs, t0.Add(32*time.Second), both); fire || release {
		t.Fatalf("held run must not fire or release: fire=%v release=%v", fire, release)
	}
	if fire, release := advanceTrigger(rs, t0.Add(40*time.Second), aOnly); fire || !release {
		t.Fatalf("expected release when the final step stops matching: fire=%v release=%v", fire, release)
	}
	if rs.held {
		t.Fatal("held state must clear on release")
	}
}

func TestAdvanceSequenceTriggerStepBlip(t *testing.T) {
	rs := &ruleState{rule: newSequenceRule()}
	t0 := time.Now()
	aOnly := map[string]any{"door": "open", "motion": false}
	both := map[string]any{"door": "open", "motion": true}

	advanceTrigger(rs, t0, aOnly)
	advanceTrigger(rs, t0.Add(10*time.Second), aOnly) // armed
	if !rs.seqArmed {
		t.Fatal("expected run armed")
	}
	advanceTrigger(rs, t0.Add(11*time.Second), both) // step hold starts
	if rs.stepSince.IsZero() {
		t.Fatal("expected final step hold clock")
	}
	advanceTrigger(rs, t0.Add(15*time.Second), aOnly) // blip resets only this step
	if !rs.stepSince.IsZero() {
		t.Fatal("a blip must clear the current step hold")
	}
	if !rs.seqArmed {
		t.Fatal("a blip must not disarm the run")
	}
	advanceTrigger(rs, t0.Add(16*time.Second), both) // hold restarts
	if fire, _ := advanceTrigger(rs, t0.Add(31*time.Second), both); fire {
		t.Fatal("fired before the restarted hold elapsed")
	}
	if fire, _ := advanceTrigger(rs, t0.Add(36*time.Second), both); !fire {
		t.Fatal("expected fire after the restarted hold elapsed")
	}
}

func TestAdvanceSequenceTriggerWindowExpiry(t *testing.T) {
	rs := &ruleState{rule: newSequenceRule()}
	t0 := time.Now()
	aOnly := map[string]any{"door": "open", "motion": false}
	both := map[string]any{"door": "open", "motion": true}

	advanceTrigger(rs, t0, aOnly)
	advanceTrigger(rs, t0.Add(10*time.Second), aOnly)
	if !rs.seqArmed {
		t.Fatal("expected run armed")
	}
	fire, release := advanceTrigger(rs, t0.Add(10*time.Second+3601*time.Second), both)
	if fire || release {
		t.Fatalf("an expired window must reset silently: fire=%v release=%v", fire, release)
	}
	if rs.seqArmed || rs.seqStep != 0 || !rs.armedAt.IsZero() || !rs.stepSince.IsZero() {
		t.Fatal("expired window must reset the run")
	}
}

func TestAdvanceSequenceTriggerSecondStepAlone(t *testing.T) {
	rs := &ruleState{rule: newSequenceRule()}
	t0 := time.Now()
	bOnly := map[string]any{"door": "closed", "motion": true}
	for i := 0; i < 5; i++ {
		if fire, release := advanceTrigger(rs, t0.Add(time.Duration(i)*time.Minute), bOnly); fire || release {
			t.Fatalf("final step alone must never fire: fire=%v release=%v", fire, release)
		}
	}
	if rs.seqArmed {
		t.Fatal("final step alone must not arm the run")
	}
}

func TestApplyTriggerOutcomeThenActionOncePerRisingEdge(t *testing.T) {
	client := newEventRuleTestDB(t)
	provider := &countingStateProvider{state: map[string]any{"weather": map[string]any{"temp": float64(30), "wind": float64(5)}}}
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, id int) (datasource.Datasource, bool) { return provider, true }
	t.Cleanup(func() { RuleTargetResolver = orig })
	ResetNonCapableLogged()

	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })

	rule := client.DisplayRule.Create().
		SetName("hot-or-windy").SetEnabled(true).
		SetSourceType("genericapi").SetSourceID(1).
		SetCondition(`{"op":"or","conditions":[{"path":"temp","operator":"gt","value":25},{"path":"wind","operator":"gt","value":50}]}`).
		SetStatePath("weather").
		SetCheckIntervalSeconds(5).SetCooldownSeconds(0).
		SetThenActions(`{"kind":"notification","message":"rule fired"}`).
		SaveX(context.Background())
	states := map[int]*ruleState{rule.ID: {rule: rule}}

	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("composed condition should pin the target")
	}
	if provider.count() != 1 {
		t.Fatalf("expected exactly one state fetch per check, got %d", provider.count())
	}
	notifs, err := client.Notification.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if notifs != 1 {
		t.Fatalf("then-action must run once when the trigger fires, got %d notifications", notifs)
	}

	// The trigger stays true: no second then-action for the same rising edge.
	EvaluateRulesOnce(client, states)
	if provider.count() != 2 {
		t.Fatalf("expected one fetch per check, got %d", provider.count())
	}
	notifs, _ = client.Notification.Query().Count(context.Background())
	if notifs != 1 {
		t.Fatalf("then-action must not repeat while pinned, got %d notifications", notifs)
	}

	// Breaking the OR releases the pin.
	provider.set(map[string]any{"weather": map[string]any{"temp": float64(10), "wind": float64(5)}}, nil)
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("expected release when the composed condition turns false")
	}

	// A failed fetch behaves like an empty state and releases as well.
	states[rule.ID].pinned = true
	fc.Pin("genericapi:1", rule.Name)
	provider.set(nil, errors.New("upstream down"))
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("failed state fetch must release the pin")
	}

	// A fresh rising edge after the release fires again exactly once.
	provider.set(map[string]any{"weather": map[string]any{"temp": float64(30), "wind": float64(5)}}, nil)
	EvaluateRulesOnce(client, states)
	notifs, _ = client.Notification.Query().Count(context.Background())
	if notifs != 2 {
		t.Fatalf("expected a second then-action on a fresh rising edge, got %d notifications", notifs)
	}
}

func TestComposedPinManualSkipContract(t *testing.T) {
	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })

	rule := &ent.DisplayRule{
		ID:                   20,
		Name:                 "door-opened",
		Condition:            `{"op":"and","conditions":[{"path":"door","operator":"eq","value":"open"},{"path":"armed","operator":"eq","value":true}]}`,
		CheckIntervalSeconds: 5,
	}
	rs := &ruleState{rule: rule}
	t0 := time.Now()
	match := map[string]any{"door": "open", "armed": true}
	noMatch := map[string]any{"door": "closed", "armed": true}

	fire, release := advanceTrigger(rs, t0, match)
	applyTriggerOutcome(context.Background(), nil, rs, "systemstats:0", t0, fire, release)
	if key, by, ok := fc.IsPinned(); !ok || key != "systemstats:0" || by != "door-opened" {
		t.Fatalf("expected composed rule to pin its target, got key=%q by=%q ok=%v", key, by, ok)
	}

	// Manual skip clears the feed pin while the rule remains logically pinned.
	fc.Next()
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("manual skip must clear the feed pin")
	}
	if !rs.pinned {
		t.Fatal("manual skip must not reset the rule's pin bookkeeping")
	}
	fire, release = advanceTrigger(rs, t0.Add(5*time.Second), match)
	applyTriggerOutcome(context.Background(), nil, rs, "systemstats:0", t0.Add(5*time.Second), fire, release)
	if _, _, ok := fc.IsPinned(); ok {
		t.Fatal("a still-true trigger must not re-pin before a fresh rising edge")
	}

	// Dropping the condition resets the rule; the next rising edge pins again.
	fire, release = advanceTrigger(rs, t0.Add(10*time.Second), noMatch)
	applyTriggerOutcome(context.Background(), nil, rs, "systemstats:0", t0.Add(10*time.Second), fire, release)
	if rs.pinned {
		t.Fatal("expected the rule pin to clear when the condition drops")
	}
	fire, release = advanceTrigger(rs, t0.Add(15*time.Second), match)
	applyTriggerOutcome(context.Background(), nil, rs, "systemstats:0", t0.Add(15*time.Second), fire, release)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("expected a fresh rising edge to re-pin the target")
	}
}
