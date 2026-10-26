package datasource

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Composed rule conditions. A condition is either a leaf (the shape
// ParseCondition already accepts) or a group of two or more child conditions
// combined with "and"/"or". Groups nest up to MaxConditionGroupDepth levels and
// a whole tree may hold at most MaxConditionLeaves leaves, so a stored rule can
// never make the evaluator do unbounded work.
const (
	// MaxConditionGroupDepth is the deepest allowed group nesting (the root
	// group counts as level 1).
	MaxConditionGroupDepth = 4
	// MaxConditionLeaves is the total leaf budget for one condition tree.
	MaxConditionLeaves = 32
	// MaxHoldSeconds bounds for_seconds values for rules and sequence steps.
	MaxHoldSeconds = 86400
	// MinSequenceSteps and MaxSequenceSteps bound a sequence trigger.
	MinSequenceSteps = 2
	MaxSequenceSteps = 4
	// MinSequenceWindowSeconds and MaxSequenceWindowSeconds bound the window in
	// which a sequence has to complete.
	MinSequenceWindowSeconds = 5
	MaxSequenceWindowSeconds = 86400
)

// ConditionNode is a node in a composed condition tree. Leaves carry
// path/operator/value exactly as Condition does; groups carry op/conditions.
type ConditionNode struct {
	// Leaf fields.
	Path     string `json:"path,omitempty"`
	Operator string `json:"operator,omitempty"`
	Value    any    `json:"value,omitempty"`

	// Group fields.
	Op         string           `json:"op,omitempty"`
	Conditions []*ConditionNode `json:"conditions,omitempty"`
}

// IsGroup reports whether the node combines child conditions instead of
// evaluating a single leaf.
func (n *ConditionNode) IsGroup() bool {
	return n != nil && (n.Op != "" || len(n.Conditions) > 0)
}

// Condition projects a leaf node back onto the legacy Condition shape so leaf
// evaluation keeps using Evaluate.
func (n *ConditionNode) Condition() Condition {
	return Condition{Path: n.Path, Operator: n.Operator, Value: n.Value}
}

// ParseConditionNode parses raw JSON into a composed condition tree. Legacy
// single-leaf conditions are valid trees, so existing rows keep working
// unchanged. Bounds are enforced here so callers can reject a rule at save time
// with a readable message.
func ParseConditionNode(raw string) (*ConditionNode, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, fmt.Errorf("invalid condition JSON: %w", err)
	}
	leaves := 0
	node, err := parseConditionObject(probe, 1, &leaves)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// parseConditionObject parses one JSON object. groupLevel is the nesting level
// this object would occupy when it turns out to be a group.
func parseConditionObject(probe map[string]json.RawMessage, groupLevel int, leaves *int) (*ConditionNode, error) {
	_, hasOp := probe["op"]
	_, hasConditions := probe["conditions"]
	_, hasPath := probe["path"]
	_, hasOperator := probe["operator"]
	_, hasValue := probe["value"]

	groupKeys := hasOp || hasConditions
	leafKeys := hasPath || hasOperator || hasValue

	if groupKeys && leafKeys {
		return nil, fmt.Errorf("a condition group cannot mix leaf fields (path/operator/value); use either a group or a leaf")
	}
	if !groupKeys {
		node := &ConditionNode{}
		if hasPath {
			if err := json.Unmarshal(probe["path"], &node.Path); err != nil {
				return nil, fmt.Errorf("invalid condition path: %w", err)
			}
		}
		if hasOperator {
			if err := json.Unmarshal(probe["operator"], &node.Operator); err != nil {
				return nil, fmt.Errorf("invalid condition operator: %w", err)
			}
		}
		if hasValue {
			if err := json.Unmarshal(probe["value"], &node.Value); err != nil {
				return nil, fmt.Errorf("invalid condition value: %w", err)
			}
		}
		if !validOperators[node.Operator] {
			return nil, fmt.Errorf("invalid operator %q: must be one of eq/ne/gt/lt/ge/le/contains/exists", node.Operator)
		}
		*leaves++
		if *leaves > MaxConditionLeaves {
			return nil, fmt.Errorf("condition has more than %d leaves; split it into several rules", MaxConditionLeaves)
		}
		return node, nil
	}

	if groupLevel > MaxConditionGroupDepth {
		return nil, fmt.Errorf("condition groups nest at most %d levels deep; flatten or split the condition", MaxConditionGroupDepth)
	}
	if !hasOp {
		return nil, fmt.Errorf("condition group is missing its \"op\" (and/or)")
	}
	node := &ConditionNode{}
	if err := json.Unmarshal(probe["op"], &node.Op); err != nil {
		return nil, fmt.Errorf("invalid condition group op: %w", err)
	}
	if node.Op != "and" && node.Op != "or" {
		return nil, fmt.Errorf("invalid group operator %q: must be \"and\" or \"or\"", node.Op)
	}
	if !hasConditions {
		return nil, fmt.Errorf("condition group %q requires between 2 and %d child conditions (0 given)", node.Op, MaxConditionLeaves)
	}
	var children []json.RawMessage
	if err := json.Unmarshal(probe["conditions"], &children); err != nil {
		return nil, fmt.Errorf("invalid condition group children: %w", err)
	}
	if len(children) < 2 {
		return nil, fmt.Errorf("condition group %q requires at least 2 child conditions (%d given)", node.Op, len(children))
	}
	node.Conditions = make([]*ConditionNode, 0, len(children))
	for i, childRaw := range children {
		var childProbe map[string]json.RawMessage
		if err := json.Unmarshal(childRaw, &childProbe); err != nil {
			return nil, fmt.Errorf("child %d of group %q is not a condition object: %w", i+1, node.Op, err)
		}
		child, err := parseConditionObject(childProbe, groupLevel+1, leaves)
		if err != nil {
			return nil, err
		}
		node.Conditions = append(node.Conditions, child)
	}
	return node, nil
}

// EvaluateNode evaluates a composed condition tree against state. It is a pure
// function: AND short-circuits on the first false leaf, OR on the first true
// leaf, and every leaf is evaluated with the existing leaf semantics.
func EvaluateNode(state map[string]any, node *ConditionNode) bool {
	if node == nil {
		return false
	}
	if !node.IsGroup() {
		return Evaluate(state, node.Condition())
	}
	if node.Op == "and" {
		for _, child := range node.Conditions {
			if !EvaluateNode(state, child) {
				return false
			}
		}
		return true
	}
	for _, child := range node.Conditions {
		if EvaluateNode(state, child) {
			return true
		}
	}
	return false
}

// SequenceStep is one ordered step of a sequence trigger: a composed condition
// that has to hold for ForSeconds before the run advances.
type SequenceStep struct {
	Condition  *ConditionNode `json:"condition"`
	ForSeconds int            `json:"for_seconds"`
}

// SequenceTrigger is an ordered chain of sustained conditions that fires once,
// after the final step completes inside WindowSeconds of the first step
// completing.
type SequenceTrigger struct {
	WindowSeconds int            `json:"window_seconds"`
	Steps         []SequenceStep `json:"steps"`
}

// ParseSequence parses raw JSON into a sequence trigger, enforcing the step
// count, per-step hold bounds and the window fit against the rule's effective
// check interval. checkIntervalSeconds is the rule's configured interval; values
// below the 5s floor are treated as the floor.
func ParseSequence(raw string, checkIntervalSeconds int) (*SequenceTrigger, error) {
	interval := EffectiveCheckInterval(checkIntervalSeconds)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, fmt.Errorf("invalid sequence JSON: %w", err)
	}

	seq := &SequenceTrigger{}
	if rawWindow, ok := probe["window_seconds"]; ok {
		if err := json.Unmarshal(rawWindow, &seq.WindowSeconds); err != nil {
			return nil, fmt.Errorf("invalid sequence window_seconds: %w", err)
		}
	}
	if seq.WindowSeconds < MinSequenceWindowSeconds || seq.WindowSeconds > MaxSequenceWindowSeconds {
		return nil, fmt.Errorf("sequence window_seconds must be between %d and %d", MinSequenceWindowSeconds, MaxSequenceWindowSeconds)
	}

	rawSteps, ok := probe["steps"]
	if !ok {
		return nil, fmt.Errorf("sequence requires between %d and %d steps (0 given)", MinSequenceSteps, MaxSequenceSteps)
	}
	var stepProbes []map[string]json.RawMessage
	if err := json.Unmarshal(rawSteps, &stepProbes); err != nil {
		return nil, fmt.Errorf("invalid sequence steps: %w", err)
	}
	if len(stepProbes) < MinSequenceSteps || len(stepProbes) > MaxSequenceSteps {
		return nil, fmt.Errorf("sequence requires between %d and %d steps (%d given)", MinSequenceSteps, MaxSequenceSteps, len(stepProbes))
	}

	seq.Steps = make([]SequenceStep, 0, len(stepProbes))
	for i, stepProbe := range stepProbes {
		step := SequenceStep{}
		rawCondition, ok := stepProbe["condition"]
		if !ok {
			return nil, fmt.Errorf("sequence step %d is missing its condition", i+1)
		}
		condition, err := ParseConditionNode(string(rawCondition))
		if err != nil {
			return nil, fmt.Errorf("sequence step %d: %w", i+1, err)
		}
		step.Condition = condition
		if rawFor, ok := stepProbe["for_seconds"]; ok {
			if err := json.Unmarshal(rawFor, &step.ForSeconds); err != nil {
				return nil, fmt.Errorf("sequence step %d: invalid for_seconds: %w", i+1, err)
			}
		}
		if step.ForSeconds < 0 || step.ForSeconds > MaxHoldSeconds {
			return nil, fmt.Errorf("sequence step %d: for_seconds must be between 0 and %d", i+1, MaxHoldSeconds)
		}
		if step.ForSeconds != 0 && step.ForSeconds < interval {
			return nil, fmt.Errorf("sequence step %d: for_seconds must be 0 or at least the check interval (%ds)", i+1, interval)
		}
		seq.Steps = append(seq.Steps, step)
	}

	// The window has to leave room for every step after the first to reach its
	// hold, plus one check interval to notice completion.
	required := interval
	for _, step := range seq.Steps[1:] {
		required += step.ForSeconds
	}
	if seq.WindowSeconds < required {
		return nil, fmt.Errorf("sequence window_seconds must be at least %ds to fit the step holds plus one check interval", required)
	}
	return seq, nil
}

