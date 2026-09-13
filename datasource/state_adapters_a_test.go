package datasource

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCurrentState_Weather(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"main":{"temp":21.5,"humidity":78},"weather":[{"main":"Clear","description":"clear sky"}],"name":"London"}`)
	}))
	defer srv.Close()
	ds := &WeatherDS{Token: "tok", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if v, ok := m["temp"]; !ok || v != 21.5 {
		t.Fatalf("temp = %v want 21.5", m["temp"])
	}
	if _, ok := m["temp"].(float64); !ok {
		t.Fatalf("temp type %T want float64", m["temp"])
	}
	if m["condition"] != "clear" {
		t.Fatalf("condition = %v want clear", m["condition"])
	}
	if m["humidity"] != 78 {
		t.Fatalf("humidity = %v", m["humidity"])
	}
	if _, ok := m["humidity"].(int); !ok {
		t.Fatalf("humidity type %T want int", m["humidity"])
	}

	// lowercase normalization
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"main":{"temp":10,"humidity":50},"weather":[{"main":"Rain"}],"name":"X"}`)
	}))
	defer srv2.Close()
	ds2 := &WeatherDS{URL: srv2.URL}
	m2, _ := ds2.CurrentState(context.Background())
	if m2["condition"] != "rain" {
		t.Fatalf("condition rain got %v", m2["condition"])
	}

	// error path
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv3.Close()
	ds3 := &WeatherDS{URL: srv3.URL}
	m3, err := ds3.CurrentState(context.Background())
	if err == nil || m3 != nil {
		t.Fatalf("expected nil,err got %v %v", m3, err)
	}
}

func TestCurrentState_Transit(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * time.Minute).Format(time.RFC3339)
	past := now.Add(-5 * time.Minute).Format(time.RFC3339)
	future2 := now.Add(2 * time.Minute).Format(time.RFC3339)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"departures":[{"line":"U1","destination":"A","time":"%s"},{"line":"S7","destination":"Potsdam","time":"%s"},{"line":"U2","destination":"B","time":"%s"}]}`, past, future, future2)
	}))
	defer srv.Close()
	ds := &TransitDS{Token: "stop", URL: srv.URL, Provider: "custom", Timezone: "UTC", now: func() time.Time { return now }}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if m["nextDepartureMinutes"] != 2 {
		t.Fatalf("mins %v want 2", m["nextDepartureMinutes"])
	}
	if m["line"] != "S7" && m["line"] != "U2" {
		// sorted: earliest is 2 min (S7? check). future2 is S7? Actually second future2 has line S7 destination Potsdam -> no wait
	}
	// earliest after now is future2 (2 min, S7? actually we used S7 for Potsdam future, U2 future2)
	// Let's check: past U1, future S7 10min, future2 U2 2min -> earliest is U2
	if m["line"] != "U2" {
		t.Fatalf("line %v want U2", m["line"])
	}
	if m["destination"] != "B" {
		t.Fatalf("dest %v", m["destination"])
	}

	// WalkTimeMin
	ds2 := &TransitDS{Token: "stop", URL: srv.URL, Provider: "custom", Timezone: "UTC", WalkTimeMin: 5, now: func() time.Time { return now }}
	m2, _ := ds2.CurrentState(context.Background())
	// effectiveNow = 12:05, U2 at 12:02 is filtered, next is S7 at 12:10 -> 5 mins
	if m2["nextDepartureMinutes"] != 5 {
		t.Fatalf("walk mins %v want 5", m2["nextDepartureMinutes"])
	}

	// empty departures
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"departures":[]}`)
	}))
	defer srvEmpty.Close()
	ds3 := &TransitDS{Token: "s", URL: srvEmpty.URL, Provider: "custom", Timezone: "UTC", now: func() time.Time { return now }}
	m3, err := ds3.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("empty err %v", err)
	}
	if m3["nextDepartureMinutes"] != 0 {
		t.Fatalf("empty mins %v", m3["nextDepartureMinutes"])
	}
	if len(m3) != 1 {
		t.Fatalf("empty len %v want 1", len(m3))
	}

	// fetch error
	ds4 := &TransitDS{Token: "s", URL: "http://127.0.0.1:1", Provider: "vbb", Timezone: "UTC"}
	m4, err := ds4.CurrentState(context.Background())
	if err == nil || m4 != nil {
		t.Fatalf("expected error")
	}
}

func TestCurrentState_Crypto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"bitcoin":{"usd":43210.5,"usd_24h_change":2.34},"ethereum":{"usd":3000,"usd_24h_change":-1}}`)
	}))
	defer srv.Close()
	ds := &CryptoDS{Token: "bitcoin,ethereum", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("%v", err)
	}
	if m["price"] != 43210.5 {
		t.Fatalf("price %v", m["price"])
	}
	if m["change"] != 2.34 {
		t.Fatalf("change %v", m["change"])
	}
	// first coin only
	ds2 := &CryptoDS{Token: "ethereum,bitcoin", URL: srv.URL}
	m2, _ := ds2.CurrentState(context.Background())
	if m2["price"] != 3000.0 {
		t.Fatalf("eth price %v", m2["price"])
	}

	// missing change defaults 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"bitcoin":{"usd":100}}`)
	}))
	defer srv2.Close()
	ds3 := &CryptoDS{Token: "bitcoin", URL: srv2.URL}
	m3, _ := ds3.CurrentState(context.Background())
	if m3["change"] != 0.0 {
		t.Fatalf("change missing %v", m3["change"])
	}

	// error
	ds4 := &CryptoDS{Token: "bitcoin", URL: "http://127.0.0.1:1"}
	if _, err := ds4.CurrentState(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCurrentState_Stock(t *testing.T) {
	// body with raw values (real Yahoo includes fmt after raw)
	body := `{"chart":{"result":[{"meta":{"regularMarketPrice":{"raw":150.25,"fmt":"150.25"},"regularMarketPreviousClose":{"raw":145.25,"fmt":"145.25"}}}]}}`
	// alternative pattern used by extractJSONFloat
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	// test URL seam with %s
	ds := &StockDS{Token: "AAPL,MSFT", URL: srv.URL + "/chart/%s"}
	// Need server to handle any path: above server matches all paths
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("%v", err)
	}
	if m["price"] != 150.25 {
		t.Fatalf("price %v", m["price"])
	}
	if m["change"] != 5.0 {
		t.Fatalf("change %v want 5.0 got %v", 5.0, m["change"])
	}

	// URL without %s (verbatim)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv2.Close()
	ds2 := &StockDS{Token: "AAPL", URL: srv2.URL}
	m2, _ := ds2.CurrentState(context.Background())
	if m2["price"] != 150.25 {
		t.Fatalf("verbatim price %v", m2["price"])
	}

	// verify GetPNG also uses seam (request hits server)
	var hit bool
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		fmt.Fprint(w, body)
	}))
	defer srv3.Close()
	ds3 := &StockDS{Token: "AAPL", URL: srv3.URL}
	_, _ = ds3.GetPNG(64, 32)
	if !hit {
		t.Fatal("GetPNG did not use URL seam")
	}

	// error path
	ds4 := &StockDS{Token: "AAPL", URL: "http://127.0.0.1:1"}
	if _, err := ds4.CurrentState(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestStateAdapter_Interface(t *testing.T) {
	var _ StateProvider = (*WeatherDS)(nil)
	var _ StateProvider = (*TransitDS)(nil)
	var _ StateProvider = (*CryptoDS)(nil)
	var _ StateProvider = (*StockDS)(nil)
}
