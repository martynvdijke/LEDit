package datasource

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePluginManifestValid(t *testing.T) {
	raw := `{
		"name": "sensor",
		"version": "1.2.0",
		"description": "reads a sensor",
		"author": "me",
		"kind": "exec",
		"target": "/opt/ledit/sensor",
		"timeout_ms": 2000,
		"fields": [
			{"key": "host", "label": "Host", "type": "text", "required": true},
			{"key": "port", "label": "Port", "type": "number", "default": "80"},
			{"key": "mode", "label": "Mode", "type": "select", "options": ["a", "b"]},
			{"key": "verbose", "label": "Verbose", "type": "bool"}
		]
	}`
	m, err := ParsePluginManifest(raw)
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.Name != "sensor" || len(m.Fields) != 4 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if m.Fields[2].Type != "select" || len(m.Fields[2].Options) != 2 {
		t.Fatalf("select field not parsed: %+v", m.Fields[2])
	}
}

func TestParsePluginManifestEmptyIsNil(t *testing.T) {
	m, err := ParsePluginManifest("   ")
	if err != nil || m != nil {
		t.Fatalf("empty manifest: m=%v err=%v", m, err)
	}
}

func TestParsePluginManifestRejects(t *testing.T) {
	cases := map[string]string{
		"malformed":         `{"name":`,
		"unknown top field": `{"name":"x","bogus":1}`,
		"empty key":         `{"fields":[{"key":"","type":"text"}]}`,
		"duplicate key":     `{"fields":[{"key":"a","type":"text"},{"key":"a","type":"text"}]}`,
		"unknown type":      `{"fields":[{"key":"a","type":"color"}]}`,
		"select no options": `{"fields":[{"key":"a","type":"select"}]}`,
	}
	for name, raw := range cases {
		if _, err := ParsePluginManifest(raw); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestValidatePluginConfig(t *testing.T) {
	m, err := ParsePluginManifest(`{"fields":[
		{"key":"host","type":"text","required":true},
		{"key":"port","type":"number"},
		{"key":"mode","type":"select","options":["a","b"]},
		{"key":"verbose","type":"bool"}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{
		`{"host":"h"}`,
		`{"host":"h","port":80}`,
		`{"host":"h","port":"80"}`,
		`{"host":"h","mode":"a"}`,
		`{"host":"h","verbose":true}`,
		`{"host":"h","extra":"ignored"}`,
	}
	for _, raw := range ok {
		if err := ValidatePluginConfig(m, json.RawMessage(raw)); err != nil {
			t.Errorf("config %s should be valid: %v", raw, err)
		}
	}
	bad := []string{
		`{}`,
		`{"host":"   "}`,
		`{"host":"h","port":"eighty"}`,
		`{"host":"h","mode":"c"}`,
		`{"host":"h","verbose":"yes"}`,
		`[1,2,3]`,
	}
	for _, raw := range bad {
		if err := ValidatePluginConfig(m, json.RawMessage(raw)); err == nil {
			t.Errorf("config %s should be invalid", raw)
		}
	}
}

func TestValidatePluginConfigNilManifestAcceptsObject(t *testing.T) {
	if err := ValidatePluginConfig(nil, json.RawMessage(`{"anything":true}`)); err != nil {
		t.Fatalf("nil manifest should accept object: %v", err)
	}
	if err := ValidatePluginConfig(nil, json.RawMessage(`[]`)); err == nil {
		t.Fatal("nil manifest should reject non-object")
	}
}

func TestPluginSourceInfoPath(t *testing.T) {
	ResetPluginHealth()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req PluginRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if string(req.Config) != `{"host":"h"}` {
			t.Errorf("config not forwarded: %s", req.Config)
		}
		w.Write([]byte(`{"v":1,"rows":[{"label":"a","value":"1","text":"t"}]}`))
	}))
	defer srv.Close()

	info := &PluginInfo{ID: 7, Kind: "http", Target: srv.URL, Enabled: true, TimeoutMs: 1000}
	ps := &PluginSource{PluginID: 7, Config: json.RawMessage(`{"host":"h"}`), Info: info}
	img, err := ps.GetPNG(64, 32)
	if err != nil {
		t.Fatalf("render via Info: %v", err)
	}
	if img == nil || img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatalf("bad image: %+v", img)
	}
	if h := GetPluginHealth(7); h == nil || h.LastError != "" {
		t.Fatalf("health not recorded: %+v", h)
	}
}

func TestPluginSourceNoInfoNoFetcher(t *testing.T) {
	ps := &PluginSource{PluginID: 1}
	if _, err := ps.GetPNG(64, 32); err == nil {
		t.Fatal("expected error when neither Info nor Fetcher set")
	}
}
