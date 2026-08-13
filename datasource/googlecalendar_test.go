package datasource

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

const icalFixture = `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
SUMMARY:Team standup
DTSTART:20260813T100000Z
END:VEVENT
BEGIN:VEVENT
SUMMARY:Deploy to prod
DTSTART:20260814T150000Z
END:VEVENT
END:VCALENDAR`

func TestGoogleCalendarGetPNG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "" {
			t.Error("google calendar fetch must not send an API key header")
		}
		w.Write([]byte(icalFixture))
	}))
	defer srv.Close()

	ds := &GoogleCalendarDS{URL: srv.URL, Name: "Work"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG", img.Format)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestGoogleCalendarFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ds := &GoogleCalendarDS{URL: srv.URL, Name: "Work"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG with failing upstream must not error: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG (fallback)", img.Format)
	}
}

func TestGoogleCalendarEmptyEventsFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BEGIN:VCALENDAR\nVERSION:2.0\nEND:VCALENDAR"))
	}))
	defer srv.Close()

	ds := &GoogleCalendarDS{URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG (fallback)", img.Format)
	}
}

func TestIsGoogleICalURL(t *testing.T) {
	if !isGoogleICalURL("https://calendar.google.com/calendar/ical/abc%40gmail.com/private-123/basic.ics") {
		t.Fatal("private iCal URL should be recognized")
	}
	if isGoogleICalURL("https://example.com/feed.ics") {
		t.Fatal("non-google URL should not be recognized")
	}
}
