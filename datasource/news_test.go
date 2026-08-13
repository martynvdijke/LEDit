package datasource

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func rssFeed(items ...string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?><rss version=\"2.0\"><channel><title>F</title>")
	for _, it := range items {
		b.WriteString("<item><title>" + it + "</title></item>")
	}
	b.WriteString("</channel></rss>")
	return b.String()
}

func TestNewsGetPNG(t *testing.T) {
	feedA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rssFeed("Breaking story", "Shared headline")))
	}))
	defer feedA.Close()
	feedB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rssFeed("Shared headline", "Second feed story")))
	}))
	defer feedB.Close()

	ds := &NewsDS{URL: feedA.URL + "," + feedB.URL, Name: "Daily"}
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

func TestNewsOneFeedFails(t *testing.T) {
	feedA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer feedA.Close()
	feedB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rssFeed("Healthy story")))
	}))
	defer feedB.Close()

	ds := &NewsDS{URL: feedA.URL + "," + feedB.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG with one failing feed must not error: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG", img.Format)
	}
}

func TestNewsAllFeedsFailFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ds := &NewsDS{URL: srv.URL + "," + srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG must fall back, not error: %v", err)
	}
	if img.Format != "PNG" {
		t.Fatalf("format = %q, want PNG (fallback)", img.Format)
	}
}

func TestSplitFeedURLs(t *testing.T) {
	got := splitFeedURLs(" https://a.example/feed , ,https://b.example/rss,")
	if len(got) != 2 || got[0] != "https://a.example/feed" || got[1] != "https://b.example/rss" {
		t.Fatalf("splitFeedURLs = %v", got)
	}
	if got := splitFeedURLs(""); len(got) != 0 {
		t.Fatalf("empty input should yield no feeds, got %v", got)
	}
}

func TestSourceTag(t *testing.T) {
	tests := []struct{ url, want string }{
		{"https://feeds.bbci.co.uk/news/rss.xml", "BBCI"},
		{"https://www.example.com/feed", "EXAM"},
		{"https://heise.de/rss", "HEIS"},
		{"://bad", "RSS"},
	}
	for _, tt := range tests {
		if got := sourceTag(tt.url); got != tt.want {
			t.Fatalf("sourceTag(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
