package datasource

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"ledit/render"
)

// NewsDS aggregates one or more RSS/Atom feeds (comma-separated URLs) into a
// single headline source, deduplicating by title and tagging each headline
// with its source.
type NewsDS struct {
	URL  string // comma-separated feed URLs
	Name string
}

type newsItem struct {
	title string
	tag   string
}

func (n *NewsDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	slog.Info("fetching news feeds", "source", "news", "url", n.URL)

	var items []newsItem
	fetched := 0
	for _, feedURL := range splitFeedURLs(n.URL) {
		body, err := apiGet(feedURL, "", nil)
		if err != nil {
			slog.Warn("news feed fetch failed", "source", "news", "url", feedURL, "error", err)
			continue
		}
		titles := parseRSS(string(body))
		tag := sourceTag(feedURL)
		for _, t := range titles {
			items = append(items, newsItem{title: t, tag: tag})
		}
		fetched++
	}

	// Dedupe by title, keeping the first occurrence (feeds are newest-first).
	seen := map[string]bool{}
	var dedup []newsItem
	for _, it := range items {
		if seen[it.title] {
			continue
		}
		seen[it.title] = true
		dedup = append(dedup, it)
	}

	if fetched == 0 || len(dedup) == 0 {
		slog.Warn("news feeds failed or empty, using fallback", "source", "news", "feeds_ok", fetched)
		return fallbackNews(n.Name, width, height), nil
	}

	data := map[string]string{}
	title := "NEWS"
	if n.Name != "" {
		title = n.Name
	}
	data["source"] = title

	for i, it := range dedup {
		if i >= 4 {
			break
		}
		key := fmt.Sprintf("#%d", i+1)
		val := "[" + it.tag + "] " + it.title
		if len(val) > 28 {
			val = val[:28] + "..."
		}
		data[key] = val
	}

	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
}

// splitFeedURLs splits a comma-separated feed URL list, dropping empty parts.
func splitFeedURLs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// sourceTag derives a short uppercase tag from a feed URL hostname, e.g.
// "https://feeds.bbci.co.uk/news/rss.xml" -> "BBC".
func sourceTag(feedURL string) string {
	u, err := url.Parse(feedURL)
	if err != nil {
		return "RSS"
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	labels := strings.Split(host, ".")
	// Prefer the domain label (second from the end, skipping TLDs and
	// subdomains like "feeds.bbci.co.uk" -> "bbci").
	label := labels[0]
	if len(labels) >= 3 {
		label = labels[len(labels)-3]
	}
	tag := strings.ToUpper(label)
	if len(tag) > 4 {
		tag = tag[:4]
	}
	if tag == "" {
		return "RSS"
	}
	return tag
}

func fallbackNews(name string, width, height int) *render.RenderedImage {
	data := map[string]string{
		"source": "NEWS",
		"status": "unavailable",
	}
	if name != "" {
		data["source"] = name
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
