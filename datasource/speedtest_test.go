package datasource

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpeedtestBearerHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(`{"download":100000000,"upload":50000000,"ping":12,"created_at":"2026-01-02T15:04:05Z"}`))
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "mytoken", URL: srv.URL}
	_, _ = ds.GetPNG(64, 64)
	if got != "Bearer mytoken" {
		t.Fatalf("Authorization %q want Bearer mytoken", got)
	}
}

func TestSpeedtestWrappedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"download":100000000,"upload":50000000,"ping":12,"created_at":"2026-01-02T15:04:05Z"}}`))
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSpeedtestBarePayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"download":80,"upload":40,"ping":10,"created_at":"2026-01-02T15:04:05Z"}`))
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSpeedtestDownloadNormalization(t *testing.T) {
	// bits/s -> 100 Mbps
	r, err := SpeedtestParseBody([]byte(`{"download":100000000,"upload":50000000,"ping":5,"created_at":"2026-01-02T15:04:05Z"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Download != 100 {
		t.Fatalf("download %v want 100", r.Download)
	}
	// already Mbps
	r2, _ := SpeedtestParseBody([]byte(`{"download":80,"upload":40,"ping":5,"created_at":"2026-01-02T15:04:05Z"}`))
	if r2.Download != 80 {
		t.Fatalf("download %v want 80", r2.Download)
	}
}

func TestSpeedtest404Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSpeedtestCurrentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"download":100000000,"upload":50000000,"ping":12,"created_at":"2026-01-02T15:04:05Z"}`))
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "tok", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["download"] != float64(100) || m["healthy"] != true {
		t.Fatalf("state %v", m)
	}
}

func TestSpeedtestFallback500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", 500)
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSpeedtestFallbackMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	ds := &SpeedtestDS{Token: "tok", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
