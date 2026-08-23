package datasource

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHomeAssistantCurrentState(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(t *testing.T, m map[string]any)
	}{
		{
			name: "array states",
			body: `[{"entity_id":"light.kitchen","state":"on","attributes":{"brightness":100}}]`,
			check: func(t *testing.T, m map[string]any) {
				if _, ok := m["states"]; !ok {
					t.Fatalf("expected states key, got %v", m)
				}
				if m["count"] != 1 {
					t.Fatalf("count=%v", m["count"])
				}
				arr, ok := m["states"].([]any)
				if !ok || len(arr) != 1 {
					t.Fatalf("states=%v", m["states"])
				}
				first, ok := arr[0].(map[string]any)
				if !ok || first["state"] != "on" {
					t.Fatalf("first=%v", arr[0])
				}
			},
		},
		{
			name: "object single",
			body: `{"entity_id":"sensor.temp","state":"22","attributes":{"unit":"C"}}`,
			check: func(t *testing.T, m map[string]any) {
				if m["state"] != "22" {
					t.Fatalf("state=%v", m["state"])
				}
				if _, ok := m["attributes"]; !ok {
					t.Fatalf("expected attributes, got %v", m)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			ds := &HomeAssistantDS{URL: srv.URL, Token: "tok"}
			m, err := ds.CurrentState(t.Context())
			if err != nil {
				t.Fatalf("CurrentState: %v", err)
			}
			tt.check(t, m)
		})
	}
	t.Run("error on failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", 500)
		}))
		defer srv.Close()
		ds := &HomeAssistantDS{URL: srv.URL, Token: "tok"}
		if _, err := ds.CurrentState(t.Context()); err == nil {
			t.Fatal("expected error")
		}
	})
}
