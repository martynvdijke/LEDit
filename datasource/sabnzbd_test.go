package datasource

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSabnzbdSuccessRender(t *testing.T) {
	var gotURL string
	var gotAPIKeyHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotAPIKeyHeader = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"queue":{"status":"Downloading","kbpersec":"123.45","mbleft":"678.9","noofslots":2,"timeleft":"0:05:00","slots":[{"filename":"VeryLongFileNameThatExceedsTwentyTwoChars.mkv","percentage":"42","mb":"100","timeleft":"0:02:00"},{"filename":"SecondFile.iso","percentage":"90","mb":"200","timeleft":"0:03:00"}]}}`))
	}))
	defer srv.Close()
	ds := &SabnzbdDS{Token: "mykey", URL: srv.URL + "/api"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(gotURL, "apikey=mykey") {
		t.Fatalf("URL %q should contain apikey", gotURL)
	}
	if gotAPIKeyHeader != "" {
		t.Fatalf("X-API-Key header should be empty, got %q", gotAPIKeyHeader)
	}
}

func TestSabnzbdNoDoubleAppend(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Write([]byte(`{"queue":{"status":"Idle","kbpersec":"0","mbleft":"0","noofslots":0,"timeleft":"0:00:00","slots":[]}}`))
	}))
	defer srv.Close()
	ds := &SabnzbdDS{Token: "mykey", URL: srv.URL + "/api?mode=queue&output=json&apikey=existing"}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if strings.Count(gotURL, "apikey=") != 1 {
		t.Fatalf("URL %q should have exactly one apikey", gotURL)
	}
	if !strings.Contains(gotURL, "apikey=existing") {
		t.Fatalf("should preserve existing apikey")
	}
}

func TestSabnzbdIdleQueue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"queue":{"status":"Idle","kbpersec":"0","mbleft":"0","noofslots":0,"timeleft":"0:00:00","slots":[]}}`))
	}))
	defer srv.Close()
	ds := &SabnzbdDS{Token: "k", URL: srv.URL + "/api"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSabnzbd5xxFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "err", 500)
	}))
	defer srv.Close()
	ds := &SabnzbdDS{Token: "k", URL: srv.URL + "/api"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback should not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSabnzbdMalformedJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	ds := &SabnzbdDS{Token: "k", URL: srv.URL + "/api"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSabnzbdCurrentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"queue":{"status":"Downloading","kbpersec":"123.45","mbleft":"678.9","noofslots":2,"timeleft":"0:05:00","slots":[]}}`))
	}))
	defer srv.Close()
	ds := &SabnzbdDS{Token: "k", URL: srv.URL + "/api"}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["status"] != "Downloading" {
		t.Fatalf("status %v", m["status"])
	}
	if m["kbpersec"] == float64(0) {
		t.Fatalf("kbpersec should not be 0")
	}
	if m["noofslots"] != 2 {
		t.Fatalf("noofslots %v want 2", m["noofslots"])
	}
	if m["timeleft"] != "0:05:00" {
		t.Fatalf("timeleft %v", m["timeleft"])
	}
}