// EffectiveCheckInterval clamps a configured check interval to the engine's 5s
// floor.
func EffectiveCheckInterval(seconds int) int {
	if seconds < 5 {
		return 5
	}
	return seconds
}

// ValidateHoldSeconds checks a rule-level or step-level for_seconds value: zero
// means "fire immediately" and any other value has to be at least one check
// interval so a hold can ever be observed.
func ValidateHoldSeconds(forSeconds, checkIntervalSeconds int) error {
	if forSeconds < 0 || forSeconds > MaxHoldSeconds {
		return fmt.Errorf("for_seconds must be between 0 and %d", MaxHoldSeconds)
	}
	interval := EffectiveCheckInterval(checkIntervalSeconds)
	if forSeconds != 0 && forSeconds < interval {
		return fmt.Errorf("for_seconds must be 0 or at least the check interval (%ds)", interval)
	}
	return nil
}

// FormatConditionNode renders a composed condition the way the admin list and
// simulation views show it: `cpu gt 80 and (temp lt 20 or wind gt 30)`.
func FormatConditionNode(node *ConditionNode) string {
	if node == nil {
		return ""
	}
	if !node.IsGroup() {
		return FormatCondition(node.Condition())
	}
	parts := make([]string, 0, len(node.Conditions))
	for _, child := range node.Conditions {
		text := FormatConditionNode(child)
		if child.IsGroup() {
			text = "(" + text + ")"
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " "+node.Op+" ")
}

// FormatCondition renders a single leaf condition as `path operator value`.
func FormatCondition(cond Condition) string {
	switch {
	case cond.Operator == "exists":
		return strings.TrimSpace(cond.Path + " exists")
	case cond.Value == nil:
		return strings.TrimSpace(fmt.Sprintf("%s %s null", cond.Path, cond.Operator))
	default:
		return fmt.Sprintf("%s %s %v", cond.Path, cond.Operator, cond.Value)
	}
}

// FormatSequence renders a sequence trigger as
// `door eq open → garage.door eq closed within 15m`.
func FormatSequence(seq *SequenceTrigger) string {
	if seq == nil || len(seq.Steps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seq.Steps))
	for _, step := range seq.Steps {
		text := FormatConditionNode(step.Condition)
		if step.ForSeconds > 0 {
			text = fmt.Sprintf("%s (for %s)", text, FormatSeconds(step.ForSeconds))
		}
		parts = append(parts, text)
	}
	return fmt.Sprintf("%s within %s", strings.Join(parts, " → "), FormatSeconds(seq.WindowSeconds))
}

// FormatSeconds renders a duration in seconds as a compact human string.
func FormatSeconds(seconds int) string {
	switch {
	case seconds <= 0:
		return "0s"
	case seconds%3600 == 0:
		return fmt.Sprintf("%dh", seconds/3600)
	case seconds%60 == 0:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// FormatTriggerSummary renders the readable trigger summary shown in the admin
// rule list. Unparseable triggers fall back to their raw JSON so hand-edited or
// legacy rows still render something.
func FormatTriggerSummary(triggerKind, condition, sequence string, checkIntervalSeconds int) string {
	if triggerKind == "sequence" {
		seq, err := ParseSequence(sequence, checkIntervalSeconds)
		if err != nil {
			return strings.TrimSpace(sequence)
		}
		return FormatSequence(seq)
	}
	node, err := ParseConditionNode(condition)
	if err != nil {
		return strings.TrimSpace(condition)
	}
	return FormatConditionNode(node)
}
