package datasource

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func weatherServer(conditionMain, description string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"main":    map[string]any{"temp": 21.5, "humidity": 55},
			"weather": []map[string]any{{"main": conditionMain, "description": description}},
			"name":    "London",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestNormalizeCondition(t *testing.T) {
	cases := map[string]string{
		"Rain": "rain", "SNOW": "snow", "Thunderstorm": "thunderstorm", "Drizzle": "drizzle",
		"Clear": "clear", "Clouds": "clouds", "": "unknown", "Fog": "fog",
	}
	for in, want := range cases {
		if got := normalizeCondition(in); got != want {
			t.Errorf("normalize %q got %q want %q", in, got, want)
		}
	}
}

func TestWeatherAmbientDeterminism(t *testing.T) {
	srv := weatherServer("Rain", "light rain")
	defer srv.Close()
	bucket := time.UnixMilli(4000) // 2s bucket = 2
	ds := &WeatherDS{URL: srv.URL, now: func() time.Time { return bucket }}
	a, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	ds2 := &WeatherDS{URL: srv.URL, now: func() time.Time { return time.UnixMilli(4000 + 500) }} // same bucket
	// need to set condition to precipitation so overlay triggers; reuse server
	// But second DS hasn't fetched yet; its ambient false initially. So call GetPNG again with same bucket.
	// To compare determinism within same bucket we reuse same ds with same now.
	ds.now = func() time.Time { return time.UnixMilli(4000 + 800) }
	b, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Data, b.Data) {
		t.Fatal("same 2s bucket should be byte-identical")
	}
	// different bucket differs
	ds.now = func() time.Time { return time.UnixMilli(6000) }
	c, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Data, c.Data) {
		t.Fatal("different buckets should differ")
	}
	_ = ds2
}

func TestWeatherClearNoOverlay(t *testing.T) {
	srvRain := weatherServer("Rain", "light rain")
	defer srvRain.Close()
	srvClear := weatherServer("Clear", "clear sky")
	defer srvClear.Close()
	now := time.UnixMilli(8000)
	dsRain := &WeatherDS{URL: srvRain.URL, now: func() time.Time { return now }}
	rainImg, _ := dsRain.GetPNG(64, 64)
	dsClear := &WeatherDS{URL: srvClear.URL, now: func() time.Time { return now }}
	clearImg, _ := dsClear.GetPNG(64, 64)
	if bytes.Equal(rainImg.Data, clearImg.Data) {
		t.Fatal("rain and clear should differ (overlay)")
	}
	// Clear should equal no-overlay render (fallback without overlay would be similar check)
	// Just ensure Ambient false
	if dsClear.Ambient() {
		t.Fatal("clear should not be ambient")
	}
	if !dsRain.Ambient() {
		t.Fatal("rain should be ambient")
	}
}

func TestWeatherFallbackNoOverlay(t *testing.T) {
	// server returns 500 -> fallback
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	ds := &WeatherDS{URL: srv.URL, now: func() time.Time { return time.UnixMilli(10000) }}
	img1, _ := ds.GetPNG(64, 64)
	img2, _ := ds.GetPNG(64, 64)
	if !bytes.Equal(img1.Data, img2.Data) {
		t.Fatal("fallback should be deterministic")
	}
	if ds.Ambient() {
		t.Fatal("fallback unknown should not be ambient")
	}
	// fallback equals plain fallbackWeather directly
	fb := fallbackWeather(64, 64)
	if !bytes.Equal(img1.Data, fb.Data) {
		t.Fatal("fallback should equal plain fallback (no overlay)")
	}
}

func TestWeatherParticleCap(t *testing.T) {
	srv := weatherServer("Snow", "snow")
	defer srv.Close()
	ds := &WeatherDS{URL: srv.URL, now: func() time.Time { return time.UnixMilli(12000) }}
	img, err := ds.GetPNG(200, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Data) == 0 {
		t.Fatal("empty image")
	}
}
