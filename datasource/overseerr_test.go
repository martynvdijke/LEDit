package datasource

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOverseerrRender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/request/count" {
			t.Errorf("path %q", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "tok123" {
			t.Errorf("X-Api-Key header %q want tok123", r.Header.Get("X-Api-Key"))
		}
		w.Write([]byte(`{"pending":2,"approved":3,"processing":1,"available":5,"total":11}`))
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok123", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestOverseerrHeaderReceived(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Api-Key")
		w.Write([]byte(`{"pending":1,"approved":1,"processing":1,"available":1,"total":4}`))
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "mykey", URL: srv.URL}
	_, _ = ds.GetPNG(64, 64)
	if got != "mykey" {
		t.Fatalf("header %q want mykey", got)
	}
}

func TestOverseerrCurrentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"pending":2,"approved":3,"processing":1,"available":5,"total":11}`))
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["pending"] != 2 || m["approved"] != 3 || m["processing"] != 1 || m["available"] != 5 || m["total"] != 11 {
		t.Fatalf("state %v", m)
	}
}

func TestOverseerrFallback401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestOverseerrFallback403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestOverseerrFallback500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestOverseerrFallbackMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestOverseerrFallbackCurrentStateOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", 500)
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "tok", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if m["pending"] != 0 || m["total"] != 0 {
		t.Fatalf("expected zeros %v", m)
	}
}
