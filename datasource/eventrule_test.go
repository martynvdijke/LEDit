package datasource

import "testing"

func TestParseCondition(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		op      string
	}{
		{"valid eq", `{"path":"a","operator":"eq","value":1}`, false, "eq"},
		{"valid exists", `{"path":"a","operator":"exists","value":null}`, false, "exists"},
		{"invalid operator", `{"path":"a","operator":"bad","value":1}`, true, ""},
		{"invalid json", `not json`, true, ""},
		{"missing operator", `{"path":"a","value":1}`, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseCondition(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && c.Operator != tc.op {
				t.Fatalf("got op %q want %q", c.Operator, tc.op)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name  string
		state map[string]any
		cond  Condition
		want  bool
	}{
		// eq numeric
		{"eq numeric true", map[string]any{"a": float64(10)}, Condition{"a", "eq", float64(10)}, true},
		{"eq numeric false", map[string]any{"a": float64(10)}, Condition{"a", "eq", float64(11)}, false},
		{"eq numeric coercion string 10 vs int 10", map[string]any{"a": "10"}, Condition{"a", "eq", float64(10)}, true},
		{"eq numeric coercion string 10 vs string 10", map[string]any{"a": "10"}, Condition{"a", "eq", "10"}, true},
		{"eq numeric coercion string vs number", map[string]any{"a": "10"}, Condition{"a", "eq", 10}, true},
		// ne
		{"ne numeric true", map[string]any{"a": float64(10)}, Condition{"a", "ne", float64(11)}, true},
		{"ne numeric false", map[string]any{"a": float64(10)}, Condition{"a", "ne", float64(10)}, false},
		{"ne string", map[string]any{"a": "hello"}, Condition{"a", "ne", "world"}, true},
		// eq string
		{"eq string true", map[string]any{"a": "hello"}, Condition{"a", "eq", "hello"}, true},
		{"eq string false", map[string]any{"a": "hello"}, Condition{"a", "eq", "world"}, false},
		{"eq string fallback non-numeric gt values", map[string]any{"a": "hello"}, Condition{"a", "eq", "hello"}, true},
		// gt/lt/ge/le numeric
		{"gt numeric true", map[string]any{"a": float64(10)}, Condition{"a", "gt", float64(5)}, true},
		{"gt numeric false", map[string]any{"a": float64(10)}, Condition{"a", "gt", float64(10)}, false},
		{"lt numeric true", map[string]any{"a": float64(5)}, Condition{"a", "lt", float64(10)}, true},
		{"ge numeric true equal", map[string]any{"a": float64(10)}, Condition{"a", "ge", float64(10)}, true},
		{"ge numeric true greater", map[string]any{"a": float64(11)}, Condition{"a", "ge", float64(10)}, true},
		{"le numeric true equal", map[string]any{"a": float64(10)}, Condition{"a", "le", float64(10)}, true},
		{"le numeric true less", map[string]any{"a": float64(9)}, Condition{"a", "le", float64(10)}, true},
		// numeric coercion gt/lt with strings
		{"gt coercion string 10 vs int 5", map[string]any{"a": "10"}, Condition{"a", "gt", 5}, true},
		{"gt coercion string 10 vs string 5", map[string]any{"a": "10"}, Condition{"a", "gt", "5"}, true},
		{"lt coercion string 5 vs int 10", map[string]any{"a": "5"}, Condition{"a", "lt", 10}, true},
		// non-numeric gt should be false
		{"gt non-numeric false", map[string]any{"a": "hello"}, Condition{"a", "gt", "world"}, false},
		{"lt non-numeric false", map[string]any{"a": "hello"}, Condition{"a", "lt", "world"}, false},
		{"ge non-numeric false", map[string]any{"a": "hello"}, Condition{"a", "ge", "hello"}, false},
		{"le non-numeric false", map[string]any{"a": "hello"}, Condition{"a", "le", "hello"}, false},
		// eq/ne with non-numeric fallback
		{"eq non-numeric strings", map[string]any{"a": "foo"}, Condition{"a", "eq", "foo"}, true},
		{"ne non-numeric strings", map[string]any{"a": "foo"}, Condition{"a", "ne", "bar"}, true},
		// bool handling
		{"eq bool true", map[string]any{"a": true}, Condition{"a", "eq", true}, true},
		{"eq bool false", map[string]any{"a": true}, Condition{"a", "eq", false}, false},
		{"eq bool vs string false", map[string]any{"a": true}, Condition{"a", "eq", "true"}, false},
		{"ne bool true", map[string]any{"a": true}, Condition{"a", "ne", false}, true},
		{"ne bool false", map[string]any{"a": true}, Condition{"a", "ne", true}, false},
		{"eq bool vs int false", map[string]any{"a": true}, Condition{"a", "eq", 1}, false},
		// missing path
		{"missing path eq false", map[string]any{"a": 1}, Condition{"missing", "eq", 1}, false},
		{"missing path ne false", map[string]any{"a": 1}, Condition{"missing", "ne", 1}, false},
		{"missing path gt false", map[string]any{"a": 1}, Condition{"missing", "gt", 1}, false},
		{"missing path contains false", map[string]any{"a": 1}, Condition{"missing", "contains", "x"}, false},
		{"missing path exists false", map[string]any{"a": 1}, Condition{"missing", "exists", nil}, false},
		// exists
		{"exists true", map[string]any{"a": 1}, Condition{"a", "exists", nil}, true},
		{"exists true with nil value", map[string]any{"a": nil}, Condition{"a", "exists", nil}, true},
		{"exists true with false value", map[string]any{"a": false}, Condition{"a", "exists", nil}, true},
		{"exists nested", map[string]any{"a": map[string]any{"b": 1}}, Condition{"a.b", "exists", nil}, true},
		{"exists nested missing", map[string]any{"a": map[string]any{"b": 1}}, Condition{"a.c", "exists", nil}, false},
		// contains string
		{"contains string true", map[string]any{"a": "hello world"}, Condition{"a", "contains", "world"}, true},
		{"contains string false", map[string]any{"a": "hello world"}, Condition{"a", "contains", "xyz"}, false},
		{"contains string numeric coercion", map[string]any{"a": "hello 123"}, Condition{"a", "contains", 123}, true},
		// contains array
		{"contains array true int", map[string]any{"a": []any{float64(1), float64(2), float64(3)}}, Condition{"a", "contains", float64(2)}, true},
		{"contains array true string", map[string]any{"a": []any{"foo", "bar"}}, Condition{"a", "contains", "bar"}, true},
		{"contains array false", map[string]any{"a": []any{float64(1), float64(2)}}, Condition{"a", "contains", float64(5)}, false},
		{"contains array numeric coercion string 10 vs int 10", map[string]any{"a": []any{"10", "20"}}, Condition{"a", "contains", float64(10)}, true},
		{"contains array bool", map[string]any{"a": []any{true, false}}, Condition{"a", "contains", true}, true},
		{"contains wrong type false", map[string]any{"a": float64(123)}, Condition{"a", "contains", "123"}, false},
		{"contains missing path false", map[string]any{}, Condition{"missing", "contains", "x"}, false},
		// dot path nested
		{"nested dot path eq", map[string]any{"a": map[string]any{"b": map[string]any{"c": float64(42)}}}, Condition{"a.b.c", "eq", float64(42)}, true},
		{"array index path", map[string]any{"a": []any{map[string]any{"x": float64(5)}}}, Condition{"a.0.x", "eq", float64(5)}, true},
		// wrong type
		{"gt wrong type non-numeric", map[string]any{"a": true}, Condition{"a", "gt", 5}, false},
		{"eq numeric vs bool false", map[string]any{"a": true}, Condition{"a", "eq", 1}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.state, tc.cond)
			if got != tc.want {
				t.Fatalf("Evaluate(%v, %+v)=%v want %v", tc.state, tc.cond, got, tc.want)
			}
		})
	}
}
