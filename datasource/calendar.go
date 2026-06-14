package datasource

import (
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

type CalendarDS struct {
	URL  string
	Name string
}

func (c *CalendarDS) GetPNG() (*render.RenderedImage, error) {
	slog.Info("fetching calendar data", "source", "calendar", "url", c.URL)
	body, err := apiGet(c.URL, "", nil)
	if err != nil {
		slog.Warn("calendar fetch failed, using fallback", "source", "calendar", "error", err)
		return fallbackCalendar(c.Name), nil
	}

	events := parseICal(string(body))
	if len(events) == 0 {
		slog.Warn("calendar no events found, using fallback", "source", "calendar")
		return fallbackCalendar(c.Name), nil
	}
	slog.Info("calendar data fetched successfully", "source", "calendar", "event_count", len(events))

	data := map[string]string{}
	title := "CALENDAR"
	if c.Name != "" {
		title = c.Name
	}
	data["source"] = title

	for i, ev := range events {
		if i >= 4 {
			break
		}
		key := fmt.Sprintf("%d", i+1)
		val := ev
		if len(val) > 28 {
			val = val[:28] + "..."
		}
		data[key] = val
	}

	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func parseICal(ical string) []string {
	var events []string
	lines := strings.Split(ical, "\n")
	var summary string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "SUMMARY:"); ok {
			summary = after
		} else if strings.HasPrefix(line, "DTSTART") {
			if summary != "" {
				events = append(events, summary)
				summary = ""
			}
		}
	}
	if summary != "" {
		events = append(events, summary)
	}
	return events
}

func fallbackCalendar(name string) *render.RenderedImage {
	data := map[string]string{
		"source": "CALENDAR",
		"status": "unavailable",
	}
	if name != "" {
		data["source"] = name
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
