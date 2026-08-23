package datasource

import "testing"

func TestSystemStatsCurrentState(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"flat map shape"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &SystemStatsDS{}
			m, err := ds.CurrentState(t.Context())
			if err != nil {
				t.Fatalf("CurrentState: %v", err)
			}
			for _, k := range []string{"cpu_cores", "go_version", "os", "memory", "load"} {
				if _, ok := m[k]; !ok {
					t.Fatalf("missing key %q in %v", k, m)
				}
			}
			if _, ok := m["cpu_cores"].(int); !ok {
				t.Fatalf("cpu_cores should be int, got %T", m["cpu_cores"])
			}
		})
	}
}
