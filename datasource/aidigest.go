package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"ledit/render"
)

// AIDigestDS renders an AI-generated news digest. The digest is produced by
// the configured LLM from the referenced feed headlines and cached in memory
// for TTL. Generation is single-flight per digest: concurrent renders share
// one in-flight LLM call and degrade to the last good digest (stale-beats-
// blank) when the LLM or the feeds are unavailable.
type AIDigestDS struct {
	ID       int
	Name     string
	Prompt   string
	FeedURLs []string
	TTL      time.Duration
	Config   AIConfig
	// Preview renders a configuration summary instead of calling the LLM.
	// Used by the admin form live preview so keystrokes never hit the API.
	Preview bool
}

type digestEntry struct {
	mu        sync.Mutex
	text      string
	generated time.Time
	stale     string
	inFlight  bool
}

var digestCache sync.Map // int(digest ID) -> *digestEntry

// InvalidateDigest drops the cached digest for id, forcing regeneration on the
// next render. Used by the manual refresh action.
func InvalidateDigest(id int) {
	digestCache.Delete(id)
}

func (d *AIDigestDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if d.TTL <= 0 {
		d.TTL = 30 * time.Minute
	}
	if d.Preview {
		return renderDigestSummary(d.Name, d.FeedURLs, d.TTL, width, height), nil
	}
	if strings.TrimSpace(d.Config.Endpoint) == "" || strings.TrimSpace(d.Config.Model) == "" {
		// AI not configured: render a placeholder without any network call.
		slog.Warn("AI digest skipped: AI not configured", "source", "aidigest", "name", d.Name)
		return fallbackDigest(d.Name, width, height), nil
	}

	key := d.ID
	val, _ := digestCache.LoadOrStore(key, &digestEntry{})
	entry := val.(*digestEntry)

	entry.mu.Lock()
	if !entry.inFlight && entry.text != "" && time.Since(entry.generated) < d.TTL {
		text := entry.text
		entry.mu.Unlock()
		return renderDigest(d.Name, text, width, height), nil
	}
	if entry.inFlight {
		// Another render owns generation; serve stale without blocking.
		text := entry.stale
		entry.mu.Unlock()
		if text == "" {
			return fallbackDigest(d.Name, width, height), nil
		}
		return renderDigest(d.Name, text, width, height), nil
	}
	// We own generation.
	entry.inFlight = true
	entry.mu.Unlock()

	text, err := d.generate(context.Background())

	entry.mu.Lock()
	entry.inFlight = false
	if err != nil {
		slog.Warn("AI digest generation failed, using stale", "source", "aidigest", "name", d.Name, "error", err)
	} else {
		entry.text = text
		entry.generated = time.Now()
		entry.stale = text
	}
	stale := entry.stale
	text = entry.text
	generated := entry.generated
	entry.mu.Unlock()

	switch {
	case err == nil && text != "":
		return renderDigest(d.Name, text, width, height), nil
	case stale != "" && time.Since(generated) < d.TTL:
		return renderDigest(d.Name, stale, width, height), nil
	default:
		return fallbackDigest(d.Name, width, height), nil
	}
}

// generate fetches the referenced feed headlines and asks the LLM to condense
// them into a display-sized digest.
func (d *AIDigestDS) generate(ctx context.Context) (string, error) {
	headlines := d.fetchHeadlines(ctx, 12)
	if len(headlines) == 0 {
		return "", fmt.Errorf("no headlines available from %d feeds", len(d.FeedURLs))
	}

	var b strings.Builder
	if strings.TrimSpace(d.Prompt) != "" {
		b.WriteString(d.Prompt)
		b.WriteString("\n\n")
	}
	b.WriteString("Headlines:\n")
	for _, h := range headlines {
		fmt.Fprintf(&b, "- [%s] %s\n", h.tag, h.title)
	}

	messages := []ChatMessage{
		{Role: "system", Content: BuildDigestSystemPrompt()},
		{Role: "user", Content: b.String()},
	}
	return ChatCompletions(ctx, d.Config, messages, 300)
}

type digestHeadline struct {
	title string
	tag   string
}

// fetchHeadlines pulls and dedupes headlines across all referenced feeds.
func (d *AIDigestDS) fetchHeadlines(ctx context.Context, max int) []digestHeadline {
	var out []digestHeadline
	seen := map[string]bool{}
	for _, feedURL := range d.FeedURLs {
		body, err := apiGet(feedURL, "", nil)
		if err != nil {
			slog.Warn("AI digest feed fetch failed", "source", "aidigest", "url", feedURL, "error", err)
			continue
		}
		tag := sourceTag(feedURL)
		for _, t := range parseRSS(string(body)) {
			if seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, digestHeadline{title: t, tag: tag})
			if len(out) >= max {
				return out
			}
		}
	}
	return out
}

// renderDigest lays out the LLM text as a source-labeled panel, consistent
// with the RSS and news datasources.
func renderDigest(name, text string, width, height int) *render.RenderedImage {
	data := map[string]string{"source": "AI: " + name}
	lines := strings.Split(text, "\n")
	added := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if len(ln) > 28 {
			ln = ln[:28] + "..."
		}
		data[fmt.Sprintf("#%d", added+1)] = ln
		added++
		if added >= 4 {
			break
		}
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

func fallbackDigest(name string, width, height int) *render.RenderedImage {
	data := map[string]string{
		"source": "AI: " + name,
		"status": "unavailable",
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

// renderDigestSummary renders a static configuration summary for the admin
// form live preview. It never calls the LLM.
func renderDigestSummary(name string, feeds []string, ttl time.Duration, width, height int) *render.RenderedImage {
	data := map[string]string{"source": "AI: " + name}
	data["#1"] = fmt.Sprintf("%d feed(s) selected", len(feeds))
	data["#2"] = "TTL " + ttl.Round(time.Minute).String()
	if name == "" {
		data["#1"] = "Name required"
		data["#2"] = ""
		delete(data, "#2")
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

// ParseDigestSources decodes the JSON array of feed names stored on the
// AIDigest entity.
func ParseDigestSources(s string) []string {
	var names []string
	if err := json.Unmarshal([]byte(s), &names); err != nil {
		return nil
	}
	return names
}
