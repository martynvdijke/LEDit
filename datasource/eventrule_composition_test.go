package datasource

import (
	"strings"
	"testing"
)

// state fixture shared by the composed-condition tables.
func compState() map[string]any {
	return map[string]any{
		"cpu":  80.0,
		"temp": 10.0,
		"wind": 40.0,
		"door": "open",
		"mode": true,
		"tags": []any{"a", "b"},
		"nested": map[string]any{
			"value": 3.0,
		},
	}
}

func TestParseConditionNodeLegacyLeafParity(t *testing.T) {
	cases := []string{
		`{"path":"cpu","operator":"gt","value":80}`,
		`{"path":"door","operator":"eq","value":"open"}`,
		`{"path":"door","operator":"exists"}`,
		`{"path":"nested.value","operator":"le","value":3}`,
		`{"path":"tags","operator":"contains","value":"b"}`,
		`{"path":"","operator":"eq","value":1}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			legacy, err := ParseCondition(raw)
			if err != nil {
				t.Fatalf("ParseCondition: %v", err)
			}
			node, err := ParseConditionNode(raw)
			if err != nil {
				t.Fatalf("ParseConditionNode: %v", err)
			}
			if node.IsGroup() {
				t.Fatalf("legacy leaf parsed as a group: %+v", node)
			}
			want := Evaluate(compState(), legacy)
			if got := EvaluateNode(compState(), node); got != want {
				t.Fatalf("EvaluateNode=%v, Evaluate=%v", got, want)
			}
		})
	}
}

func TestParseConditionNodeLegacyErrorParity(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"path":"cpu","operator":"nope"}`,
		`{"path":"cpu","operator":"nope","value":1}`,
	} {
		_, legacyErr := ParseCondition(raw)
		_, nodeErr := ParseConditionNode(raw)
		if legacyErr == nil || nodeErr == nil {
			t.Fatalf("%s: expected both parsers to fail (legacy=%v node=%v)", raw, legacyErr, nodeErr)
		}
		if legacyErr.Error() != nodeErr.Error() {
			t.Fatalf("%s: error mismatch\n legacy: %v\n node:   %v", raw, legacyErr, nodeErr)
		}
	}
}

