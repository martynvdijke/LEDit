package datasource

import (
	"encoding/json"
	"strconv"
	"strings"
)

// DotPath walks a decoded JSON value following dot-paths like
// "data.btc.usd" or "items.0.name". Returns (value, true) when resolved.
// It returns the raw leaf value (any) without string conversion.
func DotPath(root any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	cur := root
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func dotPathToString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch val := v.(type) {
	case string:
		return val, true
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(val), true
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}
