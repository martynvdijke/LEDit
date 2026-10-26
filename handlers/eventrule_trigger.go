package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"ledit/datasource"
	"ledit/ent"
)

// Trigger kinds stored on DisplayRule.TriggerKind.
const (
	triggerKindCondition = "condition"
	triggerKindSequence  = "sequence"
)

var (
	// triggerLogged dedups invalid-trigger logs to once per rule until the
	// rule is fixed (its trigger signature changes) or the logs are reset.
	triggerLogged = map[string]bool{}
	triggerLogMu  sync.Mutex
)

// normalizeTriggerKind maps stored values onto the two supported kinds. Any
// value other than an exact "sequence" (including empty legacy rows) reads as
// a plain condition rule.
func normalizeTriggerKind(kind string) string {
	if strings.TrimSpace(strings.ToLower(kind)) == triggerKindSequence {
		return triggerKindSequence
	}
	return triggerKindCondition
}

// validTriggerKind reports whether a submitted trigger kind is one of the
// supported values.
func validTriggerKind(kind string) bool {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "", triggerKindCondition, triggerKindSequence:
		return true
	default:
		return false
	}
}

// triggerSignature identifies a rule's trigger definition so the evaluator can
// tell whether a reloaded rule actually changed and its timers must restart.
func triggerSignature(rule *ent.DisplayRule) string {
	if rule == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s",
		rule.Condition, rule.ForSeconds, rule.TriggerKind, rule.Sequence)))
	return hex.EncodeToString(sum[:16])
}

// parseRuleTrigger validates and parses a rule's stored trigger definition.
func parseRuleTrigger(rule *ent.DisplayRule) (kind string, node *datasource.ConditionNode, seq *datasource.SequenceTrigger, err error) {
	kind = normalizeTriggerKind(rule.TriggerKind)
	if kind == triggerKindSequence {
		seq, err = datasource.ParseSequence(rule.Sequence, rule.CheckIntervalSeconds)
		return kind, nil, seq, err
	}
	node, err = datasource.ParseConditionNode(rule.Condition)
	return kind, node, nil, err
}

// logTriggerParseOnce reports an unusable trigger definition once per rule
// version so a hand-edited row cannot flood the log every check interval.
func logTriggerParseOnce(sig, rule string, err error) {
	triggerLogMu.Lock()
	first := !triggerLogged[sig]
	triggerLogged[sig] = true
	triggerLogMu.Unlock()
	if first {
		slog.Warn("event rule trigger parse failed, skipping", "rule", rule, "error", err)
	}
}

// resetTriggerLogged clears the invalid-trigger log dedupe cache.
func resetTriggerLogged() {
	triggerLogMu.Lock()
	triggerLogged = map[string]bool{}
	triggerLogMu.Unlock()
}

// clearTriggerLogged forgets that a (now valid) trigger definition was logged
// as broken, so a future break logs once more.
func clearTriggerLogged(sig string) {
	triggerLogMu.Lock()
	delete(triggerLogged, sig)
	triggerLogMu.Unlock()
}

// syncRuleStates upserts freshly loaded rules into the evaluator state map.
// Trigger timers restart only when a rule's trigger definition changed; the
// pin state and cooldown window always survive a reload.
func syncRuleStates(states map[int]*ruleState, rules []*ent.DisplayRule, now time.Time) {
	seen := map[int]bool{}
	for _, r := range rules {
		seen[r.ID] = true
		sig := triggerSignature(r)
		existing, ok := states[r.ID]
		if !ok {
			states[r.ID] = &ruleState{
				rule:       r,
				nextCheck:  now.Add(jitter(time.Duration(r.CheckIntervalSeconds) * time.Second)),
				triggerSig: sig,
			}
			continue
		}
		if existing.triggerSig != sig {
			resetTriggerState(existing)
			existing.triggerSig = sig
		}
		existing.rule = r
	}
	for id := range states {
		if !seen[id] {
			delete(states, id)
		}
	}
}

// resetTriggerState clears the per-run timers of a trigger while leaving the
// pin and cooldown bookkeeping untouched.
func resetTriggerState(rs *ruleState) {
	rs.conditionSince = time.Time{}
	rs.seqArmed = false
	rs.held = false
	rs.seqStep = 0
	rs.stepSince = time.Time{}
	rs.armedAt = time.Time{}
}

// advanceTrigger advances one rule's trigger timers for a single evaluation
// tick and reports whether the trigger completed (fire) or stopped holding
// (release). It is a pure state-machine step: the caller owns pinning,
// then-actions and the cooldown gate.
func advanceTrigger(rs *ruleState, now time.Time, state map[string]any) (fire, release bool) {
	if rs == nil || rs.rule == nil {
		return false, false
	}
	kind, node, seq, err := parseRuleTrigger(rs.rule)
	if err != nil {
		// Unusable definition: leave the pin alone and wait for a fix.
		return false, false
	}
	if kind == triggerKindSequence {
		return advanceSequenceTrigger(rs, now, state, seq)
	}
	return advanceConditionTrigger(rs, now, state, node)
}

// advanceConditionTrigger implements sustained conditions: the rule fires once
// the composed condition has held continuously for rule.ForSeconds.
func advanceConditionTrigger(rs *ruleState, now time.Time, state map[string]any, node *datasource.ConditionNode) (fire, release bool) {
	cd := time.Duration(rs.rule.CooldownSeconds) * time.Second
	if !datasource.EvaluateNode(state, node) {
		rs.conditionSince = time.Time{}
		return false, rs.pinned
	}
	if rs.pinned {
		// Keep the cooldown window sliding while the condition stays true so
		// the release still respects it (legacy "already pinned" behaviour).
		if cd > 0 {
			rs.cooldownUntil = now.Add(cd)
		}
		return false, false
	}
	if rs.conditionSince.IsZero() {
		rs.conditionSince = now
	}
	if now.Sub(rs.conditionSince) < time.Duration(rs.rule.ForSeconds)*time.Second {
		return false, false
	}
	return true, false
}

