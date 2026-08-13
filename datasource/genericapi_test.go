package datasource

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractDotPath(t *testing.T) {
	root := map[string]any{
		"data": map[string]any{
			"btc": map[string]any{"usd": 12345.5},
		},
		"items": []any{
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
		},
		"ok":   true,
		"text": "hello",
		"obj":  map[string]any{"a": 1},
		"nil":  nil,
		"num":  42,
	}
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"data.btc.usd", "12345.5", true},
		{"items.0.name", "first", true},
		{"items.1.name", "second", true},
		{"ok", "true", true},
		{"text", "hello", true},
		{"num", "42", true},
		{"obj", "{\"a\":1}", true},
		{"data.missing", "", false},
		{"items.5.name", "", false},
		{"items.x", "", false},
		{"nil", "", false},
		{"", "", false},
		{"data.btc.usd.extra", "", false},
	}
	for _, tt := range tests {
		got, ok := extractDotPath(root, tt.path)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Fatalf("extractDotPath(%q) = (%q, %v), want (%q, %v)", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}

func TestGenericAPIExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret" {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Accept") != "application/json" {
			http.Error(w, "missing accept", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"bitcoin":{"usd":12345.5},"sentiment":{"rating":"bullish"}}`))
	}))
	defer srv.Close()

	ds := &GenericAPIDS{
		Token:  "secret",
		URL:    srv.URL,
		Config: `{"title":"BTC","headers":{"Accept":"application/json"},"rows":[{"label":"Price","path":"bitcoin.usd"},{"label":"Vibe","path":"sentiment.rating"}]}`,
	}
	title, rows, err := ds.Extract()
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if title != "BTC" {
		t.Fatalf("title = %q, want BTC", title)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Label != "Price" || rows[0].Value != "12345.5" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
	if rows[1].Value != "bullish" {
		t.Fatalf("rows[1].Value = %q, want bullish", rows[1].Value)
	}
}

func TestGenericAPIExtractUnresolvedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()

	ds := &GenericAPIDS{
		URL:    srv.URL,
		Config: `{"rows":[{"label":"Missing","path":"nope.deep"}]}`,
	}
	_, rows, err := ds.Extract()
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(rows) != 1 || rows[0].Value != "n/a" {
		t.Fatalf("unresolved path should yield n/a placeholder, got %+v", rows)
	}
}

func TestGenericAPIExtractError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ds := &GenericAPIDS{URL: srv.URL, Config: `{"title":"X"}`}
	if _, _, err := ds.Extract(); err == nil {
		t.Fatal("Extract should error on upstream failure")
	}
}

func TestGenericAPIGetPNGFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ds := &GenericAPIDS{URL: srv.URL, Config: `{"title":"BTC"}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG must fall back, not error: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG (fallback)", img.Format)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestParseGenericAPIConfig(t *testing.T) {
	cfg := ParseGenericAPIConfig(`{"title":"T","headers":{"H":"1"},"rows":[{"label":"L","path":"P"}]}`)
	if cfg.Title != "T" || len(cfg.Rows) != 1 || cfg.Rows[0].Path != "P" {
		t.Fatalf("ParseGenericAPIConfig = %+v", cfg)
	}
	// Malformed input must not panic and yields an empty config.
	bad := ParseGenericAPIConfig("not json")
	if bad.Title != "" || bad.Headers == nil {
		t.Fatalf("malformed config should yield empty, got %+v", bad)
	}
	if got := GenericAPITitle(`{"title":"Y"}`); got != "Y" {
		t.Fatalf("GenericAPITitle = %q", got)
	}
}
