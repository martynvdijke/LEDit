package datasource

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Condition represents a rule condition as stored in DisplayRule.condition.
type Condition struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

var validOperators = map[string]bool{
	"eq": true, "ne": true, "gt": true, "lt": true,
	"ge": true, "le": true, "contains": true, "exists": true,
}

// ParseCondition parses raw JSON into a Condition. It requires a strict JSON
// object with a valid operator.
func ParseCondition(raw string) (Condition, error) {
	var c Condition
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Condition{}, fmt.Errorf("invalid condition JSON: %w", err)
	}
	if !validOperators[c.Operator] {
		return Condition{}, fmt.Errorf("invalid operator %q: must be one of eq/ne/gt/lt/ge/le/contains/exists", c.Operator)
	}
	return c, nil
}

// Evaluate evaluates cond against state. It is a pure function.
// Uses DotPath to resolve cond.Path.
func Evaluate(state map[string]any, cond Condition) bool {
	// exists is special: true if path resolves.
	if cond.Operator == "exists" {
		_, ok := DotPath(state, cond.Path)
		return ok
	}

	actual, ok := DotPath(state, cond.Path)
	if !ok {
		return false
	}

	switch cond.Operator {
	case "eq":
		return equalValues(actual, cond.Value)
	case "ne":
		return !equalValues(actual, cond.Value)
	case "gt", "lt", "ge", "le":
		return compareNumeric(actual, cond.Value, cond.Operator)
	case "contains":
		return containsOp(actual, cond.Value)
	default:
		return false
	}
}

func equalValues(a, b any) bool {
	// bool strict
	if ab, ok := a.(bool); ok {
		if bb, ok := b.(bool); ok {
			return ab == bb
		}
		return false
	}
	if _, ok := b.(bool); ok {
		return false
	}
	// numeric coercion: if both parse as float64, compare numerically
	if af, aok := toFloat64(a); aok {
		if bf, bok := toFloat64(b); bok {
			return af == bf
		}
	}
	// fallback to string equality
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func compareNumeric(a, b any, op string) bool {
	af, aok := toFloat64(a)
	bf, bok := toFloat64(b)
	if !aok || !bok {
		return false
	}
	switch op {
	case "gt":
		return af > bf
	case "lt":
		return af < bf
	case "ge":
		return af >= bf
	case "le":
		return af <= bf
	default:
		return false
	}
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err == nil {
			return f, true
		}
		return 0, false
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err == nil {
			return f, true
		}
		return 0, false
	default:
		// try via reflection for other numeric types
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Float32, reflect.Float64:
			return rv.Convert(reflect.TypeOf(float64(0))).Float(), true
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(rv.Int()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return float64(rv.Uint()), true
		}
		return 0, false
	}
}

func containsOp(actual, expected any) bool {
	// string contains substring
	if s, ok := actual.(string); ok {
		sub := fmt.Sprint(expected)
		return strings.Contains(s, sub)
	}
	// array/slice contains element
	rv := reflect.ValueOf(actual)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		for i := 0; i < rv.Len(); i++ {
			elem := rv.Index(i).Interface()
			if equalValues(elem, expected) {
				return true
			}
		}
		return false
	}
	return false
}