func TestEvaluateNodeComposedTruthTables(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "and all true",
			raw:  `{"op":"and","conditions":[{"path":"cpu","operator":"ge","value":80},{"path":"wind","operator":"lt","value":50}]}`,
			want: true,
		},
		{
			name: "and one false",
			raw:  `{"op":"and","conditions":[{"path":"cpu","operator":"ge","value":80},{"path":"wind","operator":"gt","value":50}]}`,
			want: false,
		},
		{
			name: "or one true",
			raw:  `{"op":"or","conditions":[{"path":"cpu","operator":"gt","value":500},{"path":"door","operator":"eq","value":"open"}]}`,
			want: true,
		},
		{
			name: "or all false",
			raw:  `{"op":"or","conditions":[{"path":"cpu","operator":"gt","value":500},{"path":"door","operator":"eq","value":"closed"}]}`,
			want: false,
		},
		{
			name: "nested and inside or",
			raw:  `{"op":"or","conditions":[{"path":"cpu","operator":"gt","value":500},{"op":"and","conditions":[{"path":"temp","operator":"lt","value":20},{"path":"wind","operator":"gt","value":30}]}]}`,
			want: true,
		},
		{
			name: "nested depth four",
			raw:  `{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"path":"mode","operator":"eq","value":true},{"path":"mode","operator":"ne","value":false}]},{"path":"door","operator":"eq","value":"open"}]},{"path":"door","operator":"eq","value":"open"}]},{"path":"door","operator":"eq","value":"open"}]}`,
			want: true,
		},
		{
			name: "exists present in group",
			raw:  `{"op":"and","conditions":[{"path":"nested.value","operator":"exists"},{"path":"missing","operator":"exists"}]}`,
			want: false,
		},
		{
			name: "exists present only",
			raw:  `{"op":"or","conditions":[{"path":"nested.value","operator":"exists"},{"path":"missing","operator":"exists"}]}`,
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := ParseConditionNode(tc.raw)
			if err != nil {
				t.Fatalf("ParseConditionNode: %v", err)
			}
			if got := EvaluateNode(compState(), node); got != tc.want {
				t.Fatalf("EvaluateNode=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateNodeUnresolvablePathsAreSafeFalse(t *testing.T) {
	// A comparison on a missing path is false, and that false short-circuits the
	// AND even though the remaining child would be true.
	rawAnd := `{"op":"and","conditions":[{"path":"missing.value","operator":"gt","value":1},{"path":"cpu","operator":"ge","value":80}]}`
	node, err := ParseConditionNode(rawAnd)
	if err != nil {
		t.Fatalf("ParseConditionNode: %v", err)
	}
	if EvaluateNode(compState(), node) {
		t.Fatal("AND with a false unresolvable leaf should be false")
	}

	// A true child short-circuits the OR even though the rest is unresolvable.
	rawOr := `{"op":"or","conditions":[{"path":"cpu","operator":"ge","value":80},{"path":"missing.tags","operator":"contains","value":"z"}]}`
	node, err = ParseConditionNode(rawOr)
	if err != nil {
		t.Fatalf("ParseConditionNode: %v", err)
	}
	if !EvaluateNode(compState(), node) {
		t.Fatal("OR with a true leaf should be true")
	}

	// nil state never panics and never matches.
	if EvaluateNode(nil, node) {
		t.Fatal("nil state should evaluate false")
	}
	if EvaluateNode(compState(), nil) {
		t.Fatal("nil node should evaluate false")
	}
}

func TestParseConditionNodeRejects(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{"malformed json", `{`, "invalid condition JSON"},
		{"json array", `[1,2]`, "invalid condition JSON"},
		{"empty object", `{}`, "invalid operator"},
		{"invalid leaf operator", `{"path":"cpu","operator":"between","value":1}`, "invalid operator"},
		{"group single child", `{"op":"and","conditions":[{"path":"cpu","operator":"gt","value":1}]}`, "at least 2 child conditions"},
		{"group no children", `{"op":"or","conditions":[]}`, "at least 2 child conditions"},
		{"group missing op", `{"conditions":[{"path":"cpu","operator":"gt","value":1},{"path":"cpu","operator":"lt","value":5}]}`, `missing its "op"`},
		{"group invalid op", `{"op":"xor","conditions":[{"path":"cpu","operator":"gt","value":1},{"path":"cpu","operator":"lt","value":5}]}`, `must be "and" or "or"`},
		{"group with leaf fields", `{"op":"and","path":"cpu","conditions":[{"path":"cpu","operator":"gt","value":1},{"path":"cpu","operator":"lt","value":5}]}`, "cannot mix leaf fields"},
		{"child not an object", `{"op":"and","conditions":[1,{"path":"cpu","operator":"gt","value":1}]}`, "not a condition object"},
		{"child invalid", `{"op":"and","conditions":[{"path":"cpu","operator":"gt","value":1},{"path":"cpu","operator":"nope"}]}`, "invalid operator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConditionNode(tc.raw)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// nestedCondition builds a group tree of the requested depth, each level holding
// the previous one plus a leaf.
func nestedCondition(depth int) string {
	leaf := `{"path":"cpu","operator":"gt","value":1}`
	text := leaf
	for i := 0; i < depth; i++ {
		text = `{"op":"and","conditions":[` + text + `,` + leaf + `]}`
	}
	return text
}

func TestParseConditionNodeEnforcesGroupDepth(t *testing.T) {
	if _, err := ParseConditionNode(nestedCondition(MaxConditionGroupDepth)); err != nil {
		t.Fatalf("depth %d should be accepted: %v", MaxConditionGroupDepth, err)
	}
	_, err := ParseConditionNode(nestedCondition(MaxConditionGroupDepth + 1))
	if err == nil {
		t.Fatalf("depth %d should be rejected", MaxConditionGroupDepth+1)
	}
	if !strings.Contains(err.Error(), "nest at most 4 levels deep") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConditionNodeEnforcesLeafBudget(t *testing.T) {
	leaf := `{"path":"cpu","operator":"gt","value":1}`
	manyLeaves := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = leaf
		}
		return `{"op":"and","conditions":[` + strings.Join(parts, ",") + `]}`
	}
	if _, err := ParseConditionNode(manyLeaves(MaxConditionLeaves)); err != nil {
		t.Fatalf("%d leaves should be accepted: %v", MaxConditionLeaves, err)
	}
	_, err := ParseConditionNode(manyLeaves(MaxConditionLeaves + 1))
	if err == nil {
		t.Fatal("too many leaves should be rejected")
	}
	if !strings.Contains(err.Error(), "more than 32 leaves") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Nested leaves count towards the budget too, not just top-level ones.
	over := `{"op":"and","conditions":[` + manyLeaves(MaxConditionLeaves) + `,` + leaf + `]}`
	if _, err := ParseConditionNode(over); err == nil {
		t.Fatal("leaves in nested groups should count towards the budget")
	}
}

func TestParseSequenceHappyPath(t *testing.T) {
	raw := `{"window_seconds":900,"steps":[
		{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":600},
		{"condition":{"op":"or","conditions":[{"path":"motion","operator":"eq","value":true},{"path":"light","operator":"gt","value":10}]},"for_seconds":60}
	]}`
	seq, err := ParseSequence(raw, 30)
	if err != nil {
		t.Fatalf("ParseSequence: %v", err)
	}
	if seq.WindowSeconds != 900 || len(seq.Steps) != 2 {
		t.Fatalf("unexpected sequence: %+v", seq)
	}
	if seq.Steps[0].ForSeconds != 600 || !seq.Steps[1].Condition.IsGroup() {
		t.Fatalf("unexpected steps: %+v", seq.Steps)
	}
}

func TestParseSequenceRejects(t *testing.T) {
	step := `{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0}`
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{"malformed", `{`, "invalid sequence JSON"},
		{"missing steps", `{"window_seconds":900}`, "between 2 and 4 steps (0 given)"},
		{"one step", `{"window_seconds":900,"steps":[` + step + `]}`, "between 2 and 4 steps (1 given)"},
		{"five steps", `{"window_seconds":900,"steps":[` + step + `,` + step + `,` + step + `,` + step + `,` + step + `]}`, "between 2 and 4 steps (5 given)"},
		{"window too small", `{"window_seconds":2,"steps":[` + step + `,` + step + `]}`, "window_seconds must be between 5 and 86400"},
		{"window too large", `{"window_seconds":90000,"steps":[` + step + `,` + step + `]}`, "window_seconds must be between 5 and 86400"},
		{"step without condition", `{"window_seconds":900,"steps":[{"for_seconds":0},` + step + `]}`, "step 1 is missing its condition"},
		{"step hold above interval", `{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":10},` + step + `]}`, "for_seconds must be 0 or at least the check interval (30s)"},
		{"step hold too large", `{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":90000},` + step + `]}`, "for_seconds must be between 0 and 86400"},
		{"window does not fit holds", `{"window_seconds":30,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":60},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":300}]}`, "window_seconds must be at least 330s"},
		{"invalid step condition", `{"window_seconds":900,"steps":[{"condition":{"op":"and","conditions":[]}},` + step + `]}`, "step 1: condition group"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSequence(tc.raw, 30)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
	// The window fit uses the rule's effective interval, which is floored at 5s.
	raw := `{"window_seconds":10,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":0}]}`
	seq, err := ParseSequence(raw, 5)
	if err != nil {
		t.Fatalf("5s interval should fit a 10s window: %v", err)
	}
	if len(seq.Steps) != 2 {
		t.Fatalf("unexpected sequence: %+v", seq)
	}
}

func TestValidateHoldSeconds(t *testing.T) {
	cases := []struct {
		forSeconds int
		interval   int
		wantErr    bool
	}{
		{0, 30, false},
		{30, 30, false},
		{60, 30, false},
		{29, 30, true},
		{-1, 30, true},
		{MaxHoldSeconds, 30, false},
		{MaxHoldSeconds + 1, 30, true},
		{5, 1, false},
	}
	for _, tc := range cases {
		err := ValidateHoldSeconds(tc.forSeconds, tc.interval)
		if (err != nil) != tc.wantErr {
			t.Fatalf("ValidateHoldSeconds(%d, %d) error=%v, wantErr=%v", tc.forSeconds, tc.interval, err, tc.wantErr)
		}
	}
}

func TestEffectiveCheckInterval(t *testing.T) {
	if got := EffectiveCheckInterval(0); got != 5 {
		t.Fatalf("EffectiveCheckInterval(0)=%d, want 5", got)
	}
	if got := EffectiveCheckInterval(3); got != 5 {
		t.Fatalf("EffectiveCheckInterval(3)=%d, want 5", got)
	}
	if got := EffectiveCheckInterval(30); got != 30 {
		t.Fatalf("EffectiveCheckInterval(30)=%d, want 30", got)
	}
}

func TestFormatting(t *testing.T) {
	node, err := ParseConditionNode(`{"op":"and","conditions":[{"path":"cpu","operator":"gt","value":80},{"op":"or","conditions":[{"path":"temp","operator":"lt","value":20},{"path":"wind","operator":"gt","value":30}]}]}`)
	if err != nil {
		t.Fatalf("ParseConditionNode: %v", err)
	}
	if got, want := FormatConditionNode(node), "cpu gt 80 and (temp lt 20 or wind gt 30)"; got != want {
		t.Fatalf("FormatConditionNode=%q, want %q", got, want)
	}

	leaf, err := ParseConditionNode(`{"path":"door","operator":"exists"}`)
	if err != nil {
		t.Fatalf("ParseConditionNode: %v", err)
	}
	if got, want := FormatConditionNode(leaf), "door exists"; got != want {
		t.Fatalf("FormatConditionNode(exists)=%q, want %q", got, want)
	}

	nilValue, err := ParseConditionNode(`{"path":"door","operator":"eq","value":null}`)
	if err != nil {
		t.Fatalf("ParseConditionNode: %v", err)
	}
	if got, want := FormatConditionNode(nilValue), "door eq null"; got != want {
		t.Fatalf("FormatConditionNode(null)=%q, want %q", got, want)
	}

	seq, err := ParseSequence(`{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":600},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":0}]}`, 30)
	if err != nil {
		t.Fatalf("ParseSequence: %v", err)
	}
	if got, want := FormatSequence(seq), "door eq open (for 10m) → motion eq true within 15m"; got != want {
		t.Fatalf("FormatSequence=%q, want %q", got, want)
	}

	if got, want := FormatTriggerSummary("condition", `{"path":"cpu","operator":"gt","value":80}`, "", 30), "cpu gt 80"; got != want {
		t.Fatalf("FormatTriggerSummary(leaf)=%q, want %q", got, want)
	}
	grouped := `{"op":"or","conditions":[{"path":"temp","operator":"lt","value":20},{"path":"wind","operator":"gt","value":30}]}`
	if got, want := FormatTriggerSummary("condition", grouped, "", 30), "temp lt 20 or wind gt 30"; got != want {
		t.Fatalf("FormatTriggerSummary(group)=%q, want %q", got, want)
	}
	if got, want := FormatTriggerSummary("sequence", "{}", `{"window_seconds":60,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":0}]}`, 30), "door eq open → motion eq true within 1m"; got != want {
		t.Fatalf("FormatTriggerSummary(sequence)=%q, want %q", got, want)
	}
	// Unparseable triggers fall back to raw JSON rather than erroring.
	if got := FormatTriggerSummary("condition", `{oops`, "", 30); got != `{oops` {
		t.Fatalf("FormatTriggerSummary(invalid)=%q", got)
	}
	if got := FormatTriggerSummary("sequence", "{}", `{oops`, 30); got != `{oops` {
		t.Fatalf("FormatTriggerSummary(invalid sequence)=%q", got)
	}
	if got := FormatConditionNode(nil); got != "" {
		t.Fatalf("FormatConditionNode(nil)=%q", got)
	}
	if got := FormatSequence(nil); got != "" {
		t.Fatalf("FormatSequence(nil)=%q", got)
	}
}
