package datasource

import (
	"fmt"
	"strings"

	"ledit/render"
)

type RssFeedDS struct {
	URL  string
	Name string
}

func (r *RssFeedDS) GetPNG() (*render.RenderedImage, error) {
	body, err := apiGet(r.URL, "", nil)
	if err != nil {
		return fallbackRSS(r.Name), nil
	}

	items := parseRSS(string(body))
	if len(items) == 0 {
		return fallbackRSS(r.Name), nil
	}

	data := map[string]string{}
	title := "RSS"
	if r.Name != "" {
		title = r.Name
	}
	data["source"] = title

	for i, item := range items {
		if i >= 4 {
			break
		}
		key := fmt.Sprintf("#%d", i+1)
		val := item
		if len(val) > 28 {
			val = val[:28] + "..."
		}
		data[key] = val
	}

	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func parseRSS(xml string) []string {
	var items []string
	for {
		itemStart := strings.Index(xml, "<item>")
		if itemStart < 0 {
			itemStart = strings.Index(xml, "<entry>")
		}
		if itemStart < 0 {
			break
		}
		xml = xml[itemStart+1:]

		titleStart := strings.Index(xml, "<title>")
		if titleStart < 0 {
			continue
		}
		titleStart += len("<title>")
		titleEnd := strings.Index(xml[titleStart:], "</title>")
		if titleEnd < 0 {
			continue
		}
		title := xml[titleStart : titleStart+titleEnd]
		// Decode common entities
		title = strings.ReplaceAll(title, "&amp;", "&")
		title = strings.ReplaceAll(title, "&lt;", "<")
		title = strings.ReplaceAll(title, "&gt;", ">")
		title = strings.ReplaceAll(title, "&quot;", "\"")
		title = strings.ReplaceAll(title, "&#39;", "'")
		title = strings.ReplaceAll(title, "&#x27;", "'")
		// Remove CDATA
		title = strings.TrimPrefix(title, "<![CDATA[")
		title = strings.TrimSuffix(title, "]]>")
		items = append(items, strings.TrimSpace(title))
		xml = xml[titleStart+titleEnd:]
	}
	return items
}

func fallbackRSS(name string) *render.RenderedImage {
	data := map[string]string{
		"source": "RSS",
		"status": "unavailable",
	}
	if name != "" {
		data["source"] = name
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