// advanceSequenceTrigger implements the ordered-step trigger: the run arms when
// the first step holds for its own duration and fires once the final step
// completes inside the window. The pin is then held while the final step keeps
// matching, and released (subject to the cooldown) when it stops.
func advanceSequenceTrigger(rs *ruleState, now time.Time, state map[string]any, seq *datasource.SequenceTrigger) (fire, release bool) {
	if seq == nil || len(seq.Steps) == 0 {
		return false, false
	}
	cd := time.Duration(rs.rule.CooldownSeconds) * time.Second
	last := len(seq.Steps) - 1

	resetRun := func() {
		rs.seqArmed = false
		rs.seqStep = 0
		rs.stepSince = time.Time{}
		rs.armedAt = time.Time{}
	}

	if rs.held {
		if datasource.EvaluateNode(state, seq.Steps[last].Condition) {
			if rs.pinned && cd > 0 {
				rs.cooldownUntil = now.Add(cd)
			}
			return false, false
		}
		rs.held = false
		resetRun()
		return false, rs.pinned
	}

	if rs.seqArmed {
		if now.Sub(rs.armedAt) > time.Duration(seq.WindowSeconds)*time.Second {
			// The run ran out of time: reset without firing.
			resetRun()
			return false, false
		}
		if rs.seqStep < 1 || rs.seqStep > last {
			resetRun()
			return false, false
		}
		step := seq.Steps[rs.seqStep]
		if !datasource.EvaluateNode(state, step.Condition) {
			// Only this step's hold restarts; the run keeps its window.
			rs.stepSince = time.Time{}
			return false, false
		}
		if rs.stepSince.IsZero() {
			rs.stepSince = now
		}
		if now.Sub(rs.stepSince) < time.Duration(step.ForSeconds)*time.Second {
			return false, false
		}
		if rs.seqStep == last {
			rs.held = true
			resetRun()
			return true, false
		}
		rs.seqStep++
		rs.stepSince = time.Time{}
		return false, false
	}

	// Idle: the first step holding arms the run and starts the window.
	first := seq.Steps[0]
	if !datasource.EvaluateNode(state, first.Condition) {
		rs.stepSince = time.Time{}
		return false, false
	}
	if rs.stepSince.IsZero() {
		rs.stepSince = now
	}
	if now.Sub(rs.stepSince) < time.Duration(first.ForSeconds)*time.Second {
		return false, false
	}
	rs.seqArmed = true
	rs.armedAt = now
	rs.seqStep = 1
	rs.stepSince = time.Time{}
	return false, false
}

// applyTriggerOutcome performs the shared pin/unpin bookkeeping for one rule
// tick so both evaluator loops keep the exact legacy contract: a completed
// trigger pins the target and runs its then-actions once; a dropped trigger
// unpins once the cooldown (minimum hold) has elapsed.
func applyTriggerOutcome(ctx context.Context, srv *Server, rs *ruleState, cacheKey string, now time.Time, fire, release bool) {
	if rs == nil || rs.rule == nil {
		return
	}
	cd := time.Duration(rs.rule.CooldownSeconds) * time.Second
	if fire && !rs.pinned {
		pinAll(cacheKey, rs.rule.Name)
		rs.pinned = true
		if cd > 0 {
			rs.cooldownUntil = now.Add(cd)
		}
		actCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		executeThenAction(actCtx, srv, rs.rule)
		cancel()
	}
	if release && rs.pinned {
		if now.Before(rs.cooldownUntil) {
			return
		}
		unpinAll()
		rs.pinned = false
		if cd > 0 {
			rs.cooldownUntil = now.Add(cd)
		}
	}
}

// ---------------------------------------------------------------------------
// Simulation helpers
// ---------------------------------------------------------------------------

// simulateNode is one leaf of a composed condition plus its instantaneous match
// result. The simulate endpoint returns these so an administrator can see which
// part of an and/or tree is (not) satisfied right now.
type simulateNode struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Matched  bool   `json:"matched"`
}

// simulateStep is one ordered step of a sequence trigger plus its instantaneous
// match result and hold setting. It deliberately carries no fire verdict: the
// real trigger depends on holds and window history the endpoint never watched.
type simulateStep struct {
	Index      int  `json:"index"`
	Matched    bool `json:"matched"`
	ForSeconds int  `json:"for_seconds"`
}

// collectConditionLeaves flattens a condition tree into its leaves in document
// order, evaluating each leaf against the supplied (already scoped) state.
func collectConditionLeaves(node *datasource.ConditionNode, state map[string]any, out *[]simulateNode) {
	if node == nil {
		return
	}
	if !node.IsGroup() {
		*out = append(*out, simulateNode{
			Path:     node.Path,
			Operator: node.Operator,
			Matched:  datasource.EvaluateNode(state, node),
		})
		return
	}
	for _, child := range node.Conditions {
		collectConditionLeaves(child, state, out)
	}
}

// simulateSequenceSteps reports each step's instantaneous match and hold time.
// A step that is not matching right now may still be armed in the live engine;
// the response never claims a fire verdict.
func simulateSequenceSteps(seq *datasource.SequenceTrigger, state map[string]any) []simulateStep {
	if seq == nil {
		return nil
	}
	out := make([]simulateStep, 0, len(seq.Steps))
	for i, step := range seq.Steps {
		out = append(out, simulateStep{
			Index:      i,
			Matched:    datasource.EvaluateNode(state, step.Condition),
			ForSeconds: step.ForSeconds,
		})
	}
	return out
}
