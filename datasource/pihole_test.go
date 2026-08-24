package datasource

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildPiholeRows(t *testing.T) {
	body := []byte(`{"status":"enabled","queries_today":1234,"ads_blocked_today":56,"ads_percentage_today":42.5}`)
	rows, err := BuildPiholeRows(body)
	if err != nil {
		t.Fatalf("BuildPiholeRows: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows len %d want 4", len(rows))
	}
	expect := map[string]string{
		"STATUS": "ENABLED", "QUERIES": "1234", "BLOCKED": "56", "BLOCKED %": "42%",
	}
	for _, r := range rows {
		if expect[r[0]] != r[1] {
			t.Fatalf("row %q = %q want %q", r[0], r[1], expect[r[0]])
		}
	}
}

func TestPiholeAuthAppendedWhenMissing(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		if r.Header.Get("X-API-Key") != "" {
			t.Errorf("X-API-Key should not be set, got %q", r.Header.Get("X-API-Key"))
		}
		w.Write([]byte(`{"status":"enabled","queries_today":1,"ads_blocked_today":1,"ads_percentage_today":10}`))
	}))
	defer srv.Close()

	ds := &PiHoleDS{Token: "mytoken", URL: srv.URL + "?summary"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if !strings.Contains(gotURL, "auth=mytoken") {
		t.Fatalf("request URL %q should contain auth=mytoken", gotURL)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestPiholeNoDoubleAppend(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Write([]byte(`{"status":"enabled","queries_today":1,"ads_blocked_today":1,"ads_percentage_today":10}`))
	}))
	defer srv.Close()

	ds := &PiHoleDS{Token: "mytoken", URL: srv.URL + "?summary&auth=existing"}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if strings.Count(gotURL, "auth=") != 1 {
		t.Fatalf("URL %q should contain exactly one auth=, got %d", gotURL, strings.Count(gotURL, "auth="))
	}
	if !strings.Contains(gotURL, "auth=existing") {
		t.Fatalf("URL %q should preserve existing auth", gotURL)
	}
}

func TestPiholeSuccessRender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"disabled","queries_today":999,"ads_blocked_today":10,"ads_percentage_today":1.2}`))
	}))
	defer srv.Close()
	ds := &PiHoleDS{Token: "tok", URL: srv.URL + "?summary"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestPihole401Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	ds := &PiHoleDS{Token: "tok", URL: srv.URL + "?summary"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG should fallback not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("fallback decode: %v", err)
	}
}

func TestPiholeMalformedJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	ds := &PiHoleDS{Token: "tok", URL: srv.URL + "?summary"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG should fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
