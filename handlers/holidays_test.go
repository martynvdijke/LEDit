package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetHolidaysAndIsHoliday(t *testing.T) {
	ResetHolidays()
	SetHolidays([]string{"2026-12-25"}, []string{"2026-12-26"})
	if !IsHoliday(time.Date(2026, 12, 25, 10, 0, 0, 0, time.Local)) {
		t.Fatal("expected holiday")
	}
	if !IsHoliday(time.Date(2026, 12, 26, 10, 0, 0, 0, time.Local)) {
		t.Fatal("expected ics holiday")
	}
	if IsHoliday(time.Date(2026, 12, 27, 10, 0, 0, 0, time.Local)) {
		t.Fatal("unexpected holiday")
	}
}

func TestParseICSDates(t *testing.T) {
	body := []byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nDTSTART;VALUE=DATE:20261225\nEND:VEVENT\nBEGIN:VEVENT\nDTSTART:20261226\nEND:VEVENT\nBEGIN:VEVENT\nDTSTART:bad\nEND:VEVENT\nEND:VCALENDAR\n")
	dates := ParseICSDates(body)
	if len(dates) != 2 {
		t.Fatalf("got %v", dates)
	}
	if dates[0] != "2026-12-25" || dates[1] != "2026-12-26" {
		t.Fatalf("bad dates %v", dates)
	}
}

func TestFetchHolidayICS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BEGIN:VEVENT\nDTSTART;VALUE=DATE:20261225\nEND:VEVENT"))
	}))
	defer srv.Close()
	dates, err := FetchHolidayICS(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch err %v", err)
	}
	if len(dates) != 1 || dates[0] != "2026-12-25" {
		t.Fatalf("bad %v", dates)
	}
	// failure retains last-known: simulate failure
	ResetHolidays()
	SetHolidays([]string{"2026-01-01"}, nil)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv2.Close()
	_, err = FetchHolidayICS(context.Background(), srv2.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHoliday(time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)) {
		t.Fatal("should retain last-known")
	}
}
